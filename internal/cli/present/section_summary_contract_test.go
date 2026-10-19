package clipresent

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestBoundedNoOpSummaryPreservesObservationAndJSON(t *testing.T) {
	record, relation := hostSourceCarrierFixture(t, "bounded", desiredextension.CarrierAntigravityCLIPlugin, target.TargetAntigravityCLI, target.ScopeGlobal, "tools@market")
	expected := relation.ExpectedRelation()
	row, err := observerelation.NewRow(observerelation.RowSpec{SubjectKey: string(expected.SubjectKey())})
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := observerelation.NewInventory(observerelation.InventorySpec{
		Availability: observerelation.InventorySupported, Freshness: observerelation.EvidenceFresh,
		Rows: []observerelation.Row{row},
	})
	if err != nil {
		t.Fatal(err)
	}
	admission, err := reconcile.NewRelationRouteAdmissionDecision(reconcile.RelationRouteAdmissionSpec{
		Row: reconcile.RouteAdmissionRowInstallCarrier, RequestedOutcome: reconcile.AdmissionOutcomeOrdinaryMutation,
		SelectedOutcome: reconcile.AdmissionOutcomeObserveOnly, ObservationPolicy: reconcile.ObservationRequireCurrent,
	})
	if err != nil {
		t.Fatal(err)
	}
	action, err := reconcile.NewRelationAction(reconcile.RelationActionInput{
		CarrierIdentity: presentManagedCarrierIdentity(t, record), RouteRequest: relation.RouteRequest(),
		Correlation: observerelation.Correlate(expected, inventory), RouteAdmission: admission, ManagedClaimPresent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if action.Kind() != reconcile.ActionNoOp || action.CorrelationState() != observerelation.StateUnkeyedSameSubject {
		t.Fatalf("not an admitted bounded no-op: %s/%s", action.Kind(), action.CorrelationState())
	}
	plan, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextDryRun, Relations: []reconcile.RelationAction{action}})
	if err != nil {
		t.Fatal(err)
	}
	jsonOutput := func() []byte {
		t.Helper()
		var output bytes.Buffer
		if err := PrintPlanJSON(&output, PlanJSONInput{Command: "apply", Mode: "dry-run", Reconciliation: plan}); err != nil {
			t.Fatal(err)
		}
		return output.Bytes()
	}
	before := jsonOutput()
	var basic, verbose bytes.Buffer
	PrintRelationActionsWithOptions(&basic, plan.Relations(), HumanOptions{})
	PrintRelationActionsWithOptions(&verbose, plan.Relations(), HumanOptions{Verbose: true})
	if basic.String() != "relation actions: 1 entry (1 unchanged)\n" {
		t.Fatalf("bounded no-op remained noisy: %s", basic.String())
	}
	for _, fact := range []string{"correlation_state=" + string(action.CorrelationState()), "evidence_source=" + string(action.EvidenceSource())} {
		if !strings.Contains(verbose.String(), fact) {
			t.Fatalf("verbose lost %q: %s", fact, verbose.String())
		}
	}
	if !bytes.Equal(before, jsonOutput()) {
		t.Fatal("human projection changed the JSON facts")
	}
}

func TestSectionSummaryPreservesInputAndVisibleOrder(t *testing.T) {
	input := []int{1, 2, 3, 4}
	visible, unchanged := summarizeUnchanged(input, false, func(value int) bool { return value%2 == 0 })
	if unchanged != 2 || !reflect.DeepEqual(visible, []int{1, 3}) || !reflect.DeepEqual(input, []int{1, 2, 3, 4}) {
		t.Fatalf("summary=%v/%d input=%v", visible, unchanged, input)
	}
	verbose, unchanged := summarizeUnchanged(input, true, func(int) bool { t.Fatal("verbose classified rows"); return false })
	if unchanged != 0 || !reflect.DeepEqual(verbose, input) {
		t.Fatalf("verbose summary=%v/%d", verbose, unchanged)
	}
}

func TestOrderResultSummaryRetainsChangeAndFailure(t *testing.T) {
	for _, outcome := range []applyworkflow.RelationOrderOutcome{
		applyworkflow.RelationOrderExact, applyworkflow.RelationOrderConverged,
		applyworkflow.RelationOrderFailed, applyworkflow.RelationOrderNotAttempted,
	} {
		for _, changed := range []bool{false, true} {
			want := outcome == applyworkflow.RelationOrderExact && !changed
			if got := unchangedOrderResult(outcome, changed); got != want {
				t.Fatalf("outcome=%s changed=%t summarized=%t, want %t", outcome, changed, got, want)
			}
		}
	}
}

func TestConditionalOrderRemainsVisible(t *testing.T) {
	var output bytes.Buffer
	PrintRelationOrderActionsWithOptions(&output, []reconcile.RelationOrderDecision{summaryOrderDecision(t, true)}, HumanOptions{})
	if !strings.Contains(output.String(), "after pending carrier install") || strings.Contains(output.String(), "unchanged)") {
		t.Fatalf("conditional order was hidden: %s", output.String())
	}
}

func TestEmptyExtensionSectionsStaySilent(t *testing.T) {
	for _, options := range []HumanOptions{{}, {Verbose: true}} {
		var output bytes.Buffer
		PrintRelationActionsWithOptions(&output, nil, options)
		PrintCarrierAdoptionActionsWithOptions(&output, nil, options)
		PrintCarrierAbsenceActionsWithOptions(&output, nil, options)
		PrintRelationOrderActionsWithOptions(&output, nil, options)
		PrintRelationOrderResultsWithOptions(&output, nil, options)
		if output.Len() != 0 {
			t.Fatalf("empty extension sections became noisy: %s", output.String())
		}
	}
}
