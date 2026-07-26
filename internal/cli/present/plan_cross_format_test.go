package clipresent

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func codexNoObserverAttemptAction(t *testing.T) reconciliation.RelationAction {
	t.Helper()
	record, subject := codexPluginCarrierFixture(t)
	realization, ok := record.Realization()
	if !ok {
		t.Fatal("locked subject contract does not carry a realization")
	}
	relation, ok := realization.DelegatedRelation()
	if !ok {
		t.Fatal("locked subject contract does not carry a delegated relation")
	}
	admission, err := reconciliation.NewRelationRouteAdmissionDecision(reconciliation.RelationRouteAdmissionSpec{
		Row:               reconciliation.RouteAdmissionRowInstallCarrier,
		RequestedOutcome:  reconciliation.AdmissionOutcomeOrdinaryMutation,
		SelectedOutcome:   reconciliation.AdmissionOutcomeHostDelegated,
		ObservationPolicy: reconciliation.ObservationAttemptWhenUnsupported,
	})
	if err != nil {
		t.Fatalf("NewRouteAdmissionDecision returned error: %v", err)
	}
	identity := presentManagedCarrierIdentity(t, record)
	action, err := reconciliation.NewRelationAction(reconciliation.RelationActionInput{
		CarrierIdentity: identity,
		RouteRequest:    relation.RouteRequest(),
		Correlation: observerelation.Correlate(
			subject.ExpectedRelation(),
			observerelation.UnsupportedInventory(),
		),
		RouteAdmission: admission,
	})
	if err != nil {
		t.Fatalf("relation.Plan returned error: %v", err)
	}
	return action
}

func codexPluginCarrierFixture(
	t *testing.T,
) (lock.LockedSubjectContract, realization.DelegatedRelation) {
	t.Helper()
	value := desiredtest.Extension(t, desiredextension.Spec{
		Name:    "documents-managed",
		Carrier: desiredextension.CarrierCodexPlugin,
		Target:  target.TargetCodex,
		Scope:   target.ScopeGlobal,
		Source:  desiredtest.ExtensionSource(t, desiredextension.SourceKindMarketplace, "documents@market"),
	})
	file, relation := snapshottest.ExtensionCarrierFile(t, value)
	return file.Locked.Subjects()[0], relation
}

func TestPrintNoObserverAttemptDisclosesEvidenceWithoutConvergenceClaims(t *testing.T) {
	action := codexNoObserverAttemptAction(t)

	var human bytes.Buffer
	PrintRelationActionsWithOptions(&human, []reconciliation.RelationAction{action}, HumanOptions{Verbose: true})
	for _, required := range []string{
		"kind=attempt",
		"evidence_availability=unsupported",
		"execution=host_route",
		"reason=unsupported_passive_inventory",
		"correlation_state=unsupported",
		"invokes_host_route=true",
		"allows_host_route_invocation=true",
		"blocks_ordinary_apply=false",
		"exact_artifact_replay",
		"future_skip_authority",
	} {
		if !strings.Contains(human.String(), required) {
			t.Fatalf("human output = %q, want %q", human.String(), required)
		}
	}
	for _, forbidden := range []string{"installed", "enabled", "ready", "converged", "success"} {
		if strings.Contains(human.String(), forbidden) {
			t.Fatalf("human output = %q, want no %q wording", human.String(), forbidden)
		}
	}

	var machine bytes.Buffer
	if err := PrintPlanJSON(&machine, PlanJSONInput{
		Command: "status",
		Mode:    "status",
		Reconciliation: reconciliationWithFamilies(
			t,
			reconciliation.ContextInspect,
			reconciliation.Result{},
			[]reconciliation.RelationAction{action},
			nil,
		),
	}); err != nil {
		t.Fatalf("PrintPlanJSON returned error: %v", err)
	}
	var payload struct {
		RelationActions []struct {
			Kind                 string   `json:"kind"`
			EvidenceAvailability string   `json:"evidence_availability"`
			CorrelationState     string   `json:"correlation_state"`
			Reason               string   `json:"reason"`
			Execution            string   `json:"execution"`
			NonClaims            []string `json:"non_claims"`
			InvokesHostRoute     bool     `json:"invokes_host_route"`
			BlocksOrdinaryApply  bool     `json:"blocks_ordinary_apply"`
		} `json:"relation_actions"`
	}
	if err := json.Unmarshal(machine.Bytes(), &payload); err != nil {
		t.Fatalf("decode plan JSON: %v", err)
	}
	if len(payload.RelationActions) != 1 {
		t.Fatalf("relation_actions = %#v, want one", payload.RelationActions)
	}
	got := payload.RelationActions[0]
	if got.Kind != "attempt" || got.EvidenceAvailability != "unsupported" ||
		got.CorrelationState != "unsupported" || got.Reason != "unsupported_passive_inventory" ||
		got.Execution != "host_route" || !got.InvokesHostRoute || got.BlocksOrdinaryApply ||
		slices.Contains(got.NonClaims, "host_route_invocation") {
		t.Fatalf("relation action = %#v, want executable unverified attempt", got)
	}
}
