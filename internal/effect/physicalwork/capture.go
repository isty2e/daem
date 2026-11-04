package physicalwork

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
)

// NewCaptureBudget bounds comparison work before the complete removal
// demand set is known. Removal observations and execution transfers stay closed
// until AdmitRemovalDemands succeeds.
func NewCaptureBudget(sharedPathBudget PathWorkBudget) *Budget {
	budget := newPhysicalWorkAllocation(sharedPathBudget)
	budget.removalAdmissionPending = true
	return budget
}

func newPhysicalWorkAllocation(sharedPathBudget PathWorkBudget) *Budget {
	return &Budget{
		pathComponentLimit: maximumPhysicalPathComponentVisits,
		entryLimit:         MaximumPhysicalEntries,
		byteLimit:          MaximumPhysicalBytes,
		sharedPathBudget:   sharedPathBudget,
	}
}

// AdmitRemovalDemands closes capture admission once, without replenishing any
// work already consumed or reserved while deriving the complete demand set.
func (budget *Budget) AdmitRemovalDemands(demands recovery.RemovalDemandSet) error {
	return budget.admitRemovalDemands(demands, 1)
}

// AdmitRecoveryRemovalDemands includes disclosure, under-lease revalidation,
// executor reopen and post-failure diagnosis in the same allocation.
func (budget *Budget) AdmitRecoveryRemovalDemands(demands recovery.RemovalDemandSet) error {
	return budget.admitRemovalDemands(demands, 4)
}

func (budget *Budget) admitRemovalDemands(demands recovery.RemovalDemandSet, planningPasses int) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if !budget.removalAdmissionPending {
		return fmt.Errorf("physical removal demands were already admitted")
	}
	if err := demands.Validate(); err != nil {
		return err
	}
	budget.observationLimit = demands.Len() * (removalObservationsPerIntent + (planningPasses-1)*removalPlanningObservationsPerIntent)
	budget.removalAdmissionPending = false
	return nil
}

// RequireCapturePending rejects a new initial observation after removal
// admission. Admitted-empty and pending capture are distinct lifecycle states.
func (budget *Budget) RequireCapturePending() error {
	if budget == nil || !budget.removalAdmissionPending {
		return fmt.Errorf("physical capture requires pending removal admission")
	}
	return nil
}

func (budget *Budget) requireRemovalAdmission() error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.removalAdmissionPending {
		return fmt.Errorf("complete physical removal demands are not admitted")
	}
	return nil
}
