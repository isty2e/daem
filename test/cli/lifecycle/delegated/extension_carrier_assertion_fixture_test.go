package cli_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func assertClaudeCallCount(t *testing.T, path string, want int) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Claude call log: %v", err)
	}
	if got := strings.Count(string(content), "called\n"); got != want {
		t.Fatalf("Claude calls = %d, want %d; log=%q", got, want, content)
	}
}

func assertCLIClaudeGlobalHostRouteRequest(t *testing.T, root string, requests []subprocess.CommandRequest) {
	t.Helper()
	if len(requests) != 1 {
		t.Fatalf("host route requests = %#v, want one", requests)
	}
	request := requests[0]
	if request.Command != "claude" ||
		!slices.Equal(request.Args, []string{"plugin", "install", "context7@market", "--scope", "user"}) ||
		request.WorkDir != root {
		t.Fatalf("host route request = %#v, want claude plugin install context7@market --scope user in %q", request, root)
	}
}

func assertCLIClaudeExtensionLockedSubject(t *testing.T, lockfilePath string) {
	t.Helper()
	assertCLIClaudeExtensionLockedSubjectWithScope(t, lockfilePath, "context7-managed", "project")
}

func assertCLIClaudeExtensionLockedSubjectWithScope(t *testing.T, lockfilePath string, declarationID string, scope string) {
	t.Helper()
	locked, err := lockfile.Load(t.Context(), lockfilePath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(locked.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one", locked.Locked.Subjects())
	}
	record := locked.Locked.Subjects()[0]
	subjectID := record.SubjectID()
	if subjectID.Kind() != topology.SubjectHostRelation ||
		subjectID.Namespace() != "claude-code.plugin-carrier" ||
		subjectID.Key() != declarationID {
		t.Fatalf("locked subject = %#v, want Claude plugin carrier subject", subjectID)
	}
	relation := snapshottest.DelegatedRelation(t, record)
	source, err := desiredextension.ParseSourceRef(relation.SourceNamespace())
	if err != nil {
		t.Fatalf("ParseSourceRef returned error: %v", err)
	}
	if relation.Target() != target.TargetClaudeCode ||
		relation.Scope() != target.Scope(scope) ||
		source.Ref() != "context7@market" ||
		string(relation.ExpectedRelation().SubjectKey()) != "context7@market" {
		t.Fatalf("relation = %#v, want context7 Claude marketplace relation", relation)
	}
	if _, ok := record.DelegatePlan(); ok {
		t.Fatal("Claude extension lock unexpectedly carries delegate plan")
	}
}

func assertClaudeExtensionCarrierMissingCreateAction(t *testing.T, actions []clijson.RelationAction) {
	t.Helper()
	assertClaudeExtensionCarrierMissingCreateActionWithScope(t, actions, "context7-managed", "project")
}

func assertClaudeExtensionCarrierMissingCreateActionWithScope(t *testing.T, actions []clijson.RelationAction, declarationID string, scope string) {
	t.Helper()
	if len(actions) != 1 {
		t.Fatalf("relation_actions = %#v, want one", actions)
	}
	action := actions[0]
	if action.Subject == nil ||
		action.Subject.Kind != string(topology.SubjectHostRelation) ||
		action.Subject.Namespace != "claude-code.plugin-carrier" ||
		action.Subject.Name != declarationID ||
		action.Kind != "create" ||
		action.Target != "claude-code" ||
		action.Scope != scope ||
		action.SourceNamespace != "marketplace:context7@market" ||
		action.SourceKind != "marketplace" ||
		action.SourceRef != "context7@market" ||
		action.RelationSubjectKey != "context7@market" ||
		action.EvidenceSource != "passive_relation_inventory" ||
		action.EvidenceAvailability != "supported" ||
		action.EvidenceFreshness != "fresh" ||
		action.RouteID != "claude-code.plugin-carrier.install" ||
		!strings.HasPrefix(action.RouteRequestHash, "sha256:") ||
		action.RouteAdmissionRow != "RA-01" ||
		action.RequestedOutcome != "ordinary-mutation" ||
		action.SelectedOutcome != "host-delegated" ||
		action.CorrelationState != "missing" ||
		action.CorrelationReason != "managed_relation_missing" ||
		action.Reason != "" ||
		action.Execution != "host_route" ||
		action.ReplayBoundary != "locked_route_request_identity_only" ||
		!action.InvokesHostRoute ||
		!action.AllowsHostRouteInvocation ||
		action.BlocksOrdinaryApply {
		t.Fatalf("carrier action = %#v, want missing Claude plugin create action", action)
	}
	if len(action.Watchpoints) != 0 {
		t.Fatalf("watchpoints = %#v, want none for fresh missing inventory", action.Watchpoints)
	}
	assertRelationDisclosureSlice(t, "retained effects", action.RetainedEffects, []string{
		"host_selected_artifacts",
		"provider_contributions",
		"package_cache",
		"credentials",
		"trust_session_state",
		"runtime_state",
		"logs",
	})
	assertRelationDisclosureSlice(t, "non claims", action.NonClaims, []string{
		"exact_artifact_replay",
		"current_contribution_inventory",
		"runtime_readiness",
		"tool_inventory",
		"auth_trust_state",
		"package_cache_convergence",
		"carrier_removal",
		"destructive_cleanup",
		"future_skip_authority",
	})
}

func assertLockJSONClaudePluginDelegatedRelation(t *testing.T, payload clijson.Lock, declarationID string, scope string) {
	t.Helper()
	if len(payload.SubjectChanges) != 1 {
		t.Fatalf("subject_changes = %#v, want one", payload.SubjectChanges)
	}
	change := payload.SubjectChanges[0]
	if change.Status != "added" ||
		change.Subject.Kind != string(topology.SubjectHostRelation) ||
		change.Subject.Namespace != "claude-code.plugin-carrier" ||
		change.Subject.Name != declarationID ||
		change.After == nil ||
		change.After.Realization == nil {
		t.Fatalf("subject change = %#v, want added Claude delegated relation", change)
	}
	relation := change.After.Realization
	if relation.Kind != string(realization.RealizationDelegatedRelation) ||
		relation.Target != "claude-code" ||
		relation.Scope != scope ||
		relation.SourceNamespace != "marketplace:context7@market" ||
		relation.RelationSubjectKey != "context7@market" ||
		!strings.HasPrefix(relation.ManagedInstanceKey, "host-relation:v1:") ||
		relation.RouteID != claudePluginRoute(t).RouteID() ||
		relation.RouteContractVersion != claudePluginRoute(t).AdapterContractVersion() ||
		!strings.HasPrefix(relation.CanonicalRequestHash, "sha256:") ||
		!slices.Contains(relation.VerifiedRelationFields, "scope") ||
		!slices.Contains(relation.VerifiedRelationFields, "source_kind") ||
		!slices.Contains(relation.VerifiedRelationFields, "source_ref") ||
		!slices.Contains(relation.VerifiedRelationFields, "target") {
		t.Fatalf("delegated relation realization = %#v, want public Claude marketplace relation with scope %q", relation, scope)
	}
}

func assertCLIClaudeHostRouteAttemptJSON(
	t *testing.T,
	attempts []clijson.HostRouteAttempt,
	declarationID string,
	scope string,
	wantClass string,
	wantReason string,
) {
	t.Helper()
	if len(attempts) != 1 {
		t.Fatalf("host_route_attempts = %#v, want one", attempts)
	}
	attempt := attempts[0]
	if attempt.EvidenceKind != "host_route_attempt_diagnostics" ||
		attempt.Authority != "history_only" ||
		attempt.Subject.Kind != string(topology.SubjectHostRelation) ||
		attempt.Subject.Namespace != "claude-code.plugin-carrier" ||
		attempt.Subject.Name != declarationID ||
		attempt.Target != "claude-code" ||
		attempt.Scope != scope ||
		attempt.RouteID != "claude-code.plugin-carrier.install" ||
		!strings.HasPrefix(attempt.RouteRequestHash, "sha256:") ||
		attempt.ResultClass != wantClass ||
		attempt.Reason != wantReason ||
		!attempt.AttemptObserved ||
		attempt.GrantsApplySkipAuthority {
		t.Fatalf("host_route_attempt = %#v, want %s/%s history-only diagnostic", attempt, wantClass, wantReason)
	}
	if !slices.Contains(attempt.NonClaims, "future_skip_authority") ||
		!slices.Contains(attempt.NonClaims, "package_cache_convergence") ||
		!slices.Contains(attempt.NonClaims, "runtime_readiness") {
		t.Fatalf("non_claims = %#v, want retained-effect non-claims", attempt.NonClaims)
	}
}

func assertCLIHostRouteAttemptObservedPresentCommandSuccessJSON(t *testing.T, attempt clijson.HostRouteAttempt) {
	t.Helper()
	if attempt.AttemptReason != "" ||
		attempt.ExitCode == nil ||
		*attempt.ExitCode != 0 ||
		attempt.TimedOut ||
		attempt.Redacted ||
		attempt.Observation != "present" ||
		attempt.Postcondition != "observed" {
		t.Fatalf("host_route_attempt = %#v, want observed-present command success diagnostic", attempt)
	}
}

func assertCLIHostRouteAttemptAttemptedUnverifiedCommandSuccessJSON(t *testing.T, attempt clijson.HostRouteAttempt) {
	t.Helper()
	if attempt.AttemptReason != "" ||
		attempt.ExitCode == nil ||
		*attempt.ExitCode != 0 ||
		attempt.TimedOut ||
		attempt.Redacted ||
		attempt.Observation != "not_observed" ||
		attempt.Postcondition != "unknown" {
		t.Fatalf("host_route_attempt = %#v, want attempted-unverified command success diagnostic", attempt)
	}
}

func assertCLIHostRouteAttemptObservedAbsentCommandSuccessJSON(t *testing.T, attempt clijson.HostRouteAttempt) {
	t.Helper()
	if attempt.AttemptReason != "" ||
		attempt.ExitCode == nil ||
		*attempt.ExitCode != 0 ||
		attempt.TimedOut ||
		attempt.Redacted ||
		attempt.Observation != "missing" ||
		attempt.Postcondition != "missing" {
		t.Fatalf("host_route_attempt = %#v, want observed-absent command success diagnostic", attempt)
	}
}

func assertCLICodexHostRouteAttemptJSON(
	t *testing.T,
	attempts []clijson.HostRouteAttempt,
	wantClass string,
	wantReason string,
) {
	t.Helper()
	if len(attempts) != 1 {
		t.Fatalf("host_route_attempts = %#v, want one", attempts)
	}
	attempt := attempts[0]
	if attempt.EvidenceKind != "host_route_attempt_diagnostics" ||
		attempt.Authority != "history_only" ||
		attempt.Subject.Kind != string(topology.SubjectHostRelation) ||
		attempt.Subject.Namespace != "codex.plugin-carrier" ||
		attempt.Subject.Name != "documents-managed" ||
		attempt.Target != "codex" ||
		attempt.Scope != "global" ||
		attempt.RouteID != "codex.plugin-carrier.install" ||
		!strings.HasPrefix(attempt.RouteRequestHash, "sha256:") ||
		attempt.ResultClass != wantClass ||
		attempt.Reason != wantReason ||
		!attempt.AttemptObserved ||
		attempt.GrantsApplySkipAuthority {
		t.Fatalf("host_route_attempt = %#v, want %s/%s Codex history-only diagnostic", attempt, wantClass, wantReason)
	}
	if !slices.Contains(attempt.NonClaims, "future_skip_authority") ||
		!slices.Contains(attempt.NonClaims, "package_cache_convergence") ||
		!slices.Contains(attempt.NonClaims, "runtime_readiness") {
		t.Fatalf("non_claims = %#v, want retained-effect non-claims", attempt.NonClaims)
	}
}

func mustCLIClaudePluginInventory(
	t *testing.T,
	spec observeclaudeplugin.InventorySpec,
) observeclaudeplugin.Inventory {
	t.Helper()
	inventory, err := observeclaudeplugin.NewInventory(spec)
	if err != nil {
		t.Fatalf("NewInventory returned error: %v", err)
	}
	return inventory
}

func mustCLIClaudeObservationBatch(
	t *testing.T,
	subjectID topology.SubjectID,
	subject realization.DelegatedRelation,
	inventory observeclaudeplugin.Inventory,
) relationobserve.Batch {
	t.Helper()
	key, err := relationobserve.NewCorrelationKey(
		subjectID,
		subject.ExpectedRelation(),
	)
	if err != nil {
		t.Fatalf("relationobserve.NewCorrelationKey returned error: %v", err)
	}
	batch, err := relationobserve.NewBatch(relationobserve.BatchSpec{
		Correlations: []relationobserve.Correlation{{
			Key:    key,
			Result: observeclaudeplugin.Correlate(subject, inventory),
		}},
	})
	if err != nil {
		t.Fatalf("relationobserve.NewBatch returned error: %v", err)
	}
	return batch
}

func mustCLIClaudeCarrierSubjectFromLockfile(
	t *testing.T,
	lockfilePath string,
) (topology.SubjectID, realization.DelegatedRelation) {
	t.Helper()
	locked, err := lockfile.Load(t.Context(), lockfilePath)
	if err != nil {
		t.Fatalf("load lockfile: %v", err)
	}
	if len(locked.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one Claude carrier", locked.Locked.Subjects())
	}
	record := locked.Locked.Subjects()[0]
	return record.SubjectID(), snapshottest.DelegatedRelation(t, record)
}

func mustCLIClaudePluginManagedRowWithScope(
	t *testing.T,
	subjectKey string,
	managedKey string,
	scope observeclaudeplugin.HostScope,
) observeclaudeplugin.Row {
	t.Helper()
	row, err := observeclaudeplugin.NewRow(observeclaudeplugin.RowSpec{
		SubjectKey:            subjectKey,
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    managedKey,
		Scope:                 scope,
	})
	if err != nil {
		t.Fatalf("NewRow returned error: %v", err)
	}
	return row
}
