package profile

import (
	"fmt"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
)

// MCPProviderAuthoringProfile carries target-owned facts required to author an
// explicit provider declaration without moving host specialization into the
// generic authoring workflow.
type MCPProviderAuthoringProfile struct {
	target                             target.Target
	carrier                            desiredextension.Carrier
	providerLabel                      string
	defaultSource                      string
	projectActivationRequiresTrust     bool
	globalProviderSharedAcrossProjects bool
	globalProviderPrecedesProjectTrust bool
}

func newMCPProviderAuthoringProfile(
	selectedTarget target.Target,
	carrier desiredextension.Carrier,
	providerLabel string,
	defaultSource string,
	projectActivationRequiresTrust bool,
	globalProviderSharedAcrossProjects bool,
	globalProviderPrecedesProjectTrust bool,
) (MCPProviderAuthoringProfile, error) {
	admittedTarget, ok := carrier.AdmittedTarget()
	if !ok || admittedTarget != selectedTarget {
		return MCPProviderAuthoringProfile{}, fmt.Errorf(
			"MCP provider authoring carrier %q does not admit target %q",
			carrier,
			selectedTarget,
		)
	}
	sourceKind, ok := carrier.RequiredSourceKind()
	if !ok {
		return MCPProviderAuthoringProfile{}, fmt.Errorf(
			"MCP provider authoring carrier %q has no source kind",
			carrier,
		)
	}
	if _, err := desiredextension.NewAuthoredSourceRef(sourceKind, defaultSource); err != nil {
		return MCPProviderAuthoringProfile{}, fmt.Errorf(
			"MCP provider authoring default source: %w",
			err,
		)
	}
	if providerLabel == "" {
		return MCPProviderAuthoringProfile{}, fmt.Errorf(
			"MCP provider authoring label is required",
		)
	}
	if globalProviderPrecedesProjectTrust && !globalProviderSharedAcrossProjects {
		return MCPProviderAuthoringProfile{}, fmt.Errorf(
			"MCP provider cannot precede project trust unless it is shared across projects",
		)
	}
	return MCPProviderAuthoringProfile{
		target:                             selectedTarget,
		carrier:                            carrier,
		providerLabel:                      providerLabel,
		defaultSource:                      defaultSource,
		projectActivationRequiresTrust:     projectActivationRequiresTrust,
		globalProviderSharedAcrossProjects: globalProviderSharedAcrossProjects,
		globalProviderPrecedesProjectTrust: globalProviderPrecedesProjectTrust,
	}, nil
}

func mustMCPProviderAuthoringProfile(
	selectedTarget target.Target,
	carrier desiredextension.Carrier,
	providerLabel string,
	defaultSource string,
	projectActivationRequiresTrust bool,
	globalProviderSharedAcrossProjects bool,
	globalProviderPrecedesProjectTrust bool,
) MCPProviderAuthoringProfile {
	result, err := newMCPProviderAuthoringProfile(
		selectedTarget,
		carrier,
		providerLabel,
		defaultSource,
		projectActivationRequiresTrust,
		globalProviderSharedAcrossProjects,
		globalProviderPrecedesProjectTrust,
	)
	if err != nil {
		panic(err)
	}
	return result
}

// MCPProviderAuthoringProfileForTarget returns the explicit provider authoring
// policy admitted by one target profile.
func MCPProviderAuthoringProfileForTarget(
	selectedTarget target.Target,
) (MCPProviderAuthoringProfile, bool) {
	switch selectedTarget {
	case target.TargetPi:
		return piMCPProviderAuthoringProfile, true
	default:
		return MCPProviderAuthoringProfile{}, false
	}
}

func (profile MCPProviderAuthoringProfile) Target() target.Target {
	return profile.target
}

func (profile MCPProviderAuthoringProfile) Carrier() desiredextension.Carrier {
	return profile.carrier
}

func (profile MCPProviderAuthoringProfile) ProviderLabel() string {
	return profile.providerLabel
}

func (profile MCPProviderAuthoringProfile) DefaultExtensionID(
	selectedScope target.Scope,
) (string, error) {
	scope, err := target.ParseScope(string(selectedScope))
	if err != nil {
		return "", err
	}
	return profile.providerLabel + "-" + string(scope), nil
}

func (profile MCPProviderAuthoringProfile) DefaultSource() string {
	return profile.defaultSource
}

func (profile MCPProviderAuthoringProfile) ProjectActivationRequiresTrust() bool {
	return profile.projectActivationRequiresTrust
}

func (profile MCPProviderAuthoringProfile) GlobalProviderSharedAcrossProjects() bool {
	return profile.globalProviderSharedAcrossProjects
}

func (profile MCPProviderAuthoringProfile) GlobalProviderPrecedesProjectTrust() bool {
	return profile.globalProviderPrecedesProjectTrust
}
