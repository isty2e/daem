package recoveryoperation

import (
	"fmt"

	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// RegistryScopeFor selects actual document mutation/baseline intents; merely
// retaining an exclusive observation for a no-op member does not select it.

func (operation *NativeRecoveryOperation) RegistryScope() (ownership.DocumentScope, error) {
	if operation == nil || operation.closed {
		return ownership.DocumentScope{}, fmt.Errorf("live native recovery operation is required")
	}
	return operation.plan.registryScope, nil
}

// DocumentWriteAliases omits retained read-only groups, not original mutation
// intents that happen to be already at their target during this recovery.
func (plan NativeRecoveryPlan) DocumentWriteAliases() ([][]aggregate.DocumentAddress, error) {
	return plan.assessment.DocumentWriteAliases()
}

// ObservedDocuments projects the plan's qualified observation without exposing
// a complete Registry that could be mistaken for a publication candidate.
func (plan NativeRecoveryPlan) ObservedDocuments(scope ownership.DocumentScope) (ownership.DocumentRegistryView, error) {
	return plan.assessment.ObservedDocuments(scope)
}
