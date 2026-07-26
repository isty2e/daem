package lock

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/profile"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
)

func validateAdmittedHookAssetPathProjection(contract LockedSubjectContract) (bool, error) {
	spec, realized := contract.Realization()
	if !realized {
		return false, nil
	}
	projection, managedPath := spec.ManagedPathProjection()
	if !managedPath || contract.EntityID().Kind() != entity.KindHookAsset {
		return false, nil
	}
	if projection.ContentKind() != realization.PathProjectionFile ||
		projection.PlacementMode() != realization.PathProjectionCopy {
		return true, fmt.Errorf("HookAsset projection must be a copied file")
	}
	placement, err := profile.HookAssetPlacementFor(projection.Scope(), projection.ConsumerTargets())
	if err != nil {
		return true, err
	}
	if placement.ID() != projection.PlacementID() {
		return true, fmt.Errorf("HookAsset projection placement %q is not selected by its consumers", projection.PlacementID())
	}
	contentHash, err := placement.ContentHash(contract.EntityID().Name(), projection.Destination())
	if err != nil {
		return true, err
	}
	exactMode, exact := projection.ExactPermissionMode()
	if !exact {
		return true, fmt.Errorf("HookAsset projection requires an exact permission mode")
	}
	nonExecutableMode, err := placement.ExactPermissionMode(false)
	if err != nil {
		return true, err
	}
	executableMode, err := placement.ExactPermissionMode(true)
	if err != nil {
		return true, err
	}
	executable := false
	switch exactMode.FileMode() {
	case nonExecutableMode.FileMode():
	case executableMode.FileMode():
		executable = true
	default:
		return true, fmt.Errorf("HookAsset projection exact permission mode must be 0600 or 0700")
	}
	writeRoute, err := profile.HookAssetOperationRoute(placement, profile.OperationWrite)
	if err != nil {
		return true, err
	}
	removeRoute, err := profile.HookAssetOperationRoute(placement, profile.OperationRemove)
	if err != nil {
		return true, err
	}
	expectedRealization, err := placement.Realize(contract.EntityID().Name(), contentHash, executable, writeRoute)
	if err != nil {
		return true, err
	}
	expectedSubject, err := topologyhook.AssetSubjectID(contract.EntityID(), projection.Scope())
	if err != nil {
		return true, err
	}
	expected, err := NewManagedPathSubjectContract(ManagedPathSubjectInput{
		EntityID: contract.EntityID(), SubjectID: expectedSubject, Realization: expectedRealization,
		WriteRouteID: writeRoute.RouteID(), RemoveRouteID: removeRoute.RouteID(),
	})
	if err != nil {
		return true, err
	}
	if !contract.Equal(expected) {
		return true, fmt.Errorf("HookAsset path projection contract does not match canonical profile refinement")
	}
	return true, nil
}

func validateHookAssetPathProjectionCollection(index lockedCollectionIndex) error {
	for subject, contract := range index.pathProjectionContracts {
		if contract.EntityID().Kind() != entity.KindHookAsset {
			continue
		}
		supply, ok := index.exactSupplyByEntity[contract.EntityID()]
		if !ok {
			return fmt.Errorf("HookAsset path subject %q has no exact Supply", subject)
		}
		materialized, ok := supply.MaterializedFileIdentity()
		if !ok {
			return fmt.Errorf("HookAsset path subject %q has no materialized identity", subject)
		}
		spec, _ := contract.Realization()
		projection, _ := spec.ManagedPathProjection()
		placement, err := profile.HookAssetPlacementFor(projection.Scope(), projection.ConsumerTargets())
		if err != nil {
			return err
		}
		lockedHash, err := placement.ContentHash(contract.EntityID().Name(), projection.Destination())
		if err != nil {
			return err
		}
		if lockedHash != materialized.ContentHash() {
			return fmt.Errorf("HookAsset path subject %q destination hash does not match materialized identity", subject)
		}
		fileUse, ok := supply.ExactFileUse()
		if !ok {
			return fmt.Errorf("HookAsset path subject %q has no exact file use", subject)
		}
		exactMode, ok := projection.ExactPermissionMode()
		if !ok {
			return fmt.Errorf("HookAsset path subject %q has no exact permission mode", subject)
		}
		expectedMode, err := placement.ExactPermissionMode(fileUse.Executable())
		if err != nil {
			return err
		}
		if !exactMode.Equal(expectedMode) {
			return fmt.Errorf("HookAsset path subject %q exact permission mode does not match file use", subject)
		}
	}
	return nil
}
