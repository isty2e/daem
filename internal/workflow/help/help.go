package help

import (
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

// UsageFacts contains workflow-owned facts used to render CLI usage text.
type UsageFacts struct {
	SupportedTargets          []target.Target
	ImportTargets             []target.Target
	MCPAuthoringPlacements    []MCPPlacementFact
	MCPRuntimeProbePlacements []MCPPlacementFact
}

// MCPPlacementFact preserves one admitted target/scope correlation for help.
type MCPPlacementFact struct {
	Target target.Target
	Scope  target.Scope
}

// BuildUsageFacts returns static target facts needed by command help.
func BuildUsageFacts() UsageFacts {
	return UsageFacts{
		SupportedTargets:          target.SupportedTargets(),
		ImportTargets:             profile.ImportableTargets(),
		MCPAuthoringPlacements:    mcpPlacementFacts(aggregate.ImplementedMCPPlacements()),
		MCPRuntimeProbePlacements: mcpPlacementFacts(aggregatecodec.MCPRuntimeProbePlacements()),
	}
}

func mcpPlacementFacts(placements []aggregate.MCPPlacement) []MCPPlacementFact {
	facts := make([]MCPPlacementFact, 0, len(placements))
	for _, placement := range placements {
		facts = append(facts, MCPPlacementFact{
			Target: placement.Target(),
			Scope:  placement.Scope(),
		})
	}
	return facts
}

// InitHintManifestPath resolves the manifest path displayed in missing-manifest init hints.
func InitHintManifestPath(manifestPath string) (string, error) {
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		return "", err
	}
	return paths.ManifestPath, nil
}
