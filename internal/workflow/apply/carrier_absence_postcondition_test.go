package apply

import (
	"encoding/json"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func TestCarrierAbsenceFingerprintIncludesEffectPostconditions(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	base := carrierAbsenceFingerprintRows([]carrierabsence.Action{fixture.action})

	route := fixture.action.RouteAdmission()
	operation := route.Operation()
	coupledOperation, err := lock.NewOperationContract(lock.OperationContractInput{
		Operation:         operation.Operation(),
		Actuation:         operation.Actuation(),
		Authority:         operation.Authority(),
		Route:             operation.Route(),
		HostCompatibility: operation.HostCompatibility(),
		Preconditions:     operation.Preconditions(),
		EffectEnvelope:    operation.EffectEnvelope(),
		EffectPostconditions: []effectpostcondition.Requirement{
			effectpostcondition.CarrierArtifactsAbsent,
		},
		Idempotency:     operation.Idempotency(),
		Verification:    operation.Verification(),
		TrustActivation: operation.TrustActivation(),
		Recovery:        operation.Recovery(),
	})
	if err != nil {
		t.Fatalf("NewOperationContract returned error: %v", err)
	}
	coupledRoute, err := carrierabsence.NewRouteAdmission(carrierabsence.RouteAdmissionInput{
		Operation:              coupledOperation,
		Request:                route.Request(),
		PreservesSharedCarrier: route.PreservesSharedCarrier(),
		RemovedEffects:         route.RemovedEffects(),
		RetainedEffects:        route.RetainedEffects(),
		NonClaims:              route.NonClaims(),
	})
	if err != nil {
		t.Fatalf("NewRouteAdmission returned error: %v", err)
	}
	observation, present := fixture.action.Observation()
	if !present {
		t.Fatal("fixture action has no observation")
	}
	coupledAction, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:       fixture.action.Claim(),
		Desired:     fixture.action.Desired(),
		Observation: observation,
		Occupancy:   fixture.action.Occupancy(),
		Route:       coupledRoute,
	})
	if err != nil {
		t.Fatalf("NewAction returned error: %v", err)
	}
	coupled := carrierAbsenceFingerprintRows([]carrierabsence.Action{coupledAction})

	baseJSON, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	coupledJSON, err := json.Marshal(coupled)
	if err != nil {
		t.Fatal(err)
	}
	if string(baseJSON) == string(coupledJSON) {
		t.Fatal("effect postcondition change did not change fingerprint facts")
	}
	got := coupled[0].RemovalOperation.EffectPostconditions
	if len(got) != 1 || got[0] != effectpostcondition.CarrierArtifactsAbsent {
		t.Fatalf("fingerprint effect postconditions = %#v", got)
	}
}

func TestCarrierAbsenceFingerprintIncludesExactPendingBaseline(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	requirements, err := effectpostcondition.NewSet(
		[]effectpostcondition.Requirement{effectpostcondition.LocalSourceUnchanged},
	)
	if err != nil {
		t.Fatal(err)
	}
	absentBaseline, err := durablecarrier.NewAbsentEffectBaseline(
		effectpostcondition.LocalSourceUnchanged,
	)
	if err != nil {
		t.Fatal(err)
	}
	contentBaseline, err := durablecarrier.NewContentEffectBaseline(
		effectpostcondition.LocalSourceUnchanged,
		artifact.HashFileContent([]byte("before")),
	)
	if err != nil {
		t.Fatal(err)
	}

	var fingerprints [][]carrierAbsenceFingerprintFacts
	for _, baseline := range []durablecarrier.EffectBaseline{absentBaseline, contentBaseline} {
		baselines, err := durablecarrier.NewEffectBaselineSet([]durablecarrier.EffectBaseline{baseline})
		if err != nil {
			t.Fatal(err)
		}
		pending, err := durablecarrier.NewPendingCarrierRemoval(
			fixture.claim,
			fixture.removeRequest,
			requirements,
			baselines,
		)
		if err != nil {
			t.Fatal(err)
		}
		observed, present := fixture.action.Observation()
		if !present {
			t.Fatal("fixture action has no observation key")
		}
		action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
			Claim:   fixture.claim,
			Desired: carrierabsence.DesiredAbsent,
			Observation: observerelation.Correlation{
				Key:    observed.Key,
				Result: missingCorrelation(t, fixture.expected),
			},
			Occupancy: fixture.action.Occupancy(),
			Route:     carrierabsence.UnavailableRoute(),
			Pending:   &pending,
		})
		if err != nil {
			t.Fatal(err)
		}
		fingerprints = append(
			fingerprints,
			carrierAbsenceFingerprintRows([]carrierabsence.Action{action}),
		)
	}
	left, err := json.Marshal(fingerprints[0])
	if err != nil {
		t.Fatal(err)
	}
	right, err := json.Marshal(fingerprints[1])
	if err != nil {
		t.Fatal(err)
	}
	if string(left) == string(right) {
		t.Fatal("pending baseline change did not change fingerprint facts")
	}
	pending := fingerprints[1][0].PendingRemoval
	if pending == nil ||
		len(pending.EffectBaselines) != 1 ||
		pending.EffectBaselines[0].ContentHash == "" ||
		!fingerprints[1][0].VerifiesPendingRemoval {
		t.Fatalf("pending fingerprint = %#v", fingerprints[1][0])
	}
}
