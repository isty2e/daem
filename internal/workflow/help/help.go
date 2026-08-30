package help

import (
	"github.com/isty2e/daem/internal/hostsurface/catalog"
	daempaths "github.com/isty2e/daem/internal/paths"
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
	views := catalog.Product().MCPInOwnerOrder()
	return UsageFacts{
		SupportedTargets:          target.SupportedTargets(),
		ImportTargets:             profile.ImportableTargets(),
		MCPAuthoringPlacements:    mcpPlacementFacts(views),
		MCPRuntimeProbePlacements: mcpRuntimeProbePlacementFacts(views),
	}
}

func mcpRuntimeProbePlacementFacts(views []catalog.SurfaceView) []MCPPlacementFact {
	facts := make([]MCPPlacementFact, 0)
	for _, view := range views {
		if _, ok := view.RuntimeProbe(); !ok {
			continue
		}
		facts = append(facts, MCPPlacementFact{
			Target: view.Key().Target(),
			Scope:  view.Key().Scope(),
		})
	}
	return facts
}

func mcpPlacementFacts(views []catalog.SurfaceView) []MCPPlacementFact {
	facts := make([]MCPPlacementFact, 0, len(views))
	for _, view := range views {
		facts = append(facts, MCPPlacementFact{
			Target: view.Key().Target(),
			Scope:  view.Key().Scope(),
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
