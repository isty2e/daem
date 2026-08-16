package clipresent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/contractversion"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
)

func TestPlanJSONDisclosesBlockedCarrierAbsenceWithoutExecutionClaim(t *testing.T) {
	action := presentCarrierAbsenceAction(t)
	plan := reconciliationWithCarrierAbsences(
		t,
		reconcile.ContextInspect,
		mustReconciliationPlan(t, nil, nil),
		[]carrierabsence.Action{action},
	)
	var output bytes.Buffer
	if err := PrintPlanJSON(&output, PlanJSONInput{
		Command:        "status",
		Mode:           "status",
		Reconciliation: plan,
	}); err != nil {
		t.Fatalf("PrintPlanJSON: %v", err)
	}
	var payload struct {
		SchemaVersion   int  `json:"schema_version"`
		HasErrors       bool `json:"has_errors"`
		CarrierAbsences []struct {
			Kind                        string   `json:"kind"`
			RequestedOutcome            string   `json:"requested_outcome"`
			SelectedAction              string   `json:"selected_action"`
			Execution                   string   `json:"execution"`
			CorrelationState            string   `json:"correlation_state"`
			EvidenceAvailability        string   `json:"evidence_availability"`
			EvidenceFreshness           string   `json:"evidence_freshness"`
			DaemKnownConsumerCount      int      `json:"daem_known_consumer_count"`
			RemainingDaemKnownConsumers int      `json:"remaining_daem_known_consumers"`
			RouteID                     string   `json:"route_id"`
			NonClaims                   []string `json:"non_claims"`
			InvokesHostRoute            bool     `json:"invokes_host_route"`
			RetiresClaim                bool     `json:"retires_claim"`
			BlocksOrdinaryApply         bool     `json:"blocks_ordinary_apply"`
		} `json:"carrier_absence_actions"`
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload.SchemaVersion != contractversion.ReconciliationPlanJSON || !payload.HasErrors || len(payload.CarrierAbsences) != 1 {
		t.Fatalf("payload = %#v, want current plan schema with one blocking carrier absence", payload)
	}
	got := payload.CarrierAbsences[0]
	if got.Kind != "carrier_absence" ||
		got.RequestedOutcome != string(carrierabsence.DesiredAbsent) ||
		got.SelectedAction != string(carrierabsence.DecisionBlockRoute) ||
		got.Execution != "none" ||
		got.CorrelationState != "exact_correlation" ||
		got.EvidenceAvailability != "supported" ||
		got.EvidenceFreshness != "fresh" ||
		got.DaemKnownConsumerCount != 1 ||
		got.RemainingDaemKnownConsumers != 0 ||
		got.RouteID != "" ||
		got.NonClaims == nil ||
		got.InvokesHostRoute ||
		got.RetiresClaim ||
		!got.BlocksOrdinaryApply {
		t.Fatalf("carrier absence = %#v, want honest route block disclosure", got)
	}
}

func TestCarrierAbsenceHumanOutputDistinguishesBlockFromRemoval(t *testing.T) {
	action := presentCarrierAbsenceAction(t)
	var summary bytes.Buffer
	PrintCarrierAbsenceActionsWithOptions(
		&summary,
		[]carrierabsence.Action{action},
		HumanOptions{},
	)
	if !strings.Contains(summary.String(), "blocked carrier removal") ||
		strings.Contains(summary.String(), "remove managed carrier relation through host") {
		t.Fatalf("summary = %q, want blocked removal without execution wording", summary.String())
	}

	var verbose bytes.Buffer
	PrintCarrierAbsenceActionsWithOptions(
		&verbose,
		[]carrierabsence.Action{action},
		HumanOptions{Verbose: true},
	)
	for _, want := range []string{
		"selected_action=block_route",
		"execution=none",
		"correlation_state=exact_correlation",
		"invokes_host_route=false",
		"retires_claim=false",
		"blocks_ordinary_apply=true",
	} {
		if !strings.Contains(verbose.String(), want) {
			t.Fatalf("verbose = %q, want %q", verbose.String(), want)
		}
	}
}

func TestCarrierAbsencePresentationDisclosesAdmittedRemovalEnvelope(t *testing.T) {
	action := admittedCarrierAbsenceAction(t)
	row := carrierAbsenceJSONActions([]carrierabsence.Action{action})[0]
	if row.RequestedOutcome != "absent" ||
		row.SelectedAction != "remove" ||
		row.Execution != "host_route" ||
		row.RouteID != "test.remove" ||
		row.RouteRequestHash == "" ||
		row.PostconditionVerification != string(lock.VerificationHostRelation) ||
		row.RecoveryContract != string(lock.OperationRecoverySafeRetry) ||
		!slices.Equal(row.RemovedEffects, []string{"managed_relation"}) ||
		!slices.Equal(row.RetainedEffects, []string{"external_store"}) ||
		!slices.Contains(row.NonClaims, "ambient_consumers") ||
		!row.InvokesHostRoute ||
		!row.RetiresClaim ||
		row.StateOnly ||
		row.BlocksOrdinaryApply {
		t.Fatalf("carrier absence row = %#v, want complete admitted removal disclosure", row)
	}

	var verbose bytes.Buffer
	PrintCarrierAbsenceActionsWithOptions(
		&verbose,
		[]carrierabsence.Action{action},
		HumanOptions{Verbose: true},
	)
	for _, want := range []string{
		"requested_outcome=absent",
		"selected_action=remove",
		"execution=host_route",
		`route_id="test.remove"`,
		`postcondition_verification="host_relation"`,
		`recovery_contract="safe_retry"`,
		`removed_effects=["managed_relation"]`,
		`retained_effects=["external_store"]`,
		"invokes_host_route=true",
	} {
		if !strings.Contains(verbose.String(), want) {
			t.Fatalf("verbose = %q, want %q", verbose.String(), want)
		}
	}
}

func TestCarrierAbsenceJSONRedactsOpaqueOpenCodeHostSources(t *testing.T) {
	for _, source := range []string{"plugins/local.ts", `plugins\local.ts`} {
		t.Run(source, func(t *testing.T) {
			contract, relation := hostSourceCarrierFixture(
				t,
				"local-opencode",
				desiredextension.CarrierOpenCodePlugin,
				target.TargetOpenCode,
				target.ScopeProject,
				source,
			)
			action := presentCarrierAbsenceActionFromContract(t, contract, relation)
			row := carrierAbsenceJSONActions([]carrierabsence.Action{action})[0]
			if !row.SourceNamespaceRedacted ||
				row.CarrierSubject == nil ||
				!row.CarrierSubject.NameRedacted ||
				strings.Contains(row.SourceNamespace, source) ||
				strings.Contains(row.CarrierSubject.Name, source) {
				t.Fatalf("carrier absence row = %#v", row)
			}
		})
	}
}

func TestCarrierAbsencePresentationDistinguishesPendingSettlement(t *testing.T) {
	action := pendingCarrierAbsenceAction(t)
	row := carrierAbsenceJSONActions([]carrierabsence.Action{action})[0]
	if row.SelectedAction != "verify_pending_removal" ||
		row.Execution != "observation_only" ||
		row.RouteID != "test.remove" ||
		row.RouteRequestHash == "" ||
		row.PostconditionVerification != "fresh_pending_removal_postconditions" ||
		row.RecoveryContract != "observation_only_pending_settlement" ||
		row.InvokesHostRoute ||
		!row.RetiresClaim ||
		row.StateOnly ||
		row.BlocksOrdinaryApply {
		t.Fatalf("carrier absence row = %#v, want observation-only settlement", row)
	}

	var summary bytes.Buffer
	PrintCarrierAbsenceActionsWithOptions(
		&summary,
		[]carrierabsence.Action{action},
		HumanOptions{},
	)
	if !strings.Contains(summary.String(), "verify pending carrier removal") ||
		strings.Contains(summary.String(), "retire already-absent") ||
		strings.Contains(summary.String(), "through host") {
		t.Fatalf("summary = %q, want pending verification without invocation", summary.String())
	}
}

func TestCarrierAbsencePresentationDisclosesDirectConfigRemoval(t *testing.T) {
	action := directCarrierAbsenceAction(t)
	row := carrierAbsenceJSONActions([]carrierabsence.Action{action})[0]
	if row.Execution != "direct_config" ||
		row.InvokesHostRoute ||
		!row.RetiresClaim ||
		row.StateOnly ||
		row.BlocksOrdinaryApply {
		t.Fatalf("carrier absence row = %#v, want direct config mutation", row)
	}

	var summary bytes.Buffer
	PrintCarrierAbsenceActionsWithOptions(
		&summary,
		[]carrierabsence.Action{action},
		HumanOptions{},
	)
	if !strings.Contains(summary.String(), "from host config") ||
		strings.Contains(summary.String(), "through host") {
		t.Fatalf("summary = %q, want direct config removal wording", summary.String())
	}

	var verbose bytes.Buffer
	PrintCarrierAbsenceActionsWithOptions(
		&verbose,
		[]carrierabsence.Action{action},
		HumanOptions{Verbose: true},
	)
	for _, want := range []string{
		"execution=direct_config",
		"invokes_host_route=false",
		`retained_effects=["external_store"]`,
	} {
		if !strings.Contains(verbose.String(), want) {
			t.Fatalf("verbose = %q, want %q", verbose.String(), want)
		}
	}
}

func admittedCarrierAbsenceAction(t *testing.T) carrierabsence.Action {
	return admittedCarrierAbsenceActionWithActuation(
		t,
		lock.ActuationDelegatedHostRoute,
	)
}

func directCarrierAbsenceAction(t *testing.T) carrierabsence.Action {
	return admittedCarrierAbsenceActionWithActuation(
		t,
		lock.ActuationDirectProjection,
	)
}

func admittedCarrierAbsenceActionWithActuation(
	t *testing.T,
	actuation lock.ActuationKind,
) carrierabsence.Action {
	t.Helper()
	blocked := presentCarrierAbsenceAction(t)
	observation, observed := blocked.Observation()
	if !observed {
		t.Fatal("presentation fixture lacks exact observation")
	}
	digest := sha256.Sum256([]byte("test-removal-v1"))
	request, err := realizationdelegate.NewRequest(
		"test.remove",
		"test-removal-v1",
		"sha256:"+hex.EncodeToString(digest[:]),
	)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := lock.NewOperationContract(lock.OperationContractInput{
		Operation: lock.OperationRemove,
		Actuation: actuation,
		Authority: lock.AuthorityRemove,
		Route: lock.RouteContractRef{
			RouteID:                request.RouteID(),
			AdapterContractVersion: request.ContractVersion(),
		},
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
		Request:         request,
		RemovedEffects:  []string{"managed_relation"},
		RetainedEffects: []string{"external_store"},
		NonClaims:       []string{"ambient_consumers"},
	})
	if err != nil {
		t.Fatal(err)
	}
	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:       blocked.Claim(),
		Desired:     carrierabsence.DesiredAbsent,
		Observation: observation,
		Occupancy:   blocked.Occupancy(),
		Route:       route,
	})
	if err != nil {
		t.Fatal(err)
	}
	return action
}

func pendingCarrierAbsenceAction(t *testing.T) carrierabsence.Action {
	t.Helper()
	admitted := admittedCarrierAbsenceAction(t)
	route := admitted.RouteAdmission()
	baselines, err := durablecarrier.NewEffectBaselineSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierRemoval(
		admitted.Claim(),
		route.Request(),
		route.Operation().EffectPostconditions(),
		baselines,
	)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := observerelation.NewInventory(observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	if err != nil {
		t.Fatal(err)
	}
	observed, present := admitted.Observation()
	if !present {
		t.Fatal("presentation fixture lacks observation key")
	}
	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:   admitted.Claim(),
		Desired: carrierabsence.DesiredAbsent,
		Observation: observerelation.Correlation{
			Key: observed.Key,
			Result: observerelation.Correlate(
				admitted.Claim().Identity().ExpectedRelation(),
				inventory,
			),
		},
		Occupancy: admitted.Occupancy(),
		Route:     carrierabsence.UnavailableRoute(),
		Pending:   &pending,
	})
	if err != nil {
		t.Fatal(err)
	}
	return action
}
