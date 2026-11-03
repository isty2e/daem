package journal

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func nativeNamespaceScope(namespaces []NativeDocumentNamespace) (ownership.DocumentScope, error) {
	var exact []pathauthority.Exact
	var missing []pathauthority.Provisional
	for _, namespace := range namespaces {
		if err := namespace.observation.Validate(); err != nil {
			return ownership.DocumentScope{}, err
		}
		if physical, present := namespace.observation.Exact(); present {
			exact = append(exact, physical)
		} else if provisional, present := namespace.observation.Provisional(); present {
			missing = append(missing, provisional)
		} else {
			return ownership.DocumentScope{}, fmt.Errorf("native Registry scope has no namespace observation")
		}
	}
	return ownership.NewDocumentScope(exact, missing)
}

func (batch NativeDocumentObservationBatch) RegistryScope() (ownership.DocumentScope, error) {
	if !batch.initialized {
		return ownership.DocumentScope{}, fmt.Errorf("native document observation is required")
	}
	return nativeNamespaceScope(batch.namespaces)
}

// RegistryScopeFor selects actual document mutation/baseline intents; merely
// retaining an exclusive observation for a no-op member does not select it.
func (batch NativeDocumentObservationBatch) RegistryScopeFor(aliases []aggregate.DocumentAddress) (ownership.DocumentScope, error) {
	if !batch.initialized {
		return ownership.DocumentScope{}, fmt.Errorf("native document observation is required")
	}
	selected := make([]NativeDocumentNamespace, 0, len(aliases))
	seen := make(map[int]struct{})
	for _, alias := range aliases {
		found := false
		for index, namespace := range batch.namespaces {
			if slices.Contains(namespace.Addresses(), alias) {
				if _, present := seen[index]; !present {
					selected = append(selected, namespace)
					seen[index] = struct{}{}
				}
				found = true
				break
			}
		}
		if !found {
			return ownership.DocumentScope{}, fmt.Errorf("native mutation has no qualified namespace")
		}
	}
	return nativeNamespaceScope(selected)
}

func (operation *NativeRecoveryOperation) RegistryScope() (ownership.DocumentScope, error) {
	if operation == nil || operation.closed {
		return ownership.DocumentScope{}, fmt.Errorf("live native recovery operation is required")
	}
	return operation.plan.registryScope, nil
}

func (assessment nativeDocumentAssessment) documentWriteIndexes() ([]int, error) {
	records := assessment.qualified.records
	if len(records.groups) != len(records.claims) || len(records.groups) != len(assessment.stages) {
		return nil, fmt.Errorf("native recovery document facts are incomplete")
	}
	var indexes []int
	for index, group := range records.groups {
		comparison := group.comparison
		if err := comparison.Validate(); err != nil {
			return nil, err
		}
		changed := !comparison.before.Equal(comparison.expected) ||
			!comparison.beforeState.SameRootMetadata(comparison.expectedState) || assessment.stages[index].restoration.Changed()
		claims := records.claims[index]
		changed = changed || !slices.EqualFunc(claims.Before(), claims.Prepared(), ownership.Claim.Equal) ||
			!slices.EqualFunc(claims.Prepared(), claims.After(), ownership.Claim.Equal)
		for _, intent := range group.bindingIntents {
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
	if len(assessment.qualified.records.groups) != len(assessment.qualified.namespaces) {
		return ownership.DocumentScope{}, fmt.Errorf("live native namespace qualification is required before freezing scope")
	}
	indexes, err := assessment.documentWriteIndexes()
	if err != nil {
		return ownership.DocumentScope{}, err
	}
	namespaces := make([]NativeDocumentNamespace, 0, len(indexes))
	for _, index := range indexes {
		namespaces = append(namespaces, assessment.qualified.namespaces[index])
	}
	return nativeNamespaceScope(namespaces)
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
		aliases = append(aliases, slices.Clone(plan.assessment.qualified.records.groups[index].aliases))
	}
	return aliases, nil
}

// ObservedDocuments projects the plan's qualified observation without exposing
// a complete Registry that could be mistaken for a publication candidate.
func (plan NativeRecoveryPlan) ObservedDocuments(scope ownership.DocumentScope) (ownership.DocumentRegistryView, error) {
	return plan.assessment.registry.Documents(scope)
}
