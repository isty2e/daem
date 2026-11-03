package profile

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// KeyOperationRoute describes a static native-key operation, not an installed
// executor or a grant over the containing document.
func (placement HostConfigPlacement) KeyOperationRoute(operation Operation) (OperationRoute, error) {
	if err := placement.Validate(); err != nil {
		return OperationRoute{}, err
	}
	if placement.Ownership() != hostconfig.OwnershipKeys {
		return OperationRoute{}, fmt.Errorf("native key route requires key ownership")
	}
	codec, supported := aggregate.HostConfigCodecForSyntax(string(placement.Grammar()))
	if !supported {
		return OperationRoute{}, fmt.Errorf("native key route has no codec")
	}
	var route string
	switch operation {
	case OperationWrite:
		route = "managed-host-config-key.write"
	case OperationRemove:
		route = "managed-host-config-key.remove"
	default:
		return OperationRoute{}, fmt.Errorf("unsupported native key operation %q", operation)
	}
	return NewOperationRoute(entity.KindHostConfig, operation, placement.ID(), route, string(codec))
}
