package adopt

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
)

type mergeResourceKind uint8

const (
	mergeResourceInvalid mergeResourceKind = iota
	mergeResourceEntity
	mergeResourceManifest
)

// MergeResource identifies an entity decision or an explicit manifest decision.
type MergeResource struct {
	kind   mergeResourceKind
	entity entity.ID
}

func NewEntityMergeResource(kind entity.Kind, name string) (MergeResource, error) {
	id, err := entity.New(kind, name)
	if err != nil {
		return MergeResource{}, err
	}
	return MergeResource{kind: mergeResourceEntity, entity: id}, nil
}

func ManifestMergeResource() MergeResource {
	return MergeResource{kind: mergeResourceManifest}
}

func (resource MergeResource) EntityID() (entity.ID, bool) {
	return resource.entity, resource.kind == mergeResourceEntity
}

func (resource MergeResource) IsManifest() bool {
	return resource.kind == mergeResourceManifest
}

func (resource MergeResource) Validate() error {
	switch resource.kind {
	case mergeResourceEntity:
		return resource.entity.Validate()
	case mergeResourceManifest:
		if resource.entity != (entity.ID{}) {
			return fmt.Errorf("manifest merge resource cannot carry an entity")
		}
		return nil
	default:
		return fmt.Errorf("merge resource is required")
	}
}

type mergeResourceIdentity struct {
	Kind   mergeResourceKind
	Entity string
}

func (resource MergeResource) identity() mergeResourceIdentity {
	return mergeResourceIdentity{Kind: resource.kind, Entity: resource.entity.String()}
}
