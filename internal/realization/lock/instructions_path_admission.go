package lock

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/profile"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func validateAdmittedInstructionsPathProjection(contract LockedSubjectContract) (bool, error) {
	spec, realized := contract.Realization()
	if !realized {
		return false, nil
	}
	projection, pathProjection := spec.ManagedPathProjection()
	if !pathProjection || contract.EntityID().Kind() != entity.KindInstructions {
		return false, nil
	}
	if projection.ContentKind() != realization.PathProjectionFile {
		return true, fmt.Errorf("Instructions path projection must realize a file")
	}
	if projection.PlacementMode() != realization.PathProjectionCopy &&
		projection.PlacementMode() != realization.PathProjectionSymlink {
		return true, fmt.Errorf("Instructions path projection mode %q is unsupported", projection.PlacementMode())
	}

	var selected profile.ManagedPathPlacement
	for index, consumer := range projection.ConsumerTargets() {
		candidate, err := profile.ManagedFilePlacementFor(
			entity.KindInstructions,
			consumer,
			projection.Scope(),
			projection.Destination(),
		)
		if err != nil {
			return true, err
		}
		if index == 0 {
			selected = candidate
			continue
		}
		selected, err = profile.MergeManagedPathPlacements(selected, candidate)
		if err != nil {
			return true, err
		}
	}
	if selected.ID() != projection.PlacementID() {
		return true, fmt.Errorf("Instructions path projection placement %q is not selected by its consumers", projection.PlacementID())
	}
	writeRoute, err := profile.ManagedPathOperationRoute(selected, profile.OperationWrite)
	if err != nil {
		return true, err
	}
	removeRoute, err := profile.ManagedPathOperationRoute(selected, profile.OperationRemove)
	if err != nil {
		return true, err
	}
	expectedRealization, err := selected.Realize(selected.Root(), projection.PlacementMode(), writeRoute)
	if err != nil {
		return true, err
	}
	expectedSubject, err := topologyprojection.Subject(contract.EntityID(), selected.ID())
	if err != nil {
		return true, err
	}
	expected, err := NewManagedPathSubjectContract(ManagedPathSubjectInput{
		EntityID:      contract.EntityID(),
		SubjectID:     expectedSubject,
		Realization:   expectedRealization,
		WriteRouteID:  writeRoute.RouteID(),
		RemoveRouteID: removeRoute.RouteID(),
	})
	if err != nil {
		return true, err
	}
	if !contract.Equal(expected) {
		return true, fmt.Errorf("Instructions path projection contract does not match canonical profile refinement")
	}
	return true, nil
}

func validateInstructionsPathProjectionCollection(index lockedCollectionIndex) error {
	for entityID := range index.exactSupplyByEntity {
		if entityID.Kind() == entity.KindInstructions && index.pathProjectionCountByEntity[entityID] == 0 {
			return fmt.Errorf("Instructions exact-Supply entity %q has no managed file projection", entityID)
		}
	}
	return nil
}
