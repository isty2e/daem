package mcptest

import (
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
)

// OperationsForPlacementID resolves codec operations for a placement in tests.
func OperationsForPlacementID(
	id aggregate.MCPPlacementID,
) (mcpcodec.MCPPlacementOperations, bool) {
	placement, ok := aggregate.MCPPlacementForID(id)
	if !ok {
		return mcpcodec.MCPPlacementOperations{}, false
	}
	return mcpcodec.ImplementedMCPPlacementOperationsForCodecContract(
		placement.CodecContractID(),
	)
}
