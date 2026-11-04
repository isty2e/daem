package recovery_test

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func TestPhysicalCaptureInvalidDemandAdmissionIsAtomic(t *testing.T) {
	set := recovery.CaptureBudgetDemandsForTest(t)
	for name, demands := range recovery.InvalidCaptureBudgetDemandsForTest(set) {
		t.Run(name, func(t *testing.T) {
			budget := physicalwork.NewCaptureBudget(nil)
			if budget.AdmitRemovalDemands(demands) == nil || budget.AdmitObservation() == nil {
				t.Fatal("invalid complete demand set opened removal admission")
			}
			if err := budget.AdmitRemovalDemands(set); err != nil {
				t.Fatal("invalid admission burned the transition", err)
			}
			baseline := physicalwork.NewCaptureBudget(nil)
			if err := baseline.AdmitRemovalDemands(set); err != nil {
				t.Fatal(err)
			}
			for baseline.AdmitObservation() == nil {
				if err := budget.AdmitObservation(); err != nil {
					t.Fatal("invalid attempt reduced exact observation capacity", err)
				}
			}
			if budget.AdmitObservation() == nil {
				t.Fatal("invalid attempt increased exact observation capacity")
			}
		})
	}
	for _, count := range []int{0, 1} {
		budget, err := physicalwork.NewBudget(count)
		if err != nil {
			t.Fatal(err)
		}
		if budget.AdmitRemovalDemands(set) == nil {
			t.Fatal("eager budget was reopened as a capture allocation")
		}
	}
	var absent *physicalwork.Budget
	if absent.AdmitRemovalDemands(set) == nil {
		t.Fatal("missing budget admitted demands")
	}
}
