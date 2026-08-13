package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/statefile"
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/topology"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestClaudeGlobalExtensionCarrierApplyDryRunHumanJSONParity(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", claudeGlobalExtensionManifest())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("lock exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}

	missingInventory := mustCLIClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	subjectID, carrierSubject := mustCLIClaudeCarrierSubjectFromLockfile(t, filepath.Join(tempDir, "daem.lock.toml"))
	missingObservations := mustCLIClaudeObservationBatch(t, subjectID, carrierSubject, missingInventory)

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLIWithOptions(
		[]string{"apply", "--manifest", manifestPath, "--dry-run", "--json"},
		clipkg.RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			ApplyExecuteOptions: applyworkflow.ExecuteOptions{
				RelationObservations: &missingObservations,
			},
		},
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("apply --dry-run --json exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	jsonPayload := clijson.DecodePlan(t, stdout.Bytes())
	if len(jsonPayload.HostRouteAttempts) != 0 {
		t.Fatalf("json host_route_attempts = %#v, want none for dry-run", jsonPayload.HostRouteAttempts)
	}
	assertCLIHostRouteDryRunDisclosure(t, jsonPayload.RelationActions, hostRouteDisclosureExpectation{
		namespace: "claude-code.plugin-carrier",
		name:      "context7-global",
		target:    "claude-code",
		scope:     "global",
		routeID:   claudePluginRoute(t).RouteID(),
	})
	assertNoHostUserScopeLeak(t, stdout.String())
	assertNoCarrierInstallConvergenceClaims(t, stdout.String())

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLIWithOptions(
		[]string{"apply", "--manifest", manifestPath, "--dry-run"},
		clipkg.RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			ApplyExecuteOptions: applyworkflow.ExecuteOptions{
				RelationObservations: &missingObservations,
			},
		},
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("apply --dry-run human exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	humanOutput := stdout.String()
	for _, want := range []string{
		"dry-run: 0 actions",
		"relation actions: 1 subjects",
		"kind=create",
		"subject=\"host_relation/" + "claude-code.plugin-carrier" + "/context7-global\"",
		"target=claude-code",
		"scope=global",
		"source_kind=\"marketplace\"",
		"source_ref=\"context7@market\"",
		"source_namespace=\"marketplace:context7@market\"",
		"relation_subject_key=\"context7@market\"",
		"evidence_source=passive_relation_inventory",
		"evidence_availability=supported",
		"evidence_freshness=fresh",
		"execution=host_route",
		"correlation_state=missing",
		"correlation_reason=managed_relation_missing",
		"route_id=\"" + claudePluginRoute(t).RouteID() + "\"",
		"route_request_hash=\"sha256:",
		"route_admission_row=\"RA-01\"",
		"requested_outcome=ordinary-mutation",
		"selected_outcome=host-delegated",
		"replay_boundary=locked_route_request_identity_only",
		"retained_effects=",
		"non_claims=",
		"exact_artifact_replay",
		"package_cache_convergence",
		"runtime_readiness",
		"future_skip_authority",
		"invokes_host_route=true",
		"allows_host_route_invocation=true",
		"blocks_ordinary_apply=false",
	} {
		if !strings.Contains(humanOutput, want) {
			t.Fatalf("human dry-run output = %q, want %q", humanOutput, want)
		}
	}
	if strings.Contains(humanOutput, "host route attempts:") {
		t.Fatalf("human dry-run output = %q, want no host route attempt diagnostics", humanOutput)
	}
	assertNoHostUserScopeLeak(t, humanOutput)
	assertNoCarrierInstallConvergenceClaims(t, humanOutput)
}

func TestClaudeExtensionCarrierPublicCLIApplyDryRunDisclosesWithoutDelegating(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", claudeExtensionManifest())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("lock write exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	locked, err := lockfile.Load(t.Context(), lockfilePath)
	if err != nil {
		t.Fatalf("load lockfile: %v", err)
	}
	if len(locked.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one", locked.Locked.Subjects())
	}
	record := locked.Locked.Subjects()[0]
	subject := testkit.LockedDelegatedRelation(t, record)

	missingInventory := mustCLIClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	missingObservations := mustCLIClaudeObservationBatch(t, record.SubjectID(), subject, missingInventory)
	var requests []subprocess.CommandRequest
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			requests = append(requests, request)
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})
	observer := func(ctx context.Context, command executehostroute.Command, _ []durablecarrier.PendingCarrierInstall, _ []durablecarrier.ManagedCarrierClaim) assurancehostroute.ObservationFact {
		inventory := mustCLIClaudePluginInventory(t, observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows: []observeclaudeplugin.Row{
				mustCLIClaudePluginManagedRowWithScope(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeProject),
			},
		})
		return assurancehostroute.CurrentObservation(observeclaudeplugin.Correlate(subject, inventory))
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLIWithOptions(
		[]string{"apply", "--manifest", manifestPath, "--dry-run", "--json"},
		clipkg.RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			ApplyExecuteOptions: applyworkflow.ExecuteOptions{
				RelationObservations: &missingObservations,
				HostRouteExecutor:    executor,
			},
		},
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("apply --dry-run --json exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if len(requests) != 0 {
		t.Fatalf("dry-run invoked host route executor: %#v", requests)
	}
	payload := clijson.DecodePlan(t, stdout.Bytes())
	if len(payload.HostRouteAttempts) != 0 {
		t.Fatalf("host_route_attempts = %#v, want none for dry-run", payload.HostRouteAttempts)
	}
	if len(payload.RelationActions) != 1 ||
		!payload.RelationActions[0].InvokesHostRoute ||
		payload.RelationActions[0].Execution != "host_route" ||
		payload.RelationActions[0].Kind != "create" {
		t.Fatalf("relation_actions = %#v, want disclosed host route create", payload.RelationActions)
	}
	assertNoCarrierInstallConvergenceClaims(t, stdout.String())

	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	if _, err := os.Stat(statefilePath); !os.IsNotExist(err) {
		t.Fatalf("dry-run created statefile or stat failed: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLIWithOptions(
		[]string{"apply", "--manifest", manifestPath, "--yes", "--json"},
		clipkg.RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			ApplyExecuteOptions: applyworkflow.ExecuteOptions{
				RelationObservations: &missingObservations,
				HostRouteExecutor:    executor,
				HostRouteObserver:    observer,
			},
		},
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("apply --yes --json after dry-run exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if len(requests) != 1 ||
		requests[0].Command != "claude" ||
		!slices.Equal(requests[0].Args, []string{"plugin", "install", "context7@market", "--scope", "project"}) ||
		requests[0].WorkDir != tempDir {
		t.Fatalf("host route requests after dry-run = %#v, want one Claude project install request", requests)
	}
	applyPayload := clijson.DecodeApplyResult(t, stdout.Bytes())
	assertCLIClaudeHostRouteAttemptJSON(t, applyPayload.HostRouteAttempts, "context7-managed", "project", "attempted_observed_present", "observed_present")
	stateAfterApply, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("load statefile after mutating apply: %v", err)
	}
	attempts := stateAfterApply.HostRouteAttempts()
	if len(attempts) != 1 {
		t.Fatalf("persisted host route attempts = %#v, want one after mutating apply", attempts)
	}
}

type hostRouteDisclosureExpectation struct {
	namespace string
	name      string
	target    string
	scope     string
	routeID   string
	attempt   bool
}

func assertCLIHostRouteDryRunDisclosure(
	t *testing.T,
	actions []clijson.RelationAction,
	want hostRouteDisclosureExpectation,
) {
	t.Helper()
	if len(actions) != 1 {
		t.Fatalf("relation_actions = %#v, want one disclosed host route action", actions)
	}
	action := actions[0]
	wantKind := "create"
	wantAvailability := "supported"
	wantState := "missing"
	wantCorrelationReason := "managed_relation_missing"
	wantReason := ""
	if want.attempt {
		wantKind = "attempt"
		wantAvailability = "unsupported"
		wantState = "unsupported"
		wantCorrelationReason = "unsupported_passive_inventory"
		wantReason = "unsupported_passive_inventory"
	}
	if action.Subject == nil ||
		action.Kind != wantKind ||
		action.Subject.Kind != string(topology.SubjectHostRelation) ||
		action.Subject.Namespace != want.namespace ||
		action.Subject.Name != want.name ||
		action.Target != want.target ||
		action.Scope != want.scope ||
		action.EvidenceAvailability != wantAvailability ||
		action.EvidenceFreshness != "fresh" ||
		action.RouteID != want.routeID ||
		!strings.HasPrefix(action.RouteRequestHash, "sha256:") ||
		action.RouteAdmissionRow != "RA-01" ||
		action.RequestedOutcome != "ordinary-mutation" ||
		action.SelectedOutcome != "host-delegated" ||
		action.Execution != "host_route" ||
		action.CorrelationState != wantState ||
		action.CorrelationReason != wantCorrelationReason ||
		action.Reason != wantReason ||
		!action.InvokesHostRoute ||
		!action.AllowsHostRouteInvocation ||
		action.BlocksOrdinaryApply {
		t.Fatalf("relation_action = %#v, want disclosed host route action %#v", action, want)
	}
	if !slices.Contains(action.NonClaims, "future_skip_authority") ||
		!slices.Contains(action.NonClaims, "package_cache_convergence") ||
		!slices.Contains(action.NonClaims, "runtime_readiness") {
		t.Fatalf("non_claims = %#v, want carrier route non-claims", action.NonClaims)
	}
}

func assertCLIHostRouteConvergedDisclosure(
	t *testing.T,
	actions []clijson.RelationAction,
	want hostRouteDisclosureExpectation,
) {
	t.Helper()
	if len(actions) != 1 {
		t.Fatalf("relation_actions = %#v, want one converged host route action", actions)
	}
	action := actions[0]
	if action.Subject == nil ||
		action.Kind != "no_op" ||
		action.Subject.Kind != string(topology.SubjectHostRelation) ||
		action.Subject.Namespace != want.namespace ||
		action.Subject.Name != want.name ||
		action.Target != want.target ||
		action.Scope != want.scope ||
		action.EvidenceAvailability != "supported" ||
		action.EvidenceFreshness != "fresh" ||
		action.RouteID != want.routeID ||
		!strings.HasPrefix(action.RouteRequestHash, "sha256:") ||
		action.RouteAdmissionRow != "RA-01" ||
		action.RequestedOutcome != "ordinary-mutation" ||
		action.SelectedOutcome != "host-delegated" ||
		action.Execution != "no_mutation" ||
		action.CorrelationState != "exact_correlation" ||
		action.CorrelationReason != "" ||
		action.Reason != "" ||
		action.InvokesHostRoute ||
		!action.AllowsHostRouteInvocation ||
		action.BlocksOrdinaryApply {
		t.Fatalf("relation_action = %#v, want converged host route action %#v", action, want)
	}
}
