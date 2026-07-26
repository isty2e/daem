package aggregate

import (
	"fmt"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
)

// MCPEnvUnsupportedError reports that one otherwise-admitted placement cannot
// represent environment references. Human host labels belong to presentation.
type MCPEnvUnsupportedError struct {
	placementID MCPPlacementID
}

func (failure MCPEnvUnsupportedError) Error() string {
	return fmt.Sprintf("MCP placement %q does not support env", failure.placementID)
}

// PlacementID returns the rejected aggregate placement identity.
func (failure MCPEnvUnsupportedError) PlacementID() MCPPlacementID { return failure.placementID }

// MCPPlacementForBinding validates one Desired binding against the implemented
// placement catalog and returns the single placement that admits it.
func MCPPlacementForBinding(binding desiredmcp.Binding) (MCPPlacement, error) {
	if err := binding.Validate(); err != nil {
		return MCPPlacement{}, err
	}
	placement, ok := ImplementedMCPPlacement(binding.Target(), binding.Scope())
	if !ok {
		if TargetHasImplementedMCPPlacement(binding.Target()) {
			return MCPPlacement{}, fmt.Errorf("unsupported MCP scope %q", binding.Scope())
		}
		return MCPPlacement{}, fmt.Errorf("unsupported MCP target %q", binding.Target())
	}
	stdio, ok := binding.Transport().Stdio()
	if !ok {
		return MCPPlacement{}, fmt.Errorf("unsupported MCP transport %q", binding.Transport().Kind())
	}
	if len(stdio.Env()) != 0 && !placement.SupportsEnv() {
		return MCPPlacement{}, MCPEnvUnsupportedError{placementID: placement.ID()}
	}
	return placement, nil
}
