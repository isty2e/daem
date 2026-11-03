package recoverygate

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
)

// DocumentScope projects retained qualified comparisons, never live rights.
func DocumentScope(authority mutation.PhysicalAuthoritySet) (ownership.DocumentScope, error) {
	observations, err := authority.Namespaces()
	if err != nil {
		return ownership.DocumentScope{}, err
	}
	var exact []pathauthority.Exact
	var missing []pathauthority.Provisional
	for _, observation := range observations {
		if value, present := observation.Exact(); present {
			exact = append(exact, value)
		} else if value, present := observation.Provisional(); present {
			missing = append(missing, value)
		}
	}
	return ownership.NewDocumentScope(exact, missing)
}

func ObserveDocumentRegistry(ctx context.Context, path string, authority mutation.PhysicalAuthoritySet) (ownership.DocumentRegistryView, error) {
	scope, err := DocumentScope(authority)
	if err != nil {
		return ownership.DocumentRegistryView{}, err
	}
	store, err := ownershipstore.New(path)
	if err != nil {
		return ownership.DocumentRegistryView{}, err
	}
	registry, err := store.Load(ctx)
	if err != nil {
		return ownership.DocumentRegistryView{}, err
	}
	if err := scope.RequireWritable(registry); err != nil {
		return ownership.DocumentRegistryView{}, err
	}
	return registry.Documents(scope)
}
