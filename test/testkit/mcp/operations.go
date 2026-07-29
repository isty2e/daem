package mcptest

import (
	"bytes"

	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
)

// OperationsForPlacementID resolves codec operations for a placement in tests.
func OperationsForPlacementID(
	id aggregate.MCPPlacementID,
) (mcpcodec.MCPPlacementOperations, bool) {
	return mcpcodec.ImplementedMCPPlacementOperationsForPlacement(id)
}

// MergeCanonicalEntry exercises the production batch mutation contract for one
// canonical entry.
func MergeCanonicalEntry(
	operations mcpcodec.MCPPlacementOperations,
	existing []byte,
	serverID string,
	canonical []byte,
) ([]byte, error) {
	mutation, err := mcpcodec.NewMCPProjectionUpsert(serverID, canonical)
	if err != nil {
		return nil, err
	}
	return operations.FoldMutations(existing, []mcpcodec.MCPProjectionMutation{mutation})
}

// ExtractCanonicalEntry exercises the production aggregate observation contract
// for one selected entry.
func ExtractCanonicalEntry(
	operations mcpcodec.MCPPlacementOperations,
	existing []byte,
	serverID string,
) ([]byte, bool, error) {
	observation, err := operations.ObserveCanonicalEntries(existing, []string{serverID})
	if err != nil {
		return nil, false, err
	}
	return observation.CanonicalEntry(serverID)
}

// CompareCanonicalEntry compares one observed canonical entry with expected
// canonical bytes through the production observation contract.
func CompareCanonicalEntry(
	operations mcpcodec.MCPPlacementOperations,
	existing []byte,
	serverID string,
	canonical []byte,
) (mcpcodec.MCPProjectionCanonicalComparison, error) {
	contentPath, err := operations.Placement().ContentPath(serverID)
	if err != nil {
		return mcpcodec.MCPProjectionCanonicalComparison{}, err
	}
	observed, present, err := ExtractCanonicalEntry(operations, existing, serverID)
	if err != nil {
		return mcpcodec.MCPProjectionCanonicalComparison{}, err
	}
	return mcpcodec.MCPProjectionCanonicalComparison{
		ContentPath: string(contentPath),
		Present:     present,
		Equivalent:  present && bytes.Equal(observed, canonical),
	}, nil
}
