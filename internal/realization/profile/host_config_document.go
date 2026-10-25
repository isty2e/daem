package profile

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
)

// DocumentOperationRoute selects only a static native whole-document route.
// It does not install that route in the default executor.
func (placement HostConfigPlacement) DocumentOperationRoute(operation Operation) (OperationRoute, error) {
	if err := placement.Validate(); err != nil {
		return OperationRoute{}, err
	}
	if placement.Ownership() != hostconfig.OwnershipDocument {
		return OperationRoute{}, fmt.Errorf("whole-document route requires document ownership")
	}
	var route string
	switch operation {
	case OperationWrite:
		route = "managed-host-config-document.write"
	case OperationRemove:
		route = "managed-host-config-document.remove"
	default:
		return OperationRoute{}, fmt.Errorf("unsupported native document operation %q", operation)
	}
	return NewOperationRoute(entity.KindHostConfig, operation, placement.ID(), route, "native-document.v1")
}

// DocumentProjection retains whole-file identity and the private output policy.
func (placement HostConfigPlacement) DocumentProjection() (realization.RealizationSpec, error) {
	route, err := placement.DocumentOperationRoute(OperationWrite)
	if err != nil {
		return realization.RealizationSpec{}, err
	}
	mode, err := realization.NewExactPathPermissionMode(0o600)
	if err != nil {
		return realization.RealizationSpec{}, err
	}
	return realization.NewManagedPathProjection(realization.ManagedPathProjectionInput{
		PlacementID: placement.ID(), ConsumerTargets: []target.Target{placement.Target()}, Scope: placement.Scope(),
		Destination: placement.Destination(), ContentKind: realization.PathProjectionFile,
		PlacementMode: realization.PathProjectionCopy, PermissionPolicy: realization.PathPermissionsExact,
		ExactPermissionMode: mode, AdapterContractVersion: route.AdapterContractVersion(),
	})
}
