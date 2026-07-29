package authoring

import (
	"fmt"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

type mcpProviderAuthoringPlan struct {
	extension *declaration.Extension
	warnings  []string
}

func planMCPProviderAuthoring(
	content []byte,
	header declaration.ManifestHeader,
	selectedTarget target.Target,
	selectedScope target.Scope,
) (mcpProviderAuthoringPlan, error) {
	authoringProfile, supported := profile.MCPProviderAuthoringProfileForTarget(selectedTarget)
	if !supported {
		return mcpProviderAuthoringPlan{}, nil
	}

	candidates, err := declaredMCPProviderContributions(content, header, authoringProfile)
	if err != nil {
		return mcpProviderAuthoringPlan{}, err
	}
	if selectedScope == target.ScopeGlobal &&
		!hasProviderCandidateAtScope(candidates, target.ScopeGlobal) {
		return newMCPProviderAuthoringPlan(authoringProfile, selectedScope)
	}
	if len(candidates) == 0 {
		return newMCPProviderAuthoringPlan(authoringProfile, selectedScope)
	}

	selected, err := profile.SelectMCPProviderContribution(
		selectedTarget,
		selectedScope,
		candidates,
	)
	if err != nil {
		return mcpProviderAuthoringPlan{}, err
	}
	providerScope := selected.Provider().Key().Scope()
	return mcpProviderAuthoringPlan{
		warnings: mcpProviderScopeWarnings(
			authoringProfile,
			selectedScope,
			providerScope,
			false,
		),
	}, nil
}

func newMCPProviderAuthoringPlan(
	authoringProfile profile.MCPProviderAuthoringProfile,
	selectedScope target.Scope,
) (mcpProviderAuthoringPlan, error) {
	extensionID, err := authoringProfile.DefaultExtensionID(selectedScope)
	if err != nil {
		return mcpProviderAuthoringPlan{}, err
	}
	extension, err := ExtensionFromAddRequest(
		AddExtensionRequest{
			ID:      extensionID,
			Source:  authoringProfile.DefaultSource(),
			Targets: []string{string(authoringProfile.Target())},
			Scope:   string(selectedScope),
		},
		declaration.ManifestHeader{},
		daempaths.ManifestOriginExplicit,
	)
	if err != nil {
		return mcpProviderAuthoringPlan{}, err
	}
	return mcpProviderAuthoringPlan{
		extension: &extension,
		warnings: mcpProviderScopeWarnings(
			authoringProfile,
			selectedScope,
			selectedScope,
			true,
		),
	}, nil
}

func declaredMCPProviderContributions(
	content []byte,
	header declaration.ManifestHeader,
	authoringProfile profile.MCPProviderAuthoringProfile,
) ([]extensiontopology.Contribution, error) {
	blocks, err := declarationcodec.ScanExtensionBlocks(content)
	if err != nil {
		return nil, err
	}
	candidates := make([]extensiontopology.Contribution, 0)
	for _, block := range blocks {
		contribution, admitted, err := declaredMCPProviderContribution(
			block.Extension,
			header,
			authoringProfile,
		)
		if err != nil {
			return nil, err
		}
		if admitted {
			candidates = append(candidates, contribution)
		}
	}
	return candidates, nil
}

func declaredMCPProviderContribution(
	extension declaration.Extension,
	header declaration.ManifestHeader,
	authoringProfile profile.MCPProviderAuthoringProfile,
) (extensiontopology.Contribution, bool, error) {
	carrier, ok := extensionAuthoringCarrierFor(extension.Carrier)
	if !ok || carrier != authoringProfile.Carrier() {
		return extensiontopology.Contribution{}, false, nil
	}
	effectiveTargets := header.EffectiveTargets(extension.Targets)
	if len(effectiveTargets) != 1 {
		return extensiontopology.Contribution{}, false, fmt.Errorf(
			"extension %q.targets: provider extension supports exactly one target",
			extension.ID,
		)
	}
	selectedTarget, err := target.ParseTarget(effectiveTargets[0])
	if err != nil || selectedTarget != authoringProfile.Target() {
		return extensiontopology.Contribution{}, false, fmt.Errorf(
			"extension %q.targets: provider extension supports only target %q",
			extension.ID,
			authoringProfile.Target(),
		)
	}
	selectedScope, err := target.ParseScope(header.EffectiveScope(extension.Scope))
	if err != nil {
		return extensiontopology.Contribution{}, false, fmt.Errorf(
			"extension %q.scope: %w",
			extension.ID,
			err,
		)
	}
	source, err := extensionCanonicalSourceRef(
		extension,
		carrier,
		fmt.Sprintf("extension %q", extension.ID),
	)
	if err != nil {
		return extensiontopology.Contribution{}, false, err
	}
	key, err := desiredextension.NewCarrierKey(
		carrier,
		selectedTarget,
		selectedScope,
		source,
	)
	if err != nil {
		return extensiontopology.Contribution{}, false, err
	}
	provider, err := extensiontopology.NewCarrier(key)
	if err != nil {
		return extensiontopology.Contribution{}, false, err
	}
	return profile.MCPProviderContributionForTarget(selectedTarget, provider)
}

func hasProviderCandidateAtScope(
	candidates []extensiontopology.Contribution,
	selectedScope target.Scope,
) bool {
	for _, candidate := range candidates {
		if candidate.Provider().Key().Scope() == selectedScope {
			return true
		}
	}
	return false
}

func mcpProviderScopeWarnings(
	authoringProfile profile.MCPProviderAuthoringProfile,
	bindingScope target.Scope,
	providerScope target.Scope,
	created bool,
) []string {
	action := "reuses"
	if created {
		action = "adds"
	}
	label := authoringProfile.ProviderLabel()
	switch {
	case bindingScope == target.ScopeProject &&
		providerScope == target.ScopeProject &&
		authoringProfile.ProjectActivationRequiresTrust():
		return []string{
			fmt.Sprintf(
				"%s MCP %s the project %s package; the host may not activate project package/config changes until the project is trusted",
				authoringProfile.Target(),
				action,
				label,
			),
		}
	case providerScope == target.ScopeGlobal &&
		authoringProfile.GlobalProviderSharedAcrossProjects() &&
		authoringProfile.GlobalProviderPrecedesProjectTrust():
		return []string{
			fmt.Sprintf(
				"%s MCP %s the global %s package; it is shared across projects and may read project MCP config before project trust",
				authoringProfile.Target(),
				action,
				label,
			),
		}
	default:
		return nil
	}
}
