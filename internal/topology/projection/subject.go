// Package projection owns stable topology identity for target-visible projections.
package projection

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/topology"
)

// Subject lowers one canonical entity and static placement identity into a
// target-visible projection subject. Placement facts remain owned elsewhere.
func Subject(id entity.ID, placementID string) (topology.SubjectID, error) {
	if err := id.Validate(); err != nil {
		return topology.SubjectID{}, fmt.Errorf("projection subject entity: %w", err)
	}
	return topology.NewSubjectID(topology.SubjectProjection, placementID, id.String())
}

// EntityID returns the canonical Desired entity carried by a placement
// projection subject. Projection families with a different key grammar are not
// accepted as entity-backed projections.
func EntityID(subject topology.SubjectID) (entity.ID, bool) {
	if subject.Kind() != topology.SubjectProjection {
		return entity.ID{}, false
	}
	id, err := entity.Parse(subject.Key())
	if err != nil {
		return entity.ID{}, false
	}
	return id, true
}
