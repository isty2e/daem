package mcpcodec

import "github.com/isty2e/daem/internal/realization/aggregate"

func mcpPlacementOperationsForID(
	id aggregate.MCPPlacementID,
) (MCPPlacementOperations, bool) {
	placement, ok := aggregate.MCPPlacementForID(id)
	if !ok {
		return MCPPlacementOperations{}, false
	}
	return ImplementedMCPPlacementOperationsForPlacement(placement.ID())
}
