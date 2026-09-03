package execute

import (
	"context"
	"os"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/statefile"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
)

func TestActionAttemptPendingInstallWriteAheadAndRetirement(t *testing.T) {
	owner := testAuthority(t)
	action := testInvokingActionAttempt(t)
	current, err := durable.NewSnapshot(durable.SnapshotInput{})
	if err != nil {
		t.Fatalf("NewSnapshot returned error: %v", err)
	}
	commits := 0
	commit := func(context.Context, []byte, os.FileMode) error {
		commits++
		return nil
	}

	pending, err := commitPendingCarrierInstalls(
		t.Context(),
		commit,
		current,
		owner,
		[]reconciliation.RelationAction{action},
		statefile.Codec{},
	)
	if err != nil {
		t.Fatalf("commitPendingCarrierInstalls returned error: %v", err)
	}
	facts := pending.PendingCarrierInstalls()
	if commits != 1 || len(facts) != 1 ||
		!facts[0].Owner().Equal(owner) ||
		!facts[0].Identity().ExactEqual(action.CarrierIdentity()) ||
		!facts[0].InstallRequest().Equal(action.RouteRequest()) {
		t.Fatalf("write-ahead = (%d, %#v), want one exact ActionAttempt fact", commits, facts)
	}

	retired, err := commitRetiredPendingCarrierInstall(
		t.Context(),
		commit,
		pending,
		owner,
		action,
		statefile.Codec{},
	)
	if err != nil {
		t.Fatalf("commitRetiredPendingCarrierInstall returned error: %v", err)
	}
	if commits != 2 || len(retired.PendingCarrierInstalls()) != 0 {
		t.Fatalf("retirement = (%d, %#v), want exact ActionAttempt fact removed", commits, retired)
	}
}

func testInvokingActionAttempt(t *testing.T) reconciliation.RelationAction {
	t.Helper()
	seed := testHostRelationAction(t)
	admission, err := reconciliation.NewRelationRouteAdmissionDecision(
		reconciliation.RelationRouteAdmissionSpec{
			Row:               reconciliation.RouteAdmissionRowInstallCarrier,
			RequestedOutcome:  reconciliation.AdmissionOutcomeOrdinaryMutation,
			SelectedOutcome:   reconciliation.AdmissionOutcomeHostDelegated,
			ObservationPolicy: reconciliation.ObservationAttemptWhenUnsupported,
		},
	)
	if err != nil {
		t.Fatalf("NewRelationRouteAdmissionDecision returned error: %v", err)
	}
	action, err := reconciliation.NewRelationAction(reconciliation.RelationActionInput{
		CarrierIdentity: seed.CarrierIdentity(),
		RouteRequest:    seed.RouteRequest(),
		Correlation:     observerelation.Correlate(seed.ExpectedRelation(), observerelation.UnsupportedInventory()),
		RouteAdmission:  admission,
	})
	if err != nil {
		t.Fatalf("NewRelationAction returned error: %v", err)
	}
	if action.Kind() != reconciliation.ActionAttempt || !action.InvokesHostRoute() {
		t.Fatalf("RelationAction = %#v, want invoking ActionAttempt", action)
	}
	return action
}
