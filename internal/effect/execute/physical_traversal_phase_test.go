package execute

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
)

type pathOnlyPhaseBudget struct {
	components int
}

func (budget *pathOnlyPhaseBudget) AdmitPathComponents(count int) error {
	budget.components += count
	return nil
}

func TestPhysicalTraversalPhaseForwardsPhysicalWorkToCapableBudget(t *testing.T) {
	underlying, err := recovery.NewPhysicalWorkBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	phase, err := newPhysicalTraversalPhase(underlying)
	if err != nil {
		t.Fatal(err)
	}
	if err := phase.AdmitPhysicalWork(2, 0, 0); err != nil {
		t.Fatalf("physical work against capable phase budget = %v, want admitted", err)
	}
}

func TestPhysicalTraversalPhasePathOnlyBudgetAdmitsOnlyPathWork(t *testing.T) {
	underlying := &pathOnlyPhaseBudget{}
	phase, err := newPhysicalTraversalPhase(underlying)
	if err != nil {
		t.Fatal(err)
	}
	if err := phase.AdmitPhysicalWork(4, 0, 0); err != nil {
		t.Fatalf("path-only physical work = %v, want admitted", err)
	}
	if underlying.components != 4 {
		t.Fatalf("path budget charged %d components, want 4", underlying.components)
	}
	if err := phase.AdmitPhysicalWork(0, 1, 0); err == nil {
		t.Fatal("entry work admitted through path-only phase budget")
	}
	if err := phase.AdmitPhysicalWork(0, 0, 8); err == nil {
		t.Fatal("byte work admitted through path-only phase budget")
	}
}
