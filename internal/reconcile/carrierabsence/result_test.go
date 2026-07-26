package carrierabsence_test

import (
	"strings"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
)

func TestResultOwnsCarrierAbsenceOrderingErrorsAndCopies(t *testing.T) {
	first := newBlockedAbsence(t, "alpha")
	second := newBlockedAbsence(t, "zed")
	input := []carrierabsence.Action{second, first}
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:         reconcile.ContextApply,
		CarrierAbsences: input,
	})
	if err != nil {
		t.Fatalf("NewResult: %v", err)
	}
	input[0] = carrierabsence.Action{}
	actions := result.CarrierAbsences()
	if len(actions) != 2 ||
		actions[0].Subject().Key() != "alpha" ||
		actions[1].Subject().Key() != "zed" {
		t.Fatalf("CarrierAbsences = %#v, want canonical alpha/zed order", actions)
	}
	actions[0] = carrierabsence.Action{}
	if result.CarrierAbsences()[0].Subject().Key() != "alpha" {
		t.Fatal("CarrierAbsences accessor exposed mutable result state")
	}
	if !result.HasErrors() || !result.HasBlockedCarrierAbsences() || result.DecisionCount() != 2 {
		t.Fatalf(
			"result semantics = errors:%t blocked:%t count:%d",
			result.HasErrors(),
			result.HasBlockedCarrierAbsences(),
			result.DecisionCount(),
		)
	}
	if result.Clone().CarrierAbsences()[0].Subject().Key() != "alpha" {
		t.Fatal("Clone omitted carrier absence decisions")
	}
}

func TestResultRejectsDuplicateCarrierAbsenceIdentity(t *testing.T) {
	action := newBlockedAbsence(t, "duplicate")
	_, err := reconcile.NewResult(reconcile.ResultInput{
		Context:         reconcile.ContextInspect,
		CarrierAbsences: []carrierabsence.Action{action, action},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate carrier absence action") {
		t.Fatalf("error = %v, want duplicate carrier absence rejection", err)
	}
}

func TestStateOnlyCarrierAbsenceDoesNotBlockResult(t *testing.T) {
	fixture := newFixture(t, target.ScopeProject, "missing", "missing@market")
	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:   fixture.claim,
		Desired: carrierabsence.DesiredAbsent,
		Observation: fixture.observation(
			t,
			observerelation.InventorySupported,
			observerelation.EvidenceFresh,
		),
		Occupancy: fixture.occupancy(t, fixture.claim),
		Route:     carrierabsence.UnavailableRoute(),
	})
	if err != nil {
		t.Fatalf("NewAction: %v", err)
	}
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:         reconcile.ContextApply,
		CarrierAbsences: []carrierabsence.Action{action},
	})
	if err != nil {
		t.Fatalf("NewResult: %v", err)
	}
	if result.HasErrors() || result.HasBlockedCarrierAbsences() {
		t.Fatal("state-only claim retirement incorrectly blocked the result")
	}
}

func newBlockedAbsence(t *testing.T, declarationID string) carrierabsence.Action {
	t.Helper()
	fixture := newFixture(t, target.ScopeProject, declarationID, declarationID+"@market")
	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:       fixture.claim,
		Desired:     carrierabsence.DesiredAbsent,
		Observation: fixture.exactObservation(t),
		Occupancy:   fixture.occupancy(t, fixture.claim),
		Route:       carrierabsence.UnavailableRoute(),
	})
	if err != nil {
		t.Fatalf("NewAction: %v", err)
	}
	return action
}
