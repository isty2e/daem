package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/statefile"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/mutation"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	lock "github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

type openCodeDirectWorkflowFixture struct {
	*workflowFixture
	configPaths    []string
	authorityPaths []observerelation.AuthorityPath
}

func TestExecuteWithOptionsDirectOpenCodeRemovalConvergesEndToEnd(t *testing.T) {
	const source = "@acme/opencode-formatter"

	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			root, manifestPath, lockfilePath, _, previous, _ := writeApplyOpenCodePluginCarrierCommandFixtureForScope(t, scope)
			claim := seedApplyCarrierClaim(t, root, manifestPath, previous, scope)
			writeApplyLockfile(t, lockfilePath, lock.File{Version: lock.CurrentVersion})

			globalConfigRoot := filepath.Join(root, "xdg-config")
			t.Setenv("XDG_CONFIG_HOME", globalConfigRoot)
			projectDirectory := filepath.Join(root, ".opencode")
			globalDirectory := filepath.Join(globalConfigRoot, "opencode")
			selectedDirectory := projectDirectory
			foreignDirectory := globalDirectory
			if scope == target.ScopeGlobal {
				selectedDirectory = globalDirectory
				foreignDirectory = projectDirectory
			}

			selectedPaths := writeOpenCodeRemovalConfigs(t, selectedDirectory, source)
			foreignPaths := writeOpenCodeRemovalConfigs(t, foreignDirectory, source)
			retainedArtifact := filepath.Join(root, "retained-package", "package.json")
			writeApplyFile(t, retainedArtifact, `{"name":"retained"}`)

			prepared, err := PlanWrite(t.Context(), CommandInput{
				ManifestPath: manifestPath,
				LockfilePath: lockfilePath,
				TargetValues: []string{"opencode"},
			})
			if err != nil {
				t.Fatalf("PlanWrite: %v", err)
			}
			absences := prepared.Reconciliation.CarrierAbsences()
			if len(absences) != 1 ||
				absences[0].Decision() != carrierabsence.DecisionRemove ||
				!absences[0].MutatesDirectProjection() {
				t.Fatalf("carrier absences = %#v, want one direct removal", absences)
			}
			if _, present := absences[0].HostRouteRequest(); present {
				t.Fatal("direct OpenCode removal exposed a host route")
			}

			result, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{
				PlanWasDisclosed: true,
			})
			if err != nil {
				t.Fatalf("ExecuteWithOptions: %v", err)
			}
			if result.ActionCount != 1 ||
				len(result.HostRouteAttempts) != 0 ||
				len(result.DelegateAttempts) != 0 {
				t.Fatalf(
					"result = actions %d host attempts %d delegate attempts %d",
					result.ActionCount,
					len(result.HostRouteAttempts),
					len(result.DelegateAttempts),
				)
			}
			assertOpenCodeSourceAbsent(t, selectedPaths, source)
			assertOpenCodeSourcePresent(t, foreignPaths, source)
			assertApplyFileContent(t, retainedArtifact, `{"name":"retained"}`)
			assertApplyCarrierClaimAbsent(t, root, manifestPath, claim)

			before := readFiles(t, append(selectedPaths, foreignPaths...))
			retry, err := PlanWrite(t.Context(), CommandInput{
				ManifestPath: manifestPath,
				LockfilePath: lockfilePath,
			})
			if err != nil {
				t.Fatalf("retry PlanWrite: %v", err)
			}
			if len(retry.Reconciliation.CarrierAbsences()) != 0 {
				t.Fatalf(
					"retry carrier absences = %#v, want no action",
					retry.Reconciliation.CarrierAbsences(),
				)
			}
			retryResult, err := ExecuteWithOptions(t.Context(), retry, ExecuteOptions{
				PlanWasDisclosed: true,
			})
			if err != nil {
				t.Fatalf("retry ExecuteWithOptions: %v", err)
			}
			if retryResult.ActionCount != 0 {
				t.Fatalf("retry ActionCount = %d, want no-op", retryResult.ActionCount)
			}
			after := readFiles(t, append(selectedPaths, foreignPaths...))
			for path, content := range before {
				if after[path] != content {
					t.Fatalf("retry rewrote %q:\nbefore: %q\nafter:  %q", path, content, after[path])
				}
			}
		})
	}
}

func TestExecuteWithOptionsRunsManagedMutationBeforeDirectRemoval(t *testing.T) {
	const source = "@acme/opencode-formatter"

	root, manifestPath, lockfilePath, _, previous, _ := writeApplyOpenCodePluginCarrierCommandFixtureForScope(t, target.ScopeProject)
	claim := seedApplyCarrierClaim(
		t,
		root,
		manifestPath,
		previous,
		target.ScopeProject,
	)

	writeApplyFile(t, manifestPath, `
version = 1
targets = ["codex", "opencode"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex"]
`)
	instructionSource := filepath.Join(root, "instructions", "AGENTS.md")
	writeApplyFile(t, instructionSource, "managed instruction\n")
	writeApplyLockfile(t, lockfilePath, applyInstructionLockfile(
		t,
		"project",
		"local:instructions/AGENTS.md?mode=vendor",
		hashApplyPath(t, instructionSource),
		target.TargetCodex,
	))
	selectedPaths := writeOpenCodeRemovalConfigs(
		t,
		filepath.Join(root, ".opencode"),
		source,
	)

	prepared, err := PlanWrite(t.Context(), CommandInput{
		ManifestPath: manifestPath,
		LockfilePath: lockfilePath,
	})
	if err != nil {
		t.Fatalf("PlanWrite: %v", err)
	}
	if len(prepared.Reconciliation.ManagedPaths()) == 0 ||
		len(prepared.Reconciliation.CarrierAbsences()) != 1 {
		t.Fatalf(
			"reconciliation = managed paths %d carrier absences %d",
			len(prepared.Reconciliation.ManagedPaths()),
			len(prepared.Reconciliation.CarrierAbsences()),
		)
	}

	result, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{
		PlanWasDisclosed: true,
	})
	if err != nil {
		t.Fatalf("ExecuteWithOptions: %v", err)
	}
	if result.ActionCount != 2 {
		t.Fatalf("ActionCount = %d, want managed write plus direct removal", result.ActionCount)
	}
	assertApplyFileContent(t, filepath.Join(root, "AGENTS.md"), "managed instruction\n")
	assertOpenCodeSourceAbsent(t, selectedPaths, source)
	assertApplyCarrierClaimAbsent(t, root, manifestPath, claim)
}

func TestExecuteWithOptionsTreatsOpenCodeSourceReplacementAsExactAbsence(t *testing.T) {
	const replacement = "@acme/opencode-formatter@next"

	root, manifestPath, lockfilePath, _, previous, _ := writeApplyOpenCodePluginCarrierCommandFixtureForScope(t, target.ScopeProject)
	claim := seedApplyCarrierClaim(
		t,
		root,
		manifestPath,
		previous,
		target.ScopeProject,
	)
	writeApplyLockfile(t, lockfilePath, lock.File{Version: lock.CurrentVersion})
	configPaths := writeOpenCodeRemovalConfigs(
		t,
		filepath.Join(root, ".opencode"),
		replacement,
	)
	before := readFiles(t, configPaths)

	prepared, err := PlanWrite(t.Context(), CommandInput{
		ManifestPath: manifestPath,
		LockfilePath: lockfilePath,
		TargetValues: []string{"opencode"},
	})
	if err != nil {
		t.Fatalf("PlanWrite: %v", err)
	}
	absences := prepared.Reconciliation.CarrierAbsences()
	if len(absences) != 1 ||
		absences[0].Decision() != carrierabsence.DecisionRetireAlreadyAbsent ||
		!absences[0].StateOnly() {
		t.Fatalf("carrier absences = %#v, want exact-absence retirement", absences)
	}

	result, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{
		PlanWasDisclosed: true,
	})
	if err != nil {
		t.Fatalf("ExecuteWithOptions: %v", err)
	}
	if result.ActionCount != 1 {
		t.Fatalf("ActionCount = %d, want one state-only retirement", result.ActionCount)
	}
	after := readFiles(t, configPaths)
	for path, content := range before {
		if after[path] != content {
			t.Fatalf("replacement row in %q was mutated", path)
		}
	}
	assertOpenCodeSourcePresent(t, configPaths, replacement)
	assertApplyCarrierClaimAbsent(t, root, manifestPath, claim)
}

func TestRunDirectOpenCodeRemovalConvergesWithoutHostInvocation(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newOpenCodeDirectWorkflowFixture(t, scope)
			input := fixture.directInput(t)

			result, err := runCarrierRemovals(t.Context(), input)
			if err != nil {
				t.Fatalf("runCarrierRemovals: %v", err)
			}
			if result.ActionCount != 1 || len(result.Attempts) != 0 ||
				fixture.executorCalls != 0 {
				t.Fatalf(
					"result = actions %d attempts %d host calls %d",
					result.ActionCount,
					len(result.Attempts),
					fixture.executorCalls,
				)
			}
			assertOpenCodeSourceAbsent(t, fixture.configPaths, "@acme/remove")
			assertOpenCodeRetainedBytes(t, fixture.configPaths)
			if len(result.State.PendingCarrierRemovals()) != 0 ||
				len(result.State.ManagedCarrierClaims()) != 0 ||
				len(result.GlobalClaims.Claims()) != 0 {
				t.Fatalf(
					"claims/pending/global = %#v/%#v/%#v",
					result.State.ManagedCarrierClaims(),
					result.State.PendingCarrierRemovals(),
					result.GlobalClaims.Claims(),
				)
			}
		})
	}
}

func TestRunDirectOpenCodeRemovalConvergesFromOneMissingSelectedDocument(t *testing.T) {
	fixture := newOpenCodeDirectWorkflowFixture(t, target.ScopeProject)
	if err := os.Remove(fixture.configPaths[0]); err != nil {
		t.Fatal(err)
	}

	result, err := runCarrierRemovals(t.Context(), fixture.directInput(t))
	if err != nil {
		t.Fatalf("runCarrierRemovals: %v", err)
	}
	if result.ActionCount != 1 ||
		len(result.State.PendingCarrierRemovals()) != 0 ||
		len(result.State.ManagedCarrierClaims()) != 0 {
		t.Fatalf(
			"result = actions %d pending %#v claims %#v",
			result.ActionCount,
			result.State.PendingCarrierRemovals(),
			result.State.ManagedCarrierClaims(),
		)
	}
	if _, err := os.Stat(fixture.configPaths[0]); !os.IsNotExist(err) {
		t.Fatalf("missing selected document was created: %v", err)
	}
	assertOpenCodeSourceAbsent(t, fixture.configPaths[1:], "@acme/remove")
}

func writeOpenCodeRemovalConfigs(t *testing.T, directory string, source string) []string {
	t.Helper()
	paths := []string{
		filepath.Join(directory, "opencode.jsonc"),
		filepath.Join(directory, "tui.json"),
	}
	writeApplyFile(t, paths[0], "{\n  // retained server comment\n  \"plugin\": [\""+source+"\", \"@acme/keep\",],\n}\n")
	writeApplyFile(t, paths[1], "{\"plugin\":[[\""+source+"\", {\"flag\":true}], \"@acme/keep\"]}")
	return paths
}

func readFiles(t *testing.T, paths []string) map[string]string {
	t.Helper()
	result := make(map[string]string, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		result[path] = string(content)
	}
	return result
}

func TestRunDirectOpenCodeRemovalRetriesAfterPartialMultiFileFailure(t *testing.T) {
	fixture := newOpenCodeDirectWorkflowFixture(t, target.ScopeProject)
	input := fixture.directInput(t)
	injected := errors.New("injected second config replacement failure")
	input.Filesystem = &failNthReplaceStore{
		Adapter: storagecommit.Adapter{},
		err:     injected,
		failOn:  3,
	}

	first, err := runCarrierRemovals(t.Context(), input)
	if !errors.Is(err, injected) {
		t.Fatalf("first run error = %v, want injected failure", err)
	}
	pending := first.State.PendingCarrierRemovals()
	if len(pending) != 1 ||
		len(first.State.ManagedCarrierClaims()) != 1 ||
		first.ActionCount != 0 {
		t.Fatalf(
			"partial state = pending %#v claims %#v actions %d",
			pending,
			first.State.ManagedCarrierClaims(),
			first.ActionCount,
		)
	}
	assertOpenCodeSourceAbsent(t, fixture.configPaths[:1], "@acme/remove")
	assertOpenCodeSourcePresent(t, fixture.configPaths[1:], "@acme/remove")

	fixture.current = first.State
	fixture.action = fixture.pendingDirectAction(t, pending[0])
	retry := fixture.directInput(t)
	second, err := runCarrierRemovals(t.Context(), retry)
	if err != nil {
		t.Fatalf("retry run: %v", err)
	}
	assertOpenCodeSourceAbsent(t, fixture.configPaths, "@acme/remove")
	if second.ActionCount != 1 ||
		len(second.State.PendingCarrierRemovals()) != 0 ||
		len(second.State.ManagedCarrierClaims()) != 0 {
		t.Fatalf(
			"retry state = actions %d pending %#v claims %#v",
			second.ActionCount,
			second.State.PendingCarrierRemovals(),
			second.State.ManagedCarrierClaims(),
		)
	}
}

func TestRunDirectOpenCodeRemovalRetainsPendingAfterMalformedSecondDocument(t *testing.T) {
	fixture := newOpenCodeDirectWorkflowFixture(t, target.ScopeProject)
	malformed := []byte(`{"plugin":["@acme/remove",}`)
	if err := os.WriteFile(fixture.configPaths[1], malformed, 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := runCarrierRemovals(t.Context(), fixture.directInput(t))
	if err == nil || !strings.Contains(err.Error(), "parse OpenCode config JSONC") {
		t.Fatalf("first run error = %v, want malformed JSONC refusal", err)
	}
	pending := first.State.PendingCarrierRemovals()
	if len(pending) != 1 ||
		len(first.State.ManagedCarrierClaims()) != 1 ||
		first.ActionCount != 0 {
		t.Fatalf(
			"partial state = pending %#v claims %#v actions %d",
			pending,
			first.State.ManagedCarrierClaims(),
			first.ActionCount,
		)
	}
	assertOpenCodeSourceAbsent(t, fixture.configPaths[:1], "@acme/remove")
	gotMalformed, readErr := os.ReadFile(fixture.configPaths[1])
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(gotMalformed) != string(malformed) {
		t.Fatalf("malformed document changed to %q", gotMalformed)
	}

	if err := os.WriteFile(
		fixture.configPaths[1],
		[]byte(`{"plugin":["@acme/remove","@acme/keep"]}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	fixture.current = first.State
	fixture.action = fixture.pendingDirectAction(t, pending[0])
	second, err := runCarrierRemovals(t.Context(), fixture.directInput(t))
	if err != nil {
		t.Fatalf("retry run: %v", err)
	}
	assertOpenCodeSourceAbsent(t, fixture.configPaths, "@acme/remove")
	if second.ActionCount != 1 ||
		len(second.State.PendingCarrierRemovals()) != 0 ||
		len(second.State.ManagedCarrierClaims()) != 0 {
		t.Fatalf(
			"retry state = actions %d pending %#v claims %#v",
			second.ActionCount,
			second.State.PendingCarrierRemovals(),
			second.State.ManagedCarrierClaims(),
		)
	}
}

func TestRunDirectOpenCodeRemovalRetainsPendingWhenCanceledBetweenDocuments(t *testing.T) {
	fixture := newOpenCodeDirectWorkflowFixture(t, target.ScopeProject)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	input := fixture.directInput(t)
	input.Filesystem = &cancelAfterNthReplaceStore{
		Adapter:     storagecommit.Adapter{},
		cancel:      cancel,
		cancelAfter: 2,
	}

	first, err := runCarrierRemovals(ctx, input)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("first run error = %v, want context cancellation", err)
	}
	pending := first.State.PendingCarrierRemovals()
	if len(pending) != 1 ||
		len(first.State.ManagedCarrierClaims()) != 1 ||
		first.ActionCount != 0 {
		t.Fatalf(
			"canceled state = pending %#v claims %#v actions %d",
			pending,
			first.State.ManagedCarrierClaims(),
			first.ActionCount,
		)
	}
	assertOpenCodeSourceAbsent(t, fixture.configPaths[:1], "@acme/remove")
	assertOpenCodeSourcePresent(t, fixture.configPaths[1:], "@acme/remove")

	fixture.current = first.State
	fixture.action = fixture.pendingDirectAction(t, pending[0])
	second, err := runCarrierRemovals(t.Context(), fixture.directInput(t))
	if err != nil {
		t.Fatalf("retry run: %v", err)
	}
	assertOpenCodeSourceAbsent(t, fixture.configPaths, "@acme/remove")
	if second.ActionCount != 1 ||
		len(second.State.PendingCarrierRemovals()) != 0 ||
		len(second.State.ManagedCarrierClaims()) != 0 {
		t.Fatalf(
			"retry state = actions %d pending %#v claims %#v",
			second.ActionCount,
			second.State.PendingCarrierRemovals(),
			second.State.ManagedCarrierClaims(),
		)
	}
}

func TestDirectRemovalRequiresProjectRootAndExclusiveRelationAuthority(t *testing.T) {
	fixture := newOpenCodeDirectWorkflowFixture(t, target.ScopeProject)
	reconciliation, err := reconcile.NewResult(reconcile.ResultInput{
		Context:         reconcile.ContextApply,
		CarrierAbsences: []carrierabsence.Action{fixture.action},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !requiresProjectRootAuthority(commandPlan{
		assessment: readiness.Assessment{Reconciliation: reconciliation},
	}) {
		t.Fatal("direct config removal did not retain project-root authority")
	}

	foreign := newRelationAuthorityPath(
		t,
		filepath.Join(fixture.root, "foreign.json"),
		target.ScopeGlobal,
	)
	facts := relationAuthorityPathFacts(
		[]carrierabsence.Action{fixture.action},
		append(fixture.authorityPaths, foreign),
	)
	if len(facts) != 5 {
		t.Fatalf("authority facts = %#v", facts)
	}
	for index, fact := range facts {
		want := mutation.AccessExclusive
		if index == len(facts)-1 {
			want = mutation.AccessShared
		}
		if fact.access != want {
			t.Fatalf("authority fact[%d] access = %q, want %q", index, fact.access, want)
		}
	}
}

func newOpenCodeDirectWorkflowFixture(
	t *testing.T,
	scope target.Scope,
) openCodeDirectWorkflowFixture {
	t.Helper()
	fixture := newWorkflowFixture(t, scope)
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindHostSource,
		"@acme/remove",
	)
	if err != nil {
		t.Fatal(err)
	}
	carrierKey, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierOpenCodePlugin,
		target.TargetOpenCode,
		scope,
		source,
	)
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := extensiontopology.NewCarrier(carrierKey)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"opencode.plugin-carrier",
		"tools",
	)
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(source.Ref())
	if err != nil {
		t.Fatal(err)
	}
	expected, err := hostrelation.Derive(carrierKey, subject, subjectKey)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := durablecarrier.NewManagedCarrierIdentity(carrier, subject, expected)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(
		fixture.claim.Owner(),
		identity,
		workflowRequest(t, "opencode.install", "opencode-install-v1", "install"),
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatal(err)
	}
	occupancy, err := durablecarrier.NewCarrierOccupancy(
		carrier,
		[]durablecarrier.ManagedCarrierClaim{claim},
	)
	if err != nil {
		t.Fatal(err)
	}
	removeRequest := workflowRequest(
		t,
		"opencode.plugin-config-relation.remove",
		"opencode-plugin-config-relation-v1",
		"remove",
	)
	operation, err := lock.NewOperationContract(lock.OperationContractInput{
		Operation:       lock.OperationRemove,
		Actuation:       lock.ActuationDirectProjection,
		Authority:       lock.AuthorityRemove,
		Route:           lock.RouteContractRef{RouteID: removeRequest.RouteID(), AdapterContractVersion: removeRequest.ContractVersion()},
		EffectEnvelope:  lock.EffectEnvelopeComplete,
		Idempotency:     lock.ConditionallyIdempotent,
		Verification:    lock.VerificationHostRelation,
		TrustActivation: lock.TrustActivationNotRequired,
		Recovery:        lock.OperationRecoverySafeRetry,
	})
	if err != nil {
		t.Fatal(err)
	}
	route, err := carrierabsence.NewRouteAdmission(carrierabsence.RouteAdmissionInput{
		Operation:       operation,
		Request:         removeRequest,
		RemovedEffects:  []string{"selected_opencode_plugin_config_relation"},
		RetainedEffects: []string{"package_manager_installations"},
		NonClaims:       []string{"package_or_dependency_uninstall"},
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := observerelation.NewCorrelationKey(subject, expected)
	if err != nil {
		t.Fatal(err)
	}
	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:       claim,
		Desired:     carrierabsence.DesiredAbsent,
		Observation: observerelation.Correlation{Key: key, Result: exactCorrelation(t, expected)},
		Occupancy:   occupancy,
		Route:       route,
	})
	if err != nil {
		t.Fatal(err)
	}
	current := durable.EmptySnapshot()
	globalClaims := durablecarrier.EmptyGlobalCarrierClaims()
	if scope == target.ScopeProject {
		current, err = durable.NewSnapshot(durable.SnapshotInput{
			ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{claim},
		})
	} else {
		globalClaims, err = durablecarrier.NewGlobalCarrierClaims(
			[]durablecarrier.ManagedCarrierClaim{claim},
		)
	}
	if err != nil {
		t.Fatal(err)
	}
	content, err := (statefile.Codec{}).Encode(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.statePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	configRoot := filepath.Join(fixture.root, ".opencode")
	if scope == target.ScopeGlobal {
		configRoot = filepath.Join(fixture.root, "global", "opencode")
	}
	if err := os.MkdirAll(configRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	configPaths := []string{
		filepath.Join(configRoot, "opencode.jsonc"),
		filepath.Join(configRoot, "tui.json"),
	}
	if err := os.WriteFile(
		configPaths[0],
		[]byte("{\n  // server\n  \"plugin\": [\"@acme/remove\", \"@acme/keep\",],\n}\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		configPaths[1],
		[]byte("{\"plugin\":[[\"@acme/remove\", {\"flag\":true}], \"@acme/keep\"]}"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	authorityPaths := make([]observerelation.AuthorityPath, 0, 4)
	for _, name := range []string{
		"opencode.json",
		"opencode.jsonc",
		"tui.json",
		"tui.jsonc",
	} {
		authorityPaths = append(
			authorityPaths,
			newRelationAuthorityPath(t, filepath.Join(configRoot, name), scope),
		)
	}

	fixture.action = action
	fixture.claim = claim
	fixture.expected = expected
	fixture.current = current
	fixture.globalClaims = globalClaims
	fixture.removeRequest = removeRequest
	return openCodeDirectWorkflowFixture{
		workflowFixture: fixture,
		configPaths:     configPaths,
		authorityPaths:  authorityPaths,
	}
}

func (fixture openCodeDirectWorkflowFixture) directInput(
	t *testing.T,
) carrierRemovalInput {
	t.Helper()
	input := fixture.input(t)
	input.RelationAuthorityPaths = fixture.authorityPaths
	input.Observer = func(
		_ context.Context,
		_ durablecarrier.PendingCarrierRemoval,
		_ []durablecarrier.ManagedCarrierClaim,
	) assurancehostroute.ObservationFact {
		for _, path := range fixture.configPaths {
			content, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				t.Fatal(err)
			}
			if strings.Contains(string(content), `"@acme/remove"`) {
				return assurancehostroute.CurrentObservation(
					exactCorrelation(t, fixture.expected),
				)
			}
		}
		return assurancehostroute.CurrentObservation(
			missingCorrelation(t, fixture.expected),
		)
	}
	return input
}

func (fixture openCodeDirectWorkflowFixture) pendingDirectAction(
	t *testing.T,
	pending durablecarrier.PendingCarrierRemoval,
) carrierabsence.Action {
	t.Helper()
	observation, present := fixture.action.Observation()
	if !present {
		t.Fatal("direct action has no correlation key")
	}
	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:       fixture.claim,
		Desired:     carrierabsence.DesiredAbsent,
		Observation: observerelation.Correlation{Key: observation.Key, Result: exactCorrelation(t, fixture.expected)},
		Occupancy:   fixture.action.Occupancy(),
		Route:       fixture.action.RouteAdmission(),
		Pending:     &pending,
	})
	if err != nil {
		t.Fatal(err)
	}
	return action
}

func newRelationAuthorityPath(
	t *testing.T,
	path string,
	scope target.Scope,
) observerelation.AuthorityPath {
	t.Helper()
	authorityPath, err := observerelation.NewAuthorityPath(
		path,
		target.TargetOpenCode,
		scope,
	)
	if err != nil {
		t.Fatal(err)
	}
	return authorityPath
}

func assertOpenCodeSourceAbsent(t *testing.T, paths []string, source string) {
	t.Helper()
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), `"`+source+`"`) {
			t.Fatalf("source %q remains in %q: %s", source, path, content)
		}
	}
}

func assertOpenCodeSourcePresent(t *testing.T, paths []string, source string) {
	t.Helper()
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), `"`+source+`"`) {
			t.Fatalf("source %q is absent from %q: %s", source, path, content)
		}
	}
}

func assertOpenCodeRetainedBytes(t *testing.T, paths []string) {
	t.Helper()
	server, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(server), "// server") ||
		!strings.Contains(string(server), `"@acme/keep"`) {
		t.Fatalf("server retained bytes were lost: %s", server)
	}
	tui, err := os.ReadFile(paths[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tui), `"@acme/keep"`) {
		t.Fatalf("TUI retained bytes were lost: %s", tui)
	}
}
