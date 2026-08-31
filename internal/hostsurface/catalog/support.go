package catalog

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func (catalog Catalog) withResourceSupports(supports []profile.Support) (Catalog, error) {
	catalog.resourceSupportByKey = make(map[managedPathSupportKey]profile.Support, len(supports))
	catalog.resourceSupportOrder = make([]managedPathSupportKey, 0, len(supports))
	for _, support := range supports {
		if err := support.Validate(); err != nil {
			return Catalog{}, fmt.Errorf("host-surface resource support: %w", err)
		}
		key := managedPathSupportKey{target: support.Target(), kind: support.ResourceKind()}
		if _, duplicate := catalog.resourceSupportByKey[key]; duplicate {
			return Catalog{}, fmt.Errorf(
				"host-surface duplicate resource support for %s/%s",
				support.Target(),
				support.ResourceKind(),
			)
		}
		catalog.resourceSupportByKey[key] = support
		catalog.resourceSupportOrder = append(catalog.resourceSupportOrder, key)
	}
	return catalog, nil
}

// ResourceSupportsForTarget returns compiled support facts in owner order.
func (catalog Catalog) ResourceSupportsForTarget(
	selectedTarget target.Target,
) []profile.Support {
	result := make([]profile.Support, 0)
	for _, key := range catalog.resourceSupportOrder {
		if key.target == selectedTarget {
			result = append(result, catalog.resourceSupportByKey[key])
		}
	}
	return result
}

// ResourceSupport returns one compiled target/resource support fact.
func (catalog Catalog) ResourceSupport(
	selectedTarget target.Target,
	kind entity.Kind,
) (profile.Support, bool) {
	support, ok := catalog.resourceSupportByKey[managedPathSupportKey{
		target: selectedTarget,
		kind:   kind,
	}]
	return support, ok
}
