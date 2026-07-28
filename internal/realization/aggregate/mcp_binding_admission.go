package aggregate

import (
	"fmt"
	"sort"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
)

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
	env := stdio.Env()
	childNames := make([]string, 0, len(env))
	for childName := range env {
		childNames = append(childNames, childName)
	}
	sort.Strings(childNames)
	for _, childName := range childNames {
		if err := placement.AdmitEnvironmentReference(childName, env[childName].FromEnv()); err != nil {
			return MCPPlacement{}, err
		}
	}
	return placement, nil
}
