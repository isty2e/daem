package codexplugin

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	observeconfig "github.com/isty2e/daem/internal/assurance/observe/config"
	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// ObserveConfiguredPluginContributions reports source-declared Codex plugin contributions from bounded cache artifacts.
func ObserveConfiguredPluginContributions(
	homeDirectory string,
	config observeconfig.Observation,
) []observecontribution.SourceContributionObservation {
	homeDirectory = strings.TrimSpace(homeDirectory)
	if homeDirectory == "" {
		return nil
	}

	entries := config.Entries()
	observations := make([]observecontribution.SourceContributionObservation, 0, len(entries))
	for _, entry := range entries {
		if !entry.Observed() {
			continue
		}
		observations = append(observations, observeConfiguredPluginContribution(
			homeDirectory,
			observecontribution.SourceProviderLabel(entry.Key()),
		))
	}
	return observations
}

func observeConfiguredPluginContribution(
	homeDirectory string,
	rawProvider observecontribution.SourceProviderLabel,
) observecontribution.SourceContributionObservation {
	id, ok := parsePluginID(rawProvider)
	if !ok {
		return sourceContributionBlocker(
			extensiontopology.Carrier{},
			safeProviderLabel(rawProvider),
			observecontribution.SourceContributionBlocked,
			observecontribution.SourceContributionReasonProviderProvenanceRequired,
			"",
		)
	}
	providerLabel := id.providerLabel()
	provider, err := id.carrier()
	if err != nil {
		return sourceContributionBlocker(
			extensiontopology.Carrier{},
			providerLabel,
			observecontribution.SourceContributionBlocked,
			observecontribution.SourceContributionReasonProviderProvenanceRequired,
			"",
		)
	}

	cacheBase := filepath.Join(homeDirectory, ".codex", "plugins", "cache", id.marketplace, id.plugin)
	version, ok, ambiguous, reason := activePluginCacheVersion(homeDirectory, cacheBase)
	if reason != observecontribution.SourceContributionReasonNone {
		return sourceContributionBlocker(
			provider,
			providerLabel,
			observecontribution.SourceContributionBlocked,
			reason,
			cacheArtifactIdentity(id, "<blocked>"),
		)
	}
	if ambiguous {
		return sourceContributionBlocker(
			provider,
			providerLabel,
			observecontribution.SourceContributionAmbiguous,
			observecontribution.SourceContributionReasonArtifactAmbiguous,
			cacheArtifactIdentity(id, "<ambiguous>"),
		)
	}
	if !ok {
		return sourceContributionBlocker(
			provider,
			providerLabel,
			observecontribution.SourceContributionUnavailable,
			observecontribution.SourceContributionReasonArtifactUnavailable,
			cacheArtifactIdentity(id, "<missing>"),
		)
	}

	pluginRoot := filepath.Join(cacheBase, version)
	artifactIdentity := cacheArtifactIdentity(id, version)
	content, err := readBoundedFile(pluginRoot, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"))
	if err != nil {
		if errors.Is(err, errPathBlocked) {
			return sourceContributionBlocker(provider, providerLabel, observecontribution.SourceContributionBlocked, observecontribution.SourceContributionReasonArtifactPathBlocked, artifactIdentity)
		}
		return sourceContributionBlocker(provider, providerLabel, observecontribution.SourceContributionUnavailable, observecontribution.SourceContributionReasonArtifactUnavailable, artifactIdentity)
	}

	var manifest rawPluginContributionManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return sourceContributionBlocker(provider, providerLabel, observecontribution.SourceContributionBlocked, observecontribution.SourceContributionReasonArtifactMalformed, artifactIdentity)
	}

	contributions, reason := sourceContributionsFromManifest(pluginRoot, artifactIdentity, id.plugin, manifest)
	if reason != observecontribution.SourceContributionReasonNone {
		return sourceContributionBlocker(provider, providerLabel, observecontribution.SourceContributionBlocked, reason, artifactIdentity)
	}
	sortSourceContributions(contributions)
	observation, err := observecontribution.NewSourceContributionObservation(observecontribution.SourceContributionObservationSpec{
		Provider:         provider,
		ProviderLabel:    providerLabel,
		State:            observecontribution.SourceContributionDeclared,
		Reason:           observecontribution.SourceContributionReasonNone,
		ArtifactIdentity: artifactIdentity,
		Contributions:    contributions,
	})
	if err != nil {
		return sourceContributionBlocker(provider, providerLabel, observecontribution.SourceContributionBlocked, observecontribution.SourceContributionReasonUnsupportedShape, artifactIdentity)
	}
	return observation
}

func sourceContributionBlocker(
	provider extensiontopology.Carrier,
	providerLabel observecontribution.SourceProviderLabel,
	state observecontribution.SourceContributionState,
	reason observecontribution.SourceContributionReason,
	artifactIdentity string,
) observecontribution.SourceContributionObservation {
	observation, err := observecontribution.NewSourceContributionObservation(observecontribution.SourceContributionObservationSpec{
		Provider:         provider,
		ProviderLabel:    providerLabel,
		State:            state,
		Reason:           reason,
		ArtifactIdentity: artifactIdentity,
	})
	if err == nil {
		return observation
	}
	fallbackReason := observecontribution.SourceContributionReasonUnsupportedShape
	if provider.SubjectID().IsZero() {
		fallbackReason = observecontribution.SourceContributionReasonProviderProvenanceRequired
	}
	fallback, fallbackErr := observecontribution.NewSourceContributionObservation(observecontribution.SourceContributionObservationSpec{
		Provider:      provider,
		ProviderLabel: safeProviderLabel(providerLabel),
		State:         observecontribution.SourceContributionBlocked,
		Reason:        fallbackReason,
	})
	if fallbackErr != nil {
		panic(fallbackErr)
	}
	return fallback
}

func safeProviderLabel(provider observecontribution.SourceProviderLabel) observecontribution.SourceProviderLabel {
	if observecontribution.ValidSourceToken(string(provider)) {
		return provider
	}
	return observecontribution.SourceProviderLabel("<invalid-provider>")
}
