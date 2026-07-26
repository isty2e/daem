package reconcile_test

import (
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/relation"
	reconcile "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

const routeAdmissionRowInstallCarrier reconcile.RelationRouteAdmissionRow = "RA-01"

func mustPlan(
	t *testing.T,
	subject hostrelation.ExpectedRelation,
	correlation observerelation.CorrelationResult,
	admission reconcile.RelationRouteAdmissionDecision,
) reconcile.RelationAction {
	t.Helper()
	action, err := reconcile.NewRelationAction(validInput(t, subject, correlation, admission))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if action.Subject().Kind() != topology.SubjectHostRelation {
		t.Fatalf("subject kind = %q, want host_relation", action.Subject().Kind())
	}
	if action.Target() != target.TargetClaudeCode {
		t.Fatalf("target = %q, want Claude Code", action.Target())
	}
	if action.Scope() != target.ScopeProject {
		t.Fatalf("scope = %q, want project", action.Scope())
	}
	if action.RelationSubjectKey() != string(subject.SubjectKey()) {
		t.Fatalf("relation subject key = %q, want %q", action.RelationSubjectKey(), subject.SubjectKey())
	}
	return action
}

func validInput(
	t *testing.T,
	subject hostrelation.ExpectedRelation,
	correlation observerelation.CorrelationResult,
	admission reconcile.RelationRouteAdmissionDecision,
) reconcile.RelationActionInput {
	t.Helper()
	routeRequest, err := realizationdelegate.NewRequest(
		"test.relation.install",
		"test-v1",
		"sha256:0000000000000000000000000000000000000000000000000000000000000000",
	)
	if err != nil {
		t.Fatalf("NewDelegatedRouteRequest: %v", err)
	}
	return reconcile.RelationActionInput{
		CarrierIdentity: mustCarrierIdentity(
			t,
			mustSubjectID(t),
			target.TargetClaudeCode,
			target.ScopeProject,
			"context7@official",
			string(subject.SubjectKey()),
		),
		RouteRequest:        routeRequest,
		Correlation:         correlation,
		RouteAdmission:      admission,
		ManagedClaimPresent: true,
	}
}

func assertAction(
	t *testing.T,
	action reconcile.RelationAction,
	kind reconcile.RelationActionKind,
	execution reconcile.RelationExecutionClass,
	reason reconcile.RelationReasonCode,
) {
	t.Helper()
	if action.Kind() != kind {
		t.Fatalf("action kind = %q, want %q", action.Kind(), kind)
	}
	if action.Execution() != execution {
		t.Fatalf("execution = %q, want %q", action.Execution(), execution)
	}
	if action.Reason() != reason {
		t.Fatalf("reason = %q, want %q", action.Reason(), reason)
	}
	if action.SourceNamespace() != "marketplace:context7@official" {
		t.Fatalf("source namespace = %q, want marketplace:context7@official", action.SourceNamespace())
	}
}

func assertEvidence(
	t *testing.T,
	action reconcile.RelationAction,
	availability observerelation.InventoryAvailability,
	freshness observerelation.EvidenceFreshness,
) {
	t.Helper()
	if action.EvidenceSource() != "passive_relation_inventory" {
		t.Fatalf("evidence source = %q, want passive_relation_inventory", action.EvidenceSource())
	}
	if action.EvidenceAvailability() != availability {
		t.Fatalf("evidence availability = %q, want %q", action.EvidenceAvailability(), availability)
	}
	if action.EvidenceFreshness() != freshness {
		t.Fatalf("evidence freshness = %q, want %q", action.EvidenceFreshness(), freshness)
	}
}

func assertWatchpoints(
	t *testing.T,
	action reconcile.RelationAction,
	want []observerelation.Watchpoint,
) {
	t.Helper()
	got := action.Watchpoints()
	if len(got) != len(want) {
		t.Fatalf("watchpoints = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("watchpoints = %v, want %v", got, want)
		}
	}
	got[0] = "mutated"
	if action.Watchpoints()[0] == "mutated" {
		t.Fatalf("Watchpoints leaked mutable slice")
	}
}

func correlationFor(
	t *testing.T,
	subject hostrelation.ExpectedRelation,
	spec observerelation.InventorySpec,
) observerelation.CorrelationResult {
	t.Helper()
	return observerelation.Correlate(subject, mustInventory(t, spec))
}

func blockedAdmission(t *testing.T) reconcile.RelationRouteAdmissionDecision {
	t.Helper()
	return mustAdmission(t, reconcile.AdmissionOutcomeOrdinaryMutation, reconcile.AdmissionOutcomeBlocked)
}

func ordinaryAdmission(t *testing.T) reconcile.RelationRouteAdmissionDecision {
	t.Helper()
	return mustAdmission(t, reconcile.AdmissionOutcomeOrdinaryMutation, reconcile.AdmissionOutcomeOrdinaryMutation)
}

func hostDelegatedAdmission(t *testing.T) reconcile.RelationRouteAdmissionDecision {
	t.Helper()
	return mustAdmission(t, reconcile.AdmissionOutcomeOrdinaryMutation, reconcile.AdmissionOutcomeHostDelegated)
}

func attemptWhenUnsupportedAdmission(t *testing.T) reconcile.RelationRouteAdmissionDecision {
	t.Helper()
	decision, err := reconcile.NewRelationRouteAdmissionDecision(reconcile.RelationRouteAdmissionSpec{
		Row:               routeAdmissionRowInstallCarrier,
		RequestedOutcome:  reconcile.AdmissionOutcomeOrdinaryMutation,
		SelectedOutcome:   reconcile.AdmissionOutcomeHostDelegated,
		ObservationPolicy: reconcile.ObservationAttemptWhenUnsupported,
	})
	if err != nil {
		t.Fatalf("NewRouteAdmissionDecision: %v", err)
	}
	return decision
}

func assistedAdmission(t *testing.T) reconcile.RelationRouteAdmissionDecision {
	t.Helper()
	return mustAdmission(t, reconcile.AdmissionOutcomeOrdinaryMutation, reconcile.AdmissionOutcomeAssisted)
}

func explicitAttemptAdmission(t *testing.T) reconcile.RelationRouteAdmissionDecision {
	t.Helper()
	return mustAdmission(t, reconcile.AdmissionOutcomeOrdinaryMutation, reconcile.AdmissionOutcomeExplicitAttempt)
}

func observeOnlyAdmission(t *testing.T) reconcile.RelationRouteAdmissionDecision {
	t.Helper()
	return mustAdmission(t, reconcile.AdmissionOutcomeOrdinaryMutation, reconcile.AdmissionOutcomeObserveOnly)
}

func mustAdmission(
	t *testing.T,
	requested reconcile.RelationAdmissionOutcome,
	selected reconcile.RelationAdmissionOutcome,
) reconcile.RelationRouteAdmissionDecision {
	t.Helper()
	decision, err := reconcile.NewRelationRouteAdmissionDecision(reconcile.RelationRouteAdmissionSpec{
		Row:               routeAdmissionRowInstallCarrier,
		RequestedOutcome:  requested,
		SelectedOutcome:   selected,
		ObservationPolicy: reconcile.ObservationRequireCurrent,
	})
	if err != nil {
		t.Fatalf("NewRouteAdmissionDecision: %v", err)
	}
	return decision
}

func mustSubject(t *testing.T, subjectKey string, managedKey string) hostrelation.ExpectedRelation {
	t.Helper()
	if managedKey == "managed/"+subjectKey {
		return mustCarrierIdentity(
			t,
			mustSubjectID(t),
			target.TargetClaudeCode,
			target.ScopeProject,
			"context7@official",
			subjectKey,
		).ExpectedRelation()
	}
	relationSubjectKey, err := hostrelation.NewSubjectKey(subjectKey)
	if err != nil {
		t.Fatalf("NewSubjectKey: %v", err)
	}
	managedInstanceKey, err := hostrelation.NewManagedInstanceKey(managedKey)
	if err != nil {
		t.Fatalf("NewManagedInstanceKey: %v", err)
	}
	subject, err := hostrelation.NewExpectedRelation(relationSubjectKey, managedInstanceKey)
	if err != nil {
		t.Fatalf("NewExpectedRelation: %v", err)
	}
	return subject
}

func mustSubjectID(t *testing.T) topology.SubjectID {
	t.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"claude-code.plugin-carrier",
		"context7",
	)
	if err != nil {
		t.Fatalf("HostRelationSubjectID: %v", err)
	}
	return subject
}

func mustManagedRow(t *testing.T, subjectKey string, managedKey string) observerelation.Row {
	t.Helper()
	if managedKey == "managed/"+subjectKey {
		managedKey = string(mustSubject(t, subjectKey, managedKey).ManagedInstanceKey())
	}
	return mustRow(t, observerelation.RowSpec{
		SubjectKey:            subjectKey,
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    managedKey,
	})
}

func mustCarrierIdentity(
	t *testing.T,
	subject topology.SubjectID,
	selectedTarget target.Target,
	scope target.Scope,
	sourceRef string,
	subjectKey string,
) durablecarrier.ManagedCarrierIdentity {
	t.Helper()
	carrierFamily := desiredextension.CarrierClaudeCodePlugin
	sourceKind := desiredextension.SourceKindMarketplace
	switch selectedTarget {
	case target.TargetClaudeCode:
	case target.TargetCodex:
		carrierFamily = desiredextension.CarrierCodexPlugin
	case target.TargetAntigravityCLI:
		carrierFamily = desiredextension.CarrierAntigravityCLIPlugin
		sourceKind = desiredextension.SourceKindHostSource
	default:
		t.Fatalf("unsupported test carrier target %q", selectedTarget)
	}
	source, err := desiredextension.NewSourceRef(
		sourceKind,
		sourceRef,
	)
	if err != nil {
		t.Fatalf("NewSourceRef: %v", err)
	}
	carrierKey, err := desiredextension.NewCarrierKey(
		carrierFamily,
		selectedTarget,
		scope,
		source,
	)
	if err != nil {
		t.Fatalf("NewCarrierKey: %v", err)
	}
	carrier, err := extensiontopology.NewCarrier(carrierKey)
	if err != nil {
		t.Fatalf("NewCarrier: %v", err)
	}
	key, err := hostrelation.NewSubjectKey(subjectKey)
	if err != nil {
		t.Fatalf("NewSubjectKey: %v", err)
	}
	expected, err := hostrelation.Derive(carrierKey, subject, key)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	identity, err := durablecarrier.NewManagedCarrierIdentity(carrier, subject, expected)
	if err != nil {
		t.Fatalf("NewManagedCarrierIdentity: %v", err)
	}
	return identity
}

func mustUnmanagedRow(t *testing.T, subjectKey string) observerelation.Row {
	t.Helper()
	return mustRow(t, observerelation.RowSpec{SubjectKey: subjectKey})
}

func mustRow(t *testing.T, spec observerelation.RowSpec) observerelation.Row {
	t.Helper()
	row, err := observerelation.NewRow(spec)
	if err != nil {
		t.Fatalf("NewRow: %v", err)
	}
	return row
}

func mustInventory(t *testing.T, spec observerelation.InventorySpec) observerelation.Inventory {
	t.Helper()
	inventory, err := observerelation.NewInventory(spec)
	if err != nil {
		t.Fatalf("NewInventory: %v", err)
	}
	return inventory
}
