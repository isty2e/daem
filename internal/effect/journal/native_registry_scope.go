package journal

import (
	"fmt"
	"slices"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

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

func (assessment nativeDocumentAssessment) documentWriteIndexes() ([]int, error) {
	records := assessment.qualified.records
	if records.Len() != len(assessment.stages) {
		return nil, fmt.Errorf("native recovery document facts are incomplete")
	}
	var indexes []int
	for index := range records.Len() {
		loaded, present := records.At(index)
		if !present {
			return nil, fmt.Errorf("native recovery document facts are incomplete")
		}
		group := loaded.Group()
		comparison := group.Comparison()
		if err := comparison.Validate(); err != nil {
			return nil, err
		}
		changed := !comparison.BeforeDocument().Equal(comparison.ExpectedDocument()) ||
			!comparison.BeforeState().SameRootMetadata(comparison.ExpectedState()) || assessment.stages[index].restoration.Changed()
		claims := loaded.Claims()
		changed = changed || !slices.EqualFunc(claims.Before(), claims.Prepared(), ownership.Claim.Equal) ||
			!slices.EqualFunc(claims.Prepared(), claims.After(), ownership.Claim.Equal)
		for _, intent := range group.BindingIntents() {
			bindingChanged, err := intent.ChangesBinding()
			if err != nil {
				return nil, err
			}
			changed = changed || bindingChanged
		}
		if changed {
			indexes = append(indexes, index)
		}
	}
	return indexes, nil
}

func (assessment nativeDocumentAssessment) registryScope() (ownership.DocumentScope, error) {
	if assessment.qualified.records.Len() != len(assessment.qualified.namespaces) {
		return ownership.DocumentScope{}, fmt.Errorf("live native namespace qualification is required before freezing scope")
	}
	indexes, err := assessment.documentWriteIndexes()
	if err != nil {
		return ownership.DocumentScope{}, err
	}
	namespaces := make([]journalnamespace.DocumentNamespace, 0, len(indexes))
	for _, index := range indexes {
		namespaces = append(namespaces, assessment.qualified.namespaces[index])
	}
	return journalnamespace.RegistryScope(namespaces)
}

// DocumentWriteAliases omits retained read-only groups, not original mutation
// intents that happen to be already at their target during this recovery.
func (plan NativeRecoveryPlan) DocumentWriteAliases() ([][]aggregate.DocumentAddress, error) {
	indexes, err := plan.assessment.documentWriteIndexes()
	if err != nil {
		return nil, err
	}
	aliases := make([][]aggregate.DocumentAddress, 0, len(indexes))
	for _, index := range indexes {
		loaded, present := plan.assessment.qualified.records.At(index)
		if !present {
			return nil, fmt.Errorf("native recovery document facts are incomplete")
		}
		aliases = append(aliases, loaded.Group().Aliases())
	}
	return aliases, nil
}

// ObservedDocuments projects the plan's qualified observation without exposing
// a complete Registry that could be mistaken for a publication candidate.
func (plan NativeRecoveryPlan) ObservedDocuments(scope ownership.DocumentScope) (ownership.DocumentRegistryView, error) {
	return plan.assessment.registry.Documents(scope)
}
