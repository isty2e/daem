// Package refine owns pure family-specific refinement from canonical Desired,
// Topology, Supply, and static Surface facts into canonical locked contracts.
package refine

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// SkillPathProjections refines one Skill through the static placement profile.
func SkillPathProjections(value skill.Skill) ([]lock.LockedSubjectContract, error) {
	placements, err := profile.ManagedPathPlacementsFor(
		entity.KindSkill,
		value.Scope(),
		value.Targets(),
	)
	if err != nil {
		return nil, fmt.Errorf("select skill %q placements: %w", value.ID().Name(), err)
	}
	mode := realization.PathProjectionMode(value.InstallMode())
	contracts := make([]lock.LockedSubjectContract, 0, len(placements))
	for _, placement := range placements {
		destination, err := placement.ChildDestination(value.InstallName())
		if err != nil {
			return nil, fmt.Errorf("lower skill %q destination: %w", value.ID().Name(), err)
		}
		writeRoute, err := profile.ManagedPathOperationRoute(placement, profile.OperationWrite)
		if err != nil {
			return nil, fmt.Errorf("select skill %q placement %q write route: %w", value.ID().Name(), placement.ID(), err)
		}
		removeRoute, err := profile.ManagedPathOperationRoute(placement, profile.OperationRemove)
		if err != nil {
			return nil, fmt.Errorf("select skill %q placement %q remove route: %w", value.ID().Name(), placement.ID(), err)
		}
		spec, err := placement.Realize(destination, mode, writeRoute)
		if err != nil {
			return nil, fmt.Errorf("realize skill %q placement %q: %w", value.ID().Name(), placement.ID(), err)
		}
		subjectID, err := topologyprojection.Subject(value.ID(), placement.ID())
		if err != nil {
			return nil, fmt.Errorf("lower skill %q projection topology: %w", value.ID().Name(), err)
		}
		contract, err := lock.NewManagedPathSubjectContract(lock.ManagedPathSubjectInput{
			EntityID:      value.ID(),
			SubjectID:     subjectID,
			Realization:   spec,
			WriteRouteID:  writeRoute.RouteID(),
			RemoveRouteID: removeRoute.RouteID(),
		})
		if err != nil {
			return nil, fmt.Errorf("lock skill %q projection %q: %w", value.ID().Name(), placement.ID(), err)
		}
		contracts = append(contracts, contract)
	}
	return contracts, nil
}
