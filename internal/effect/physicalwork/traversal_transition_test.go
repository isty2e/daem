package physicalwork

import (
	"errors"
	"testing"
)

type phaseBudgetProbe struct {
	remaining int
	admitted  int
}

func (budget *phaseBudgetProbe) AdmitPathComponents(count int) error {
	if count < 0 || count > budget.remaining {
		return errors.New("physical traversal budget exhausted")
	}
	budget.remaining -= count
	budget.admitted += count
	return nil
}

func TestPhysicalTraversalPhaseRoutesRetainedAuthorityToExecutionBudget(t *testing.T) {
	planning := &phaseBudgetProbe{remaining: 10}
	phase, err := NewTraversalPhase(planning)
	if err != nil {
		t.Fatal(err)
	}
	if err := phase.AdmitPathComponents(3); err != nil {
		t.Fatal(err)
	}
	execution := &phaseBudgetProbe{remaining: 10}
	if err := phase.Advance(execution, execution); err != nil {
		t.Fatal(err)
	}
	if err := phase.AdmitPathComponents(4); err != nil {
		t.Fatal(err)
	}
	if planning.admitted != 3 || execution.admitted != 4 {
		t.Fatalf(
			"phase routing admitted planning=%d execution=%d, want 3 and 4",
			planning.admitted,
			execution.admitted,
		)
	}
	if err := phase.Advance(&phaseBudgetProbe{remaining: 10}, execution); err == nil {
		t.Fatal("physical traversal phase advanced twice")
	}
}

func TestTraversalHostViewFollowsTheSameSingleTransition(t *testing.T) {
	planning := &phaseBudgetProbe{remaining: 10}
	phase, err := NewTraversalPhase(planning)
	if err != nil {
		t.Fatal(err)
	}
	hostView := phase.Host()
	if err := hostView.AdmitPathComponents(2); err != nil {
		t.Fatal(err)
	}
	control, host := &phaseBudgetProbe{remaining: 10}, &phaseBudgetProbe{remaining: 10}
	if err := phase.Advance(control, nil); err == nil {
		t.Fatal("incomplete execution allocation advanced the phase")
	}
	if err := phase.AdmitPathComponents(3); err != nil {
		t.Fatal(err)
	}
	if err := phase.Advance(control, host); err != nil {
		t.Fatal(err)
	}
	if err := phase.AdmitPathComponents(4); err != nil {
		t.Fatal(err)
	}
	if err := hostView.AdmitPathComponents(6); err != nil {
		t.Fatal(err)
	}
	if planning.admitted != 5 || control.admitted != 4 || host.admitted != 6 {
		t.Fatalf("planning/control/host charges = %d/%d/%d, want 5/4/6", planning.admitted, control.admitted, host.admitted)
	}
	if err := phase.Advance(planning, planning); err == nil {
		t.Fatal("second transition replenished the phase")
	}
	if err := hostView.AdmitPathComponents(5); err == nil {
		t.Fatal("host allocation was replenished by a refused transition")
	}
}
