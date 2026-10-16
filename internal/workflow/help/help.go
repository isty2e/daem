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

// ManifestHintPath resolves a raw selector to its intended hint target.
func ManifestHintPath(manifestPath string) (string, error) {
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		return "", err
	}
	return paths.ManifestPath, nil
}

// ImplicitManifestHintPath returns a discovered target eligible for elision.
// Explicit requests require cwd discovery to retain project-placement admission.
func ImplicitManifestHintPath(requestedManifestPath string) string {
	paths, err := daempaths.Resolve("")
	if err != nil || (requestedManifestPath != "" && paths.ManifestOrigin != daempaths.ManifestOriginCWD) {
		return ""
	}
	return paths.ManifestPath
}

// CreationManifestHintPath returns the implicit creation target, or empty when
// it cannot be established. Unlike existing-workspace discovery, it need not exist.
func CreationManifestHintPath() string {
	paths, err := daempaths.ResolveCreation("")
	if err != nil {
		return ""
	}
	return paths.ManifestPath
}
