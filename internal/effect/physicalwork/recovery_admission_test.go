package physicalwork

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

func TestRecoveryRemovalAdmissionBoundsRepeatedPlanningWithoutReset(t *testing.T) {
	budget := NewCaptureBudget(nil)
	work, err := workmeasure.NewArtifactWork(3, 17)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitTree(work); err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitPathComponents(5); err != nil {
		t.Fatal(err)
	}
	entries, bytes, paths := budget.RemainingEntries(), budget.RemainingBytes(), budget.pathComponents
	demands := captureBudgetDemands(t)
	if err := budget.AdmitRecoveryRemovalDemands(demands); err != nil {
		t.Fatal(err)
	}
	if entries != budget.RemainingEntries() || bytes != budget.RemainingBytes() || paths != budget.pathComponents {
		t.Fatal("recovery admission replenished consumed physical work")
	}
	if budget.AdmitRecoveryRemovalDemands(demands) == nil || budget.AdmitRemovalDemands(demands) == nil {
		t.Fatal("repeated admission reset the operation")
	}
	limit := demands.Len() * (removalObservationsPerIntent + 3*removalPlanningObservationsPerIntent)
	for index := 0; index < limit; index++ {
		if err := budget.AdmitObservation(); err != nil {
			t.Fatalf("finite recovery observation %d: %v", index, err)
		}
	}
	if budget.AdmitObservation() == nil {
		t.Fatal("recovery observation allowance is unbounded")
	}
	empty := NewCaptureBudget(nil)
	set, err := recovery.NewRemovalDemandSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.AdmitRecoveryRemovalDemands(set); err != nil {
		t.Fatal(err)
	}
	if empty.AdmitObservation() == nil {
		t.Fatal("empty demand set manufactured a removal observation")
	}
}
