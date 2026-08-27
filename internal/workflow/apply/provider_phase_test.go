package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

const piProviderSource = "npm:pi-mcp-adapter@^2.13.0"

func TestExecuteInstallsPiMCPProviderBeforeConfigProjection(t *testing.T) {
	root, manifestPath := writePiProviderMCPFixture(t)
	configPath := filepath.Join(root, aggregate.PiProjectMCPConfigPath)
	var requests []subprocess.CommandRequest
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			requests = append(requests, request)
			if _, err := os.Lstat(configPath); !os.IsNotExist(err) {
				t.Fatalf("Pi MCP config existed before provider route: %v", err)
			}
			writePiProviderInstallation(t, root, "2.15.0")
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})

	planned, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
		HostRouteExecutor: executor,
	})
	if err != nil {
		t.Fatalf("ExecuteWithOptions returned error: %v", err)
	}
	if len(requests) != 1 ||
		requests[0].Command != "pi" ||
		!slices.Equal(requests[0].Args, []string{"install", piProviderSource, "-l"}) {
		t.Fatalf("provider route requests = %#v", requests)
	}
	if len(result.HostRouteAttempts) != 1 {
		t.Fatalf("HostRouteAttempts = %#v, want one provider attempt", result.HostRouteAttempts)
	}
	if !result.ExecutionAttempted {
		t.Fatal("provider execution did not report crossing the effect boundary")
	}
	if len(result.MCPProjections) != 1 ||
		result.MCPProjections[0].Provider().Version() != "2.15.0" {
		t.Fatalf("MCPProjections = %#v, want current provider version 2.15.0", result.MCPProjections)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("Pi MCP config was not projected after provider verification: %v", err)
	}
}

func TestProviderEffectPreventsWholeApplyRolledBackOutcome(t *testing.T) {
	root, manifestPath := writePiProviderMCPFixture(t)
	configPath := filepath.Join(root, aggregate.PiProjectMCPConfigPath)
	ctx, cancel := context.WithCancel(t.Context())
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
			writePiProviderInstallation(t, root, "2.15.0")
			return subprocess.CommandResult{Started: true, HasExitCode: true}
		},
	})

	planned, err := PlanWrite(ctx, CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	result, err := ExecuteWithOptions(ctx, planned, ExecuteOptions{
		HostRouteExecutor: executor,
		ExecuteEvents: func(event execute.Event) {
			if event.Kind == execute.EventActionDone {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecuteWithOptions error = %v, want cancellation after managed effect", err)
	}
	if !result.ExecutionAttempted || !result.UncompensatedEffectsAttempted {
		t.Fatalf(
			"execution facts = attempted:%t uncompensated:%t, want both true",
			result.ExecutionAttempted,
			result.UncompensatedEffectsAttempted,
		)
	}
	failure := ClassifyFailure(err, result)
	if failure.Outcome() != FailureOutcomeIncomplete {
		t.Fatalf("failure outcome = %q, want incomplete", failure.Outcome())
	}
	if strings.Contains(failure.Detail(), "host changes were rolled back") {
		t.Fatalf("failure detail = %q, want no whole-apply rollback claim", failure.Detail())
	}
	if _, statErr := os.Lstat(configPath); !os.IsNotExist(statErr) {
		t.Fatalf("managed MCP config remained after rollback: %v", statErr)
	}
	if _, statErr := os.Stat(piProviderPackagePath(root)); statErr != nil {
		t.Fatalf("provider effect did not remain after managed rollback: %v", statErr)
	}
}

func TestProviderReplanPreservesAbandonedFileSetResidue(t *testing.T) {
	root, manifestPath := writePiProviderMCPFixture(t)
	paths := applyTestPaths(t, root)
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
			writePiProviderInstallation(t, root, "2.15.0")
			plantAbandonedFileSetResidue(t, paths.StateDir)
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})

	planned, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
		HostRouteExecutor: executor,
	})
	if err == nil || !errors.Is(err, transaction.ErrAbandonedFileSetResidue) {
		t.Fatalf("error = %v, want ErrAbandonedFileSetResidue", err)
	}
	failure := ClassifyFailure(err, result)
	if failure.Reason() != FailureReasonAbandonedFileSetResidue {
		t.Fatalf("reason = %q, want %q", failure.Reason(), FailureReasonAbandonedFileSetResidue)
	}
	if strings.Contains(failure.Detail(), "authorized apply plan changed") ||
		strings.Contains(failure.Detail(), "authoritative inputs changed") ||
		strings.Contains(failure.Detail(), "rerun") {
		t.Fatalf("detail = %q, want preserve-residue guidance not stale-plan retry", failure.Detail())
	}
}

func plantAbandonedFileSetResidue(t *testing.T, stateDir string) {
	t.Helper()
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	residue := filepath.Join(stateDir, ".daem-tmp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteRejectsUnavailableProviderRecoveryProvenanceBeforeEffects(t *testing.T) {
	_, manifestPath := writePiProviderMCPFixture(t)
	prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	planned := prepared.lifecycle.planned
	if planned.projectRoot == nil {
		t.Fatal("provider plan did not retain its non-nil project root")
	}
	statePath := planned.context.Paths.StatefilePath
	recoveryDir := planned.context.Paths.RecoveryDir
	for _, path := range []string{statePath, recoveryDir} {
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			t.Fatalf("pre-execution path %q exists: %v", path, statErr)
		}
	}

	providerCalls := 0
	provenanceChecks := 0
	result, err := executeWithDependencies(t.Context(), prepared, ExecuteOptions{
		HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{
			Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
				providerCalls++
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			},
		}),
	}, executeDependencies{
		recoveryProvenancePreflight: func(authority rootedpath.Authority) error {
			provenanceChecks++
			return rootedpath.NewBoundaryFailure(
				rootedpath.FailureRecoveryEvidenceUnavailable,
				authority.PhysicalRoot(),
				"injected unavailable durable recovery evidence",
				errors.New("injected recovery evidence failure"),
			)
		},
	})
	var failure *rootedpath.Failure
	if !errors.As(err, &failure) ||
		failure.Kind() != rootedpath.FailureRecoveryEvidenceUnavailable {
		t.Fatalf("ExecuteWithOptions error = %v, want unavailable recovery evidence", err)
	}
	if provenanceChecks != 1 || providerCalls != 0 || result.ExecutionAttempted {
		t.Fatalf(
			"provenance checks/provider calls/execution attempted = %d/%d/%t, want 1/0/false",
			provenanceChecks,
			providerCalls,
			result.ExecutionAttempted,
		)
	}
	if len(result.HostRouteAttempts) != 0 {
		t.Fatalf("HostRouteAttempts = %#v, want none", result.HostRouteAttempts)
	}
	for _, path := range []string{statePath, recoveryDir} {
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			t.Fatalf("post-failure path %q exists: %v", path, statErr)
		}
	}
}

func TestPrepareMCPProviderPrerequisiteActionsSkipsRecoveryPreflightWithoutEffects(t *testing.T) {
	preflightCalls := 0
	actions, err := prepareMCPProviderPrerequisiteActions(
		commandPlan{},
		func(rootedpath.Authority) error {
			preflightCalls++
			return errors.New("unexpected recovery preflight")
		},
	)
	if err != nil {
		t.Fatalf("prepare actions returned error without provider effects: %v", err)
	}
	if len(actions) != 0 || preflightCalls != 0 {
		t.Fatalf("provider actions/preflight calls = %#v/%d, want none/0", actions, preflightCalls)
	}
}

func TestExecuteDoesNotProjectPiMCPConfigWhenProviderPostconditionIsAbsent(t *testing.T) {
	root, manifestPath := writePiProviderMCPFixture(t)
	configPath := filepath.Join(root, aggregate.PiProjectMCPConfigPath)
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})

	planned, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
		HostRouteExecutor: executor,
	})
	if err == nil {
		t.Fatal("ExecuteWithOptions error = nil, want provider postcondition failure")
	}
	if len(result.HostRouteAttempts) != 1 {
		t.Fatalf("HostRouteAttempts = %#v, want failed provider attempt", result.HostRouteAttempts)
	}
	if _, statErr := os.Lstat(configPath); !os.IsNotExist(statErr) {
		t.Fatalf("Pi MCP config exists after failed provider postcondition: %v", statErr)
	}
}

func TestValidatePostProviderPlanRequiresSettledProviderRelation(t *testing.T) {
	root, manifestPath := writePiProviderMCPFixture(t)
	initial, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("initial PlanWrite returned error: %v", err)
	}
	if _, err := ExecuteWithOptions(t.Context(), initial, ExecuteOptions{
		HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{
			Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
				writePiProviderInstallation(t, root, "2.15.0")
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			},
		}),
	}); err != nil {
		t.Fatalf("initial ExecuteWithOptions returned error: %v", err)
	}

	planned, err := PlanWrite(t.Context(), CommandInput{
		ManifestPath: manifestPath,
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	defer func() {
		if err := planned.Close(); err != nil {
			t.Fatalf("close prepared write: %v", err)
		}
	}()
	postProviderPlan := planned.lifecycle.planned
	if len(postProviderPlan.assessment.MCPProviders) != 1 {
		t.Fatalf(
			"MCP provider prerequisites = %#v, want one",
			postProviderPlan.assessment.MCPProviders,
		)
	}

	provider := postProviderPlan.assessment.MCPProviders[0].Observation().Carrier()
	var providerAction reconcile.RelationAction
	found := false
	for _, action := range postProviderPlan.assessment.Reconciliation.Relations() {
		if action.CarrierIdentity().ExactEqual(provider) {
			providerAction = action
			found = true
			break
		}
	}
	if !found || providerAction.Kind() != reconcile.ActionNoOp {
		t.Fatalf(
			"provider relation action = %#v found=%t, want settled no-op",
			providerAction,
			found,
		)
	}

	unavailable, err := observerelation.NewInventory(observerelation.InventorySpec{
		Availability: observerelation.InventoryUnavailable,
		Freshness:    observerelation.EvidenceFresh,
	})
	if err != nil {
		t.Fatalf("NewInventory returned error: %v", err)
	}
	degraded, err := reconcile.NewRelationAction(reconcile.RelationActionInput{
		CarrierIdentity: providerAction.CarrierIdentity(),
		RouteRequest:    providerAction.RouteRequest(),
		Correlation: observerelation.Correlate(
			providerAction.ExpectedRelation(),
			unavailable,
		),
		RouteAdmission: providerAction.RouteAdmission(),
	})
	if err != nil {
		t.Fatalf("NewRelationAction returned error: %v", err)
	}
	if degraded.Kind() != reconcile.ActionObserveOnly {
		t.Fatalf("degraded relation kind = %q, want observe_only", degraded.Kind())
	}

	postProviderPlan.assessment.Reconciliation = reconciliationWithRelations(t, degraded)
	if err := validatePostProviderPlan(postProviderPlan); err == nil ||
		!strings.Contains(err.Error(), "not settled") ||
		!strings.Contains(err.Error(), "observe_only") {
		t.Fatalf(
			"validatePostProviderPlan error = %v, want unsettled observe-only provider relation",
			err,
		)
	}

	postProviderPlan.assessment.Reconciliation = reconciliationWithRelations(t)
	if err := validatePostProviderPlan(postProviderPlan); err == nil ||
		!strings.Contains(err.Error(), "absent after prerequisite execution") {
		t.Fatalf(
			"validatePostProviderPlan error = %v, want missing provider relation",
			err,
		)
	}
}

func TestExecuteReplaysPiProviderInstallAfterManualArtifactDeletion(t *testing.T) {
	root, manifestPath := writePiProviderMCPFixture(t)
	initial, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("initial PlanWrite returned error: %v", err)
	}
	initialExecutor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
			writePiProviderInstallation(t, root, "2.15.0")
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})
	if _, err := ExecuteWithOptions(t.Context(), initial, ExecuteOptions{
		HostRouteExecutor: initialExecutor,
	}); err != nil {
		t.Fatalf("initial ExecuteWithOptions returned error: %v", err)
	}
	if err := os.RemoveAll(piProviderPackagePath(root)); err != nil {
		t.Fatalf("remove provider package: %v", err)
	}

	calls := 0
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
			calls++
			writePiProviderPackage(t, root, "2.16.0")
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})
	retry, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("retry PlanWrite returned error: %v", err)
	}
	result, err := ExecuteWithOptions(t.Context(), retry, ExecuteOptions{
		HostRouteExecutor: executor,
	})
	if err != nil {
		t.Fatalf("retry ExecuteWithOptions returned error: %v", err)
	}
	if calls != 1 || len(result.HostRouteAttempts) != 1 {
		t.Fatalf("provider replay calls=%d attempts=%#v, want one", calls, result.HostRouteAttempts)
	}
	if result.MCPProjections[0].Provider().Version() != "2.16.0" {
		t.Fatalf(
			"provider version = %q, want freshly restored 2.16.0",
			result.MCPProjections[0].Provider().Version(),
		)
	}
}

func TestExecuteAcceptsMappedPiProviderUpdateWithoutRouteReplay(t *testing.T) {
	root, manifestPath := writePiProviderMCPFixture(t)
	writePiProviderInstallation(t, root, "2.99.0")
	calls := 0
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
			calls++
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})

	planned, err := PlanWrite(t.Context(), CommandInput{
		ManifestPath:           manifestPath,
		ManageUnmanagedMatches: true,
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
		HostRouteExecutor: executor,
	})
	if err != nil {
		t.Fatalf("ExecuteWithOptions returned error: %v", err)
	}
	if calls != 0 || len(result.HostRouteAttempts) != 0 {
		t.Fatalf("mapped provider update invoked route: calls=%d attempts=%#v", calls, result.HostRouteAttempts)
	}
	if result.MCPProjections[0].Provider().Version() != "2.99.0" {
		t.Fatalf("provider version = %q, want 2.99.0", result.MCPProjections[0].Provider().Version())
	}
}

func TestPlanWriteBlocksUnmappedPiProviderBeforeConfigMutation(t *testing.T) {
	root, manifestPath := writePiProviderMCPFixture(t)
	writePiProviderInstallation(t, root, "3.0.0")
	configPath := filepath.Join(root, aggregate.PiProjectMCPConfigPath)

	result, err := PlanWrite(t.Context(), CommandInput{
		ManifestPath:           manifestPath,
		ManageUnmanagedMatches: true,
	})
	if err == nil || !strings.Contains(err.Error(), "provider_version_incompatible") {
		t.Fatalf("PlanWrite error = %v, want incompatible provider blocker", err)
	}
	if len(result.MCPProjections) != 1 ||
		result.MCPProjections[0].Provider().Version() != "3.0.0" {
		t.Fatalf("MCPProjections = %#v, want exact incompatible version", result.MCPProjections)
	}
	if _, statErr := os.Lstat(configPath); !os.IsNotExist(statErr) {
		t.Fatalf("Pi MCP config exists after incompatible planning: %v", statErr)
	}
}

func TestExecuteCoalescesOnePiProviderRouteAcrossMultipleMCPConsumers(t *testing.T) {
	root, manifestPath := writePiProviderMCPFixtureWithServers(t, `
[[mcp_server]]
name = "context7"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "node"
args = ["context7.js"]

[[mcp_server]]
name = "search"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "node"
args = ["search.js"]
`)
	calls := 0
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
			calls++
			writePiProviderInstallation(t, root, "2.15.0")
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})

	planned, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
		HostRouteExecutor: executor,
	})
	if err != nil {
		t.Fatalf("ExecuteWithOptions returned error: %v", err)
	}
	if calls != 1 || len(result.HostRouteAttempts) != 1 {
		t.Fatalf(
			"provider calls=%d attempts=%#v, want one shared route",
			calls,
			result.HostRouteAttempts,
		)
	}
	if len(result.MCPProjections) != 2 {
		t.Fatalf("MCPProjections = %#v, want two consumers", result.MCPProjections)
	}
}

func TestExecuteStopsProviderRoutesAfterDeclarationChanges(t *testing.T) {
	root := newApplyCarrierFixtureRoot(t)
	agentRoot := filepath.Join(root, "custom-pi-agent")
	t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
	manifestPath := filepath.Join(root, "daem.toml")
	writeApplyFile(t, manifestPath, `version = 1
targets = ["pi"]

[[extension]]
id = "pi-mcp-adapter-project"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:pi-mcp-adapter@^2.13.0" }

[[extension]]
id = "pi-mcp-adapter-global"
carrier = "pi-package"
targets = ["pi"]
scope = "global"
source = { host_source = "npm:pi-mcp-adapter@^2.13.0" }

[[mcp_server]]
name = "project-context"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "node"
args = ["project.js"]

[[mcp_server]]
name = "global-context"
targets = ["pi"]
scope = "global"
transport = "stdio"
command = "node"
args = ["global.js"]
`)
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{
		ManifestPath: manifestPath,
	}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	planned, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}

	calls := 0
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
			calls++
			writePiProviderInstallation(t, root, "2.15.0")
			writeApplyFile(
				t,
				filepath.Join(agentRoot, "settings.json"),
				`{"packages":["`+piProviderSource+`"]}`,
			)
			writeApplyFile(
				t,
				filepath.Join(
					agentRoot,
					"npm",
					"node_modules",
					"pi-mcp-adapter",
					"package.json",
				),
				`{"name":"pi-mcp-adapter","version":"2.15.0"}`,
			)
			content, readErr := os.ReadFile(manifestPath)
			if readErr != nil {
				t.Fatalf("read manifest: %v", readErr)
			}
			writeApplyFile(t, manifestPath, string(content)+"\n# changed between routes\n")
			return subprocess.CommandResult{
				Started: true, HasExitCode: true, ExitCode: 0,
			}
		},
	})

	result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
		PlanWasDisclosed:  true,
		HostRouteExecutor: executor,
	})
	var stale mutation.StalePlanError
	if !errors.As(err, &stale) {
		t.Fatalf("ExecuteWithOptions error = %v, want stale disclosed plan", err)
	}
	if calls != 1 || len(result.HostRouteAttempts) != 1 {
		t.Fatalf(
			"provider calls=%d attempts=%#v, want one route before stale stop",
			calls,
			result.HostRouteAttempts,
		)
	}
}

func TestExecuteInstallsGlobalPiMCPProviderAtCustomAgentRoot(t *testing.T) {
	root, agentRoot, manifestPath := writeGlobalPiProviderMCPFixture(t)
	carrierRegistryPath := isolatedApplyCarrierRegistryPath(t, root)
	if _, err := os.Lstat(carrierRegistryPath); !os.IsNotExist(err) {
		t.Fatalf("isolated carrier registry existed before apply: %v", err)
	}
	var requests []subprocess.CommandRequest
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			requests = append(requests, request)
			writeApplyFile(
				t,
				filepath.Join(agentRoot, "settings.json"),
				`{"packages":["`+piProviderSource+`"]}`,
			)
			writeApplyFile(
				t,
				filepath.Join(
					agentRoot,
					"npm",
					"node_modules",
					"pi-mcp-adapter",
					"package.json",
				),
				`{"name":"pi-mcp-adapter","version":"2.15.0"}`,
			)
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})

	planned, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
		HostRouteExecutor: executor,
	})
	if err != nil {
		t.Fatalf("ExecuteWithOptions returned error: %v", err)
	}
	if len(requests) != 1 ||
		requests[0].Command != "pi" ||
		!slices.Equal(requests[0].Args, []string{"install", piProviderSource}) {
		t.Fatalf("global provider route requests = %#v", requests)
	}
	if len(result.MCPProjections) != 1 ||
		result.MCPProjections[0].Provider().Version() != "2.15.0" {
		t.Fatalf("MCPProjections = %#v, want current global provider", result.MCPProjections)
	}
	if _, err := os.Stat(filepath.Join(agentRoot, "mcp.json")); err != nil {
		t.Fatalf("global Pi MCP config was not projected at custom root: %v", err)
	}
	if _, err := os.Stat(carrierRegistryPath); err != nil {
		t.Fatalf("global provider claim was not persisted in the isolated registry: %v", err)
	}
}

func TestExecuteRejectsGlobalPiRootChangeBeforeProviderRoute(t *testing.T) {
	root, _, manifestPath := writeGlobalPiProviderMCPFixture(t)
	planned, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "changed-pi-agent"))
	calls := 0
	result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
		HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{
			Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
				calls++
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			},
		}),
	})
	reason, classified := mutation.ReasonCodeOf(err)
	if !classified || reason != mutation.ReasonStaleSnapshot {
		t.Fatalf("ExecuteWithOptions error = %v, want stale snapshot", err)
	}
	if calls != 0 || len(result.HostRouteAttempts) != 0 {
		t.Fatalf(
			"changed root provider calls=%d attempts=%#v, want no effect",
			calls,
			result.HostRouteAttempts,
		)
	}
}

func TestExecuteCancellationAfterPiProviderRoutePreventsConfigProjection(t *testing.T) {
	root, manifestPath := writePiProviderMCPFixture(t)
	configPath := filepath.Join(root, aggregate.PiProjectMCPConfigPath)
	ctx, cancel := context.WithCancel(t.Context())
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
			writePiProviderInstallation(t, root, "2.15.0")
			cancel()
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})

	planned, err := PlanWrite(ctx, CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	result, err := ExecuteWithOptions(ctx, planned, ExecuteOptions{
		HostRouteExecutor: executor,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecuteWithOptions error = %v, want context cancellation", err)
	}
	if len(result.HostRouteAttempts) != 0 {
		t.Fatalf("HostRouteAttempts = %#v, want no falsely completed attempt", result.HostRouteAttempts)
	}
	if _, statErr := os.Lstat(configPath); !os.IsNotExist(statErr) {
		t.Fatalf("Pi MCP config exists after cancellation: %v", statErr)
	}
	state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
	if len(state.PendingCarrierInstalls()) != 1 {
		t.Fatalf(
			"PendingCarrierInstalls = %#v, want interrupted provider boundary",
			state.PendingCarrierInstalls(),
		)
	}

	retryCalls := 0
	retry, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("retry PlanWrite returned error: %v", err)
	}
	retryResult, err := ExecuteWithOptions(t.Context(), retry, ExecuteOptions{
		HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{
			Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
				retryCalls++
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			},
		}),
	})
	if err != nil {
		t.Fatalf("retry ExecuteWithOptions returned error: %v", err)
	}
	if retryCalls != 0 || len(retryResult.HostRouteAttempts) != 0 {
		t.Fatalf(
			"retry provider calls=%d attempts=%#v, want fresh observation without replay",
			retryCalls,
			retryResult.HostRouteAttempts,
		)
	}
	if _, statErr := os.Stat(configPath); statErr != nil {
		t.Fatalf("Pi MCP config was not projected during recovery retry: %v", statErr)
	}
}

func TestPlanWriteBlocksUnobservablePiProviderBeforeConfigMutation(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, string)
	}{
		{
			name: "malformed package manifest",
			prepare: func(t *testing.T, root string) {
				writePiProviderInstallation(t, root, "2.15.0")
				writeApplyFile(
					t,
					filepath.Join(piProviderPackagePath(root), "package.json"),
					`{"name":"pi-mcp-adapter","version":`,
				)
			},
		},
		{
			name: "symlinked package manifest",
			prepare: func(t *testing.T, root string) {
				writePiProviderInstallation(t, root, "2.15.0")
				manifestPath := filepath.Join(piProviderPackagePath(root), "package.json")
				if err := os.Remove(manifestPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(root, "outside.json"), manifestPath); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, manifestPath := writePiProviderMCPFixture(t)
			test.prepare(t, root)
			configPath := filepath.Join(root, aggregate.PiProjectMCPConfigPath)

			result, err := PlanWrite(t.Context(), CommandInput{
				ManifestPath:           manifestPath,
				ManageUnmanagedMatches: true,
			})
			if err == nil || !strings.Contains(err.Error(), "provider_version_unobserved") {
				t.Fatalf("PlanWrite error = %v, want unobserved provider blocker", err)
			}
			if len(result.MCPProjections) != 1 ||
				result.MCPProjections[0].Provider().Version() != "" {
				t.Fatalf("MCPProjections = %#v, want redaction-safe unobserved state", result.MCPProjections)
			}
			if _, statErr := os.Lstat(configPath); !os.IsNotExist(statErr) {
				t.Fatalf("Pi MCP config exists after unsafe provider planning: %v", statErr)
			}
		})
	}
}

func writePiProviderMCPFixture(t *testing.T) (string, string) {
	t.Helper()
	return writePiProviderMCPFixtureWithServers(t, `
[[mcp_server]]
name = "context7"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "node"
args = ["server.js"]
`)
}

func writePiProviderMCPFixtureWithServers(
	t *testing.T,
	servers string,
) (string, string) {
	t.Helper()
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	writeApplyFile(t, manifestPath, `version = 1
targets = ["pi"]

[[extension]]
id = "pi-mcp-adapter-project"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:pi-mcp-adapter@^2.13.0" }

`+servers)
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{
		ManifestPath: manifestPath,
	}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	return root, manifestPath
}

func writeGlobalPiProviderMCPFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root := newApplyCarrierFixtureRoot(t)
	agentRoot := filepath.Join(root, "custom-pi-agent")
	t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
	manifestPath := filepath.Join(root, "daem.toml")
	writeApplyFile(t, manifestPath, `version = 1
targets = ["pi"]

[[extension]]
id = "pi-mcp-adapter-global"
carrier = "pi-package"
targets = ["pi"]
scope = "global"
source = { host_source = "npm:pi-mcp-adapter@^2.13.0" }

[[mcp_server]]
name = "context7"
targets = ["pi"]
scope = "global"
transport = "stdio"
command = "node"
args = ["server.js"]
`)
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{
		ManifestPath: manifestPath,
	}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	return root, agentRoot, manifestPath
}

func writePiProviderInstallation(t *testing.T, root string, version string) {
	t.Helper()
	writeApplyFile(
		t,
		filepath.Join(root, ".pi", "settings.json"),
		`{"packages":["`+piProviderSource+`"]}`,
	)
	writePiProviderPackage(t, root, version)
}

func writePiProviderPackage(t *testing.T, root string, version string) {
	t.Helper()
	writeApplyFile(
		t,
		filepath.Join(piProviderPackagePath(root), "package.json"),
		`{"name":"pi-mcp-adapter","version":"`+version+`"}`,
	)
}

func piProviderPackagePath(root string) string {
	return filepath.Join(root, ".pi", "npm", "node_modules", "pi-mcp-adapter")
}
