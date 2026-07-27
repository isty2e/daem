package profile

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
)

// ValidateManagedPathOccupancy checks persisted structural identity against
// the current profile-selected placement without treating entity identity as a
// realization discriminator.
func ValidateManagedPathOccupancy(
	entityID entity.ID,
	placementID string,
	consumerTargets []target.Target,
	scope target.Scope,
	destination string,
	contentKind realization.PathProjectionContentKind,
) error {
	if err := entityID.Validate(); err != nil {
		return fmt.Errorf("managed path entity: %w", err)
	}
	parsedDestination, err := output.Parse(destination)
	if err != nil {
		return err
	}
	if err := parsedDestination.ValidateScope(scope); err != nil {
		return err
	}
	if entityID.Kind() == entity.KindHookAsset {
		placement, err := HookAssetPlacementFor(scope, consumerTargets)
		if err != nil {
			return err
		}
		if placement.ID() != placementID {
			return fmt.Errorf("HookAsset file placement %q is not selected by its consumers", placementID)
		}
		if contentKind != realization.PathProjectionFile {
			return fmt.Errorf("HookAsset placement %q requires file content", placementID)
		}
		if !slices.Equal(placement.ConsumerTargets(), consumerTargets) {
			return fmt.Errorf("HookAsset placement %q consumer set is not canonical", placementID)
		}
		if _, err := placement.ContentHash(entityID.Name(), parsedDestination); err != nil {
			return err
		}
		return nil
	}

	resourceKind := entityID.Kind()
	if contentKind == realization.PathProjectionFile {
		var selected SelectedManagedPathPlacement
		for index, consumer := range consumerTargets {
			candidate, err := ManagedFilePlacementFor(resourceKind, consumer, scope, parsedDestination)
			if err != nil {
				return err
			}
			if index == 0 {
				selected = candidate
				continue
			}
			selected, err = MergeManagedPathPlacements(selected, candidate)
			if err != nil {
				return err
			}
		}
		if selected.ID() != placementID {
			return fmt.Errorf("managed file placement %q is not selected by its consumers", placementID)
		}
		if !slices.Equal(selected.ConsumerTargets(), consumerTargets) {
			return fmt.Errorf("managed file placement %q consumer set is not canonical", placementID)
		}
		return nil
	}

	selected, err := ManagedPathPlacementForConsumers(
		resourceKind,
		scope,
		placementID,
		consumerTargets,
	)
	if err != nil {
		return err
	}
	if _, err := selected.ChildName(parsedDestination); err != nil {
		return err
	}
	writeRoute, err := ManagedPathOperationRoute(selected, OperationWrite)
	if err != nil {
		return err
	}
	spec, err := selected.Realize(parsedDestination, realization.PathProjectionCopy, writeRoute)
	if err != nil {
		return err
	}
	projection, _ := spec.ManagedPathProjection()
	if projection.ContentKind() != contentKind {
		return fmt.Errorf(
			"managed path placement %q content kind %q does not match %q",
			placementID,
			projection.ContentKind(),
			contentKind,
		)
	}
	if !slices.Equal(projection.ConsumerTargets(), consumerTargets) {
		return fmt.Errorf("managed path placement %q consumer set is not canonical", placementID)
	}
	return nil
}
