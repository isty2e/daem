package lock

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/profile"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func validateAdmittedSkillPathProjection(contract LockedSubjectContract) (bool, error) {
	spec, realized := contract.Realization()
	if !realized {
		return false, nil
	}
	projection, pathProjection := spec.ManagedPathProjection()
	if !pathProjection || contract.EntityID().Kind() != entity.KindSkill {
		return false, nil
	}

	selected, err := profile.ManagedPathPlacementForConsumers(
		entity.KindSkill,
		projection.Scope(),
		projection.PlacementID(),
		projection.ConsumerTargets(),
	)
	if err != nil {
		return true, err
	}
	if _, err := selected.ChildName(projection.Destination()); err != nil {
		return true, fmt.Errorf("Skill path projection destination: %w", err)
	}
	writeRoute, err := profile.ManagedPathOperationRoute(selected, profile.OperationWrite)
	if err != nil {
		return true, err
	}
	removeRoute, err := profile.ManagedPathOperationRoute(selected, profile.OperationRemove)
	if err != nil {
		return true, err
	}
	expectedRealization, err := selected.Realize(projection.Destination(), projection.PlacementMode(), writeRoute)
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
		return true, fmt.Errorf("Skill path projection contract does not match canonical profile refinement")
	}
	return true, nil
}
