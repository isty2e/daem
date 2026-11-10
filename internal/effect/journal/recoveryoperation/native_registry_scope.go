package recoveryoperation

import (
	"fmt"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	"github.com/isty2e/daem/internal/effect/mutation"
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

// DocumentAuthorityRequest retains the qualified native route without retaining
// its root capability or reinterpreting a portable alias during recovery.
func (plan NativeRecoveryPlan) DocumentAuthorityRequest(alias aggregate.DocumentAddress) (mutation.PhysicalAuthorityRequest, error) {
	request, present := plan.documentRequests[alias]
	if !present {
		return mutation.PhysicalAuthorityRequest{}, fmt.Errorf("native recovery has no qualified authority request for the selected document")
	}
	return request, nil
}

func nativeDocumentAuthorityRequests(documents journalqualification.Documents) (map[aggregate.DocumentAddress]mutation.PhysicalAuthorityRequest, error) {
	requests := make(map[aggregate.DocumentAddress]mutation.PhysicalAuthorityRequest)
	for index := range documents.Len() {
		namespace, present := documents.NamespaceAt(index)
		if !present {
			return nil, fmt.Errorf("native recovery document has no qualified namespace")
		}
		for _, alias := range namespace.Aliases() {
			request, err := alias.PhysicalAuthorityRequest()
			if err != nil {
				return nil, err
			}
			requests[alias.Address()] = request
		}
	}
	return requests, nil
}

// ObservedDocuments projects the plan's qualified observation without exposing
// a complete Registry that could be mistaken for a publication candidate.
func (plan NativeRecoveryPlan) ObservedDocuments(scope ownership.DocumentScope) (ownership.DocumentRegistryView, error) {
	return plan.assessment.ObservedDocuments(scope)
}
