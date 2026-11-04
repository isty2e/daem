package journal

import (
	"fmt"
	"slices"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func nativeNamespaceScope(namespaces []journalnamespace.DocumentNamespace) (ownership.DocumentScope, error) {
	var exact []pathauthority.Exact
	var missing []pathauthority.Provisional
	for _, namespace := range namespaces {
		if err := namespace.Observation().Validate(); err != nil {
			return ownership.DocumentScope{}, err
		}
		if physical, present := namespace.Observation().Exact(); present {
			exact = append(exact, physical)
		} else if provisional, present := namespace.Observation().Provisional(); present {
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
	selected := make([]journalnamespace.DocumentNamespace, 0, len(aliases))
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
