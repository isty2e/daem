package profile

import (
	"fmt"
	"strings"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

const (
	piMCPProviderPackageName      = "pi-mcp-adapter"
	piMCPProviderContributionKind = "mcp-client"
	piMCPProviderContributionKey  = "default"
)

// MCPProviderContributionForTarget returns the provider contribution admitted
// by one target's static MCP profile. The profile owns host specialization so
// lock collection validation remains target-agnostic.
func MCPProviderContributionForTarget(
	selectedTarget target.Target,
	carrier extensiontopology.Carrier,
) (extensiontopology.Contribution, bool, error) {
	switch selectedTarget {
	case target.TargetPi:
		return PiMCPProviderContribution(carrier)
	default:
		return extensiontopology.Contribution{}, false, nil
	}
}

// PiMCPProviderContribution returns the provider contribution exposed by one
// explicitly declared Pi package carrier when its structural source identity
// matches the admitted provider. Package selector and current-version checks
// remain separate route and observation contracts.
func PiMCPProviderContribution(
	carrier extensiontopology.Carrier,
) (extensiontopology.Contribution, bool, error) {
	if err := carrier.Validate(); err != nil {
		return extensiontopology.Contribution{}, false, fmt.Errorf("Pi MCP provider carrier: %w", err)
	}
	if carrier.Family() != desiredextension.CarrierPiPackage {
		return extensiontopology.Contribution{}, false, nil
	}
	source, err := extensiontopology.InterpretCarrierSource(carrier.Key())
	if err != nil {
		return extensiontopology.Contribution{}, false, err
	}
	if source.Class() != extensiontopology.CarrierSourceNPM {
		if strings.Contains(strings.ToLower(carrier.Source().Ref()), piMCPProviderPackageName) {
			return extensiontopology.Contribution{}, false, fmt.Errorf(
				"Pi MCP provider %q requires npm source identity %q",
				carrier.Source().Ref(),
				piMCPProviderPackageName,
			)
		}
		return extensiontopology.Contribution{}, false, nil
	}
	if source.Identity() != piMCPProviderPackageName {
		return extensiontopology.Contribution{}, false, nil
	}
	contribution, err := extensiontopology.NewContribution(
		carrier,
		extensiontopology.ContributionSpec{
			Kind: piMCPProviderContributionKind,
			Key:  piMCPProviderContributionKey,
		},
	)
	if err != nil {
		return extensiontopology.Contribution{}, false, err
	}
	return contribution, true, nil
}
