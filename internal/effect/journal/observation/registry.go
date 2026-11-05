package observation

import (
	"fmt"
	"slices"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func (batch Batch) RegistryScope() (ownership.DocumentScope, error) {
	if !batch.initialized {
		return ownership.DocumentScope{}, fmt.Errorf("native document observation is required")
	}
	return journalnamespace.RegistryScope(batch.namespaces)
}

// RegistryScopeFor selects actual document mutation/baseline intents; merely
// retaining an exclusive observation for a no-op member does not select it.
func (batch Batch) RegistryScopeFor(aliases []aggregate.DocumentAddress) (ownership.DocumentScope, error) {
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
	return journalnamespace.RegistryScope(selected)
}
