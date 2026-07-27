package profile

import (
	"fmt"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

// MCPRuntimeProbeCapability is one static product-supported probe row.
// Placement owns target/scope identity; this facet owns only probe-specific
// delegate-plan correlation policy.
type MCPRuntimeProbeCapability struct {
	placementID          aggregate.MCPPlacementID
	requiresDelegatePlan bool
}

func newMCPRuntimeProbeCapability(
	placementID aggregate.MCPPlacementID,
	requiresDelegatePlan bool,
) (MCPRuntimeProbeCapability, error) {
	if _, ok := aggregate.MCPPlacementForID(placementID); !ok {
		return MCPRuntimeProbeCapability{}, fmt.Errorf(
			"MCP runtime-probe capability placement %q is not implemented",
			placementID,
		)
	}
	capability := MCPRuntimeProbeCapability{
		placementID:          placementID,
		requiresDelegatePlan: requiresDelegatePlan,
	}
	if err := capability.Validate(); err != nil {
		return MCPRuntimeProbeCapability{}, err
	}
	return capability, nil
}

// Validate rejects zero or forged capability rows.
func (capability MCPRuntimeProbeCapability) Validate() error {
	if _, ok := aggregate.MCPPlacementForID(capability.placementID); !ok {
		return fmt.Errorf(
			"MCP runtime-probe capability placement %q is not implemented",
			capability.placementID,
		)
	}
	return nil
}

// Placement returns the canonical aggregate placement admitted by this row.
func (capability MCPRuntimeProbeCapability) Placement() aggregate.MCPPlacement {
	placement, _ := aggregate.MCPPlacementForID(capability.placementID)
	return placement
}

// RequiresDelegatePlan reports whether the locked launch identity must
// independently correlate with the decoded canonical projection.
func (capability MCPRuntimeProbeCapability) RequiresDelegatePlan() bool {
	return capability.requiresDelegatePlan
}

var mcpRuntimeProbeCapabilityCatalog = []MCPRuntimeProbeCapability{
	mustMCPRuntimeProbeCapability(aggregate.MCPPlacementClaudeProject, true),
	mustMCPRuntimeProbeCapability(aggregate.MCPPlacementOpenCodeProject, false),
}

// MCPRuntimeProbeCapabilities returns all static probe rows in stable product order.
func MCPRuntimeProbeCapabilities() []MCPRuntimeProbeCapability {
	return append([]MCPRuntimeProbeCapability(nil), mcpRuntimeProbeCapabilityCatalog...)
}

func profileMCPRuntimeProbeCapabilities(
	selectedTarget target.Target,
) []MCPRuntimeProbeCapability {
	result := make([]MCPRuntimeProbeCapability, 0)
	for _, capability := range mcpRuntimeProbeCapabilityCatalog {
		if capability.Placement().Target() == selectedTarget {
			result = append(result, capability)
		}
	}
	return result
}

func mustMCPRuntimeProbeCapability(
	placementID aggregate.MCPPlacementID,
	requiresDelegatePlan bool,
) MCPRuntimeProbeCapability {
	capability, err := newMCPRuntimeProbeCapability(placementID, requiresDelegatePlan)
	if err != nil {
		panic(err)
	}
	return capability
}

func validateMCPRuntimeProbeCapabilityCatalog(
	capabilities []MCPRuntimeProbeCapability,
) error {
	seen := make(map[aggregate.MCPPlacementID]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if err := capability.Validate(); err != nil {
			return err
		}
		placementID := capability.Placement().ID()
		if _, duplicate := seen[placementID]; duplicate {
			return fmt.Errorf(
				"MCP runtime-probe capabilities share placement %q",
				placementID,
			)
		}
		seen[placementID] = struct{}{}
	}
	return nil
}
