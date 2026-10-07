package aggregate

import (
	"fmt"
	"regexp"
	"strings"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/target"
)

const MCPCodecPiNativeStdio CodecContractID = "pi-native-mcp-stdio-v1"

var piNativeServerName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// PiMCPProjectionCodec projects recorded backend intent into its file contract.
func PiMCPProjectionCodec(backend desiredmcp.Backend) CodecContractID {
	switch backend {
	case desiredmcp.BackendNative:
		return MCPCodecPiNativeStdio
	case desiredmcp.BackendAdapter:
		return MCPCodecPiAdapterStdio
	default:
		return ""
	}
}

// PiNativeMCPNamespace validates the name and projects its upstream tool namespace.
func PiNativeMCPNamespace(name string) (string, error) {
	if !piNativeServerName.MatchString(name) {
		return "", fmt.Errorf("native Pi MCP server name must contain only letters, digits, underscores and hyphens")
	}
	return strings.ReplaceAll(name, "-", "_"), nil
}

// ImplementedMCPContracts returns projection variants without duplicating writable physical placements.
func ImplementedMCPContracts() []MCPPlacement {
	contracts := ImplementedMCPPlacements()
	for _, id := range []MCPPlacementID{MCPPlacementPiProject, MCPPlacementPiGlobal} {
		placement, ok := MCPPlacementForCodec(id, MCPCodecPiNativeStdio)
		if !ok {
			panic("native Pi MCP contract has no physical placement")
		}
		contracts = append(contracts, placement)
	}
	return contracts
}

// MCPPlacementForCodec resolves a stored projection contract without changing its physical placement.
func MCPPlacementForCodec(id MCPPlacementID, codec CodecContractID) (MCPPlacement, bool) {
	placement, ok := MCPPlacementForID(id)
	if !ok {
		return MCPPlacement{}, false
	}
	if codec == placement.CodecContractID() {
		return placement, true
	}
	if placement.Target() != target.TargetPi || codec != MCPCodecPiNativeStdio {
		return MCPPlacement{}, false
	}

	placement.codecContractID = codec
	placement.comparedFields = canonicalTokenSet([]string{"command", "args", "env", "enabled", "exposure"})
	return placement, true
}
