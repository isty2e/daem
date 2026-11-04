package namespace

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/output/ownership"
)

// RegistryScope projects observed namespaces into read-selection facts only.
func RegistryScope(namespaces []DocumentNamespace) (ownership.DocumentScope, error) {
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
