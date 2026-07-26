// Package resource lowers canonical Desired entity identity into exact-Supply
// resource topology.
package resource

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/topology"
)

// Subject lowers one canonical Desired entity into its exact-Supply resource subject.
func Subject(id entity.ID) (topology.SubjectID, error) {
	if err := id.Validate(); err != nil {
		return topology.SubjectID{}, fmt.Errorf("resource subject entity: %w", err)
	}
	return topology.NewSubjectID(topology.SubjectResource, string(id.Kind()), id.Name())
}
