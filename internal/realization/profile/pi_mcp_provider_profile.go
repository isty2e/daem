package profile

import (
	"fmt"
	"strings"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
	"golang.org/x/mod/semver"
)

const (
	piMCPProviderPackageName      = "pi-mcp-adapter"
	piMCPProviderContributionKind = "mcp-client"
	piMCPProviderContributionKey  = "default"
	piMCPProviderDefaultSource    = "npm:pi-mcp-adapter@^2.13.0"
	piMCPProviderVersionFloor     = "v2.13.0"
	piMCPProviderVersionCeiling   = "v3.0.0"
)

var piMCPProviderAuthoringProfile = mustMCPProviderAuthoringProfile(
	target.TargetPi,
	desiredextension.CarrierPiPackage,
	piMCPProviderPackageName,
	piMCPProviderDefaultSource,
	true,
	true,
	true,
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
	if !carrier.Source().CredentialFree() || !carrier.Source().ControlFree() {
		return extensiontopology.Contribution{}, false, fmt.Errorf(
			"Pi MCP provider source is not authorized for provider execution",
		)
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
	if err := validatePiMCPProviderSelector(carrier.Source().Ref()); err != nil {
		return extensiontopology.Contribution{}, false, err
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

// MCPProviderCodecForCurrentVersion maps one freshly observed exact provider
// version to its admitted MCP codec. It does not infer installation, relation,
// source, or runtime readiness.
func MCPProviderCodecForCurrentVersion(
	selectedTarget target.Target,
	carrier extensiontopology.Carrier,
	reference extensiontopology.ContributionReference,
	version string,
) (aggregate.CodecContractID, error) {
	if err := carrier.Validate(); err != nil {
		return "", fmt.Errorf("MCP provider version carrier: %w", err)
	}
	if err := reference.Validate(); err != nil {
		return "", fmt.Errorf("MCP provider version contribution: %w", err)
	}
	if reference.ProviderSubjectID() != carrier.SubjectID() {
		return "", fmt.Errorf("MCP provider contribution does not belong to the observed carrier")
	}
	switch selectedTarget {
	case target.TargetPi:
		contribution, admitted, err := PiMCPProviderContribution(carrier)
		if err != nil {
			return "", err
		}
		if !admitted || !contribution.Reference().Equal(reference) {
			return "", fmt.Errorf("current package is not the selected Pi MCP provider")
		}
		if err := validatePiMCPProviderCurrentVersion(version); err != nil {
			return "", err
		}
		return aggregate.MCPCodecPiAdapterStdio, nil
	default:
		return "", fmt.Errorf(
			"target %q has no provider-mediated MCP version profile",
			selectedTarget,
		)
	}
}

func validatePiMCPProviderSelector(sourceRef string) error {
	const prefix = "npm:" + piMCPProviderPackageName + "@"
	if !strings.HasPrefix(sourceRef, prefix) {
		return fmt.Errorf(
			"Pi MCP provider source %q requires an exact or caret-bounded version selector",
			sourceRef,
		)
	}
	selector := strings.TrimPrefix(sourceRef, prefix)
	version := selector
	if strings.HasPrefix(selector, "^") {
		version = strings.TrimPrefix(selector, "^")
	}
	canonical, err := canonicalStableNPMVersion(version)
	if err != nil {
		return fmt.Errorf("Pi MCP provider selector %q: %w", selector, err)
	}
	if semver.Compare(canonical, piMCPProviderVersionFloor) < 0 ||
		semver.Compare(canonical, piMCPProviderVersionCeiling) >= 0 {
		return fmt.Errorf(
			"Pi MCP provider selector %q falls outside stable >=2.13.0 and <3.0.0",
			selector,
		)
	}
	if strings.HasPrefix(selector, "^") && semver.Major(canonical) != "v2" {
		return fmt.Errorf(
			"Pi MCP provider selector %q may cross an unsupported profile boundary",
			selector,
		)
	}
	return nil
}

func validatePiMCPProviderCurrentVersion(version string) error {
	canonical, err := canonicalStableNPMVersion(version)
	if err != nil {
		return fmt.Errorf("Pi MCP provider current version: %w", err)
	}
	if semver.Compare(canonical, piMCPProviderVersionFloor) < 0 ||
		semver.Compare(canonical, piMCPProviderVersionCeiling) >= 0 {
		return fmt.Errorf(
			"Pi MCP provider current version %q is outside stable >=2.13.0 and <3.0.0",
			version,
		)
	}
	return nil
}

func canonicalStableNPMVersion(version string) (string, error) {
	if version == "" || strings.TrimSpace(version) != version || strings.HasPrefix(version, "v") {
		return "", fmt.Errorf("version %q must be a canonical unprefixed semantic version", version)
	}
	canonical := "v" + version
	if !semver.IsValid(canonical) ||
		semver.Canonical(canonical) != canonical ||
		semver.Prerelease(canonical) != "" ||
		semver.Build(canonical) != "" {
		return "", fmt.Errorf("version %q must be a canonical stable semantic version", version)
	}
	return canonical, nil
}
