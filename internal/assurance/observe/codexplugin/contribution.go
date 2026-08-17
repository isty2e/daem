package codexplugin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	observeconfig "github.com/isty2e/daem/internal/assurance/observe/config"
	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// ObserveConfiguredPluginContributions reports source-declared Codex plugin contributions from bounded cache artifacts.
func ObserveConfiguredPluginContributions(
	ctx context.Context,
	homeDirectory string,
	config observeconfig.Observation,
) []observecontribution.SourceContributionObservation {
	if ctx == nil {
		ctx = context.Background()
	}
	homeDirectory = strings.TrimSpace(homeDirectory)
	if homeDirectory == "" {
		return nil
	}

	entries := config.Entries()
	observations := make([]observecontribution.SourceContributionObservation, 0, len(entries))
	budget := &observationBudget{}
	for _, entry := range entries {
		if !entry.Observed() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return observations
		}
		if budget.exceeded {
			observations = append(observations, observeConfiguredPluginContribution(
				ctx,
				homeDirectory,
				observecontribution.SourceProviderLabel(entry.Key()),
				budget,
			))
			continue
		}
		observation, err := observeConfiguredPluginContributionResult(
			ctx,
			homeDirectory,
			observecontribution.SourceProviderLabel(entry.Key()),
			budget,
		)
		if observationCanceled(err) {
			return observations
		}
		observations = append(observations, observation)
	}
	return observations
}

func observeConfiguredPluginContribution(
	ctx context.Context,
	homeDirectory string,
	rawProvider observecontribution.SourceProviderLabel,
	budget *observationBudget,
) observecontribution.SourceContributionObservation {
	observation, err := observeConfiguredPluginContributionResult(ctx, homeDirectory, rawProvider, budget)
	if observationCanceled(err) {
		return observecontribution.SourceContributionObservation{}
	}
	return observation
}

func observeConfiguredPluginContributionResult(
	ctx context.Context,
	homeDirectory string,
	rawProvider observecontribution.SourceProviderLabel,
	budget *observationBudget,
) (observecontribution.SourceContributionObservation, error) {
	id, ok := parsePluginID(rawProvider)
	if !ok {
		return sourceContributionBlocker(
			extensiontopology.Carrier{},
			safeProviderLabel(rawProvider),
			observecontribution.SourceContributionBlocked,
			observecontribution.SourceContributionReasonProviderProvenanceRequired,
			"",
		), nil
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
		), nil
	}
	if budget != nil && budget.exceeded {
		return sourceContributionBlocker(
			provider,
			providerLabel,
			observecontribution.SourceContributionBlocked,
			observecontribution.SourceContributionReasonArtifactBudgetExceeded,
			cacheArtifactIdentity(id, "<blocked>"),
		), nil
	}

	cacheBase := filepath.Join(homeDirectory, ".codex", "plugins", "cache", id.marketplace, id.plugin)
	version, ok, ambiguous, reason, err := activePluginCacheVersion(ctx, homeDirectory, cacheBase, budget)
	if observationCanceled(err) {
		return observecontribution.SourceContributionObservation{}, err
	}
	if reason != observecontribution.SourceContributionReasonNone {
		return sourceContributionBlocker(
			provider,
			providerLabel,
			observecontribution.SourceContributionBlocked,
			reason,
			cacheArtifactIdentity(id, "<blocked>"),
		), nil
	}
	if ambiguous {
		return sourceContributionBlocker(
			provider,
			providerLabel,
			observecontribution.SourceContributionAmbiguous,
			observecontribution.SourceContributionReasonArtifactAmbiguous,
			cacheArtifactIdentity(id, "<ambiguous>"),
		), nil
	}
	if !ok {
		return sourceContributionBlocker(
			provider,
			providerLabel,
			observecontribution.SourceContributionUnavailable,
			observecontribution.SourceContributionReasonArtifactUnavailable,
			cacheArtifactIdentity(id, "<missing>"),
		), nil
	}

	pluginRoot := filepath.Join(cacheBase, version)
	artifactIdentity := cacheArtifactIdentity(id, version)
	content, exists, reason, err := snapshotContainedFile(ctx, pluginRoot, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"))
	if observationCanceled(err) {
		return observecontribution.SourceContributionObservation{}, err
	}
	if reason != observecontribution.SourceContributionReasonNone {
		return sourceContributionBlocker(provider, providerLabel, observecontribution.SourceContributionBlocked, reason, artifactIdentity), nil
	}
	if !exists {
		return sourceContributionBlocker(provider, providerLabel, observecontribution.SourceContributionUnavailable, observecontribution.SourceContributionReasonArtifactUnavailable, artifactIdentity), nil
	}

	var manifest rawPluginContributionManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return sourceContributionBlocker(provider, providerLabel, observecontribution.SourceContributionBlocked, observecontribution.SourceContributionReasonArtifactMalformed, artifactIdentity), nil
	}

	contributions, reason, err := sourceContributionsFromManifest(ctx, pluginRoot, artifactIdentity, id.plugin, manifest, budget)
	if observationCanceled(err) {
		return observecontribution.SourceContributionObservation{}, err
	}
	if reason != observecontribution.SourceContributionReasonNone {
		return sourceContributionBlocker(provider, providerLabel, observecontribution.SourceContributionBlocked, reason, artifactIdentity), nil
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
		return sourceContributionBlocker(provider, providerLabel, observecontribution.SourceContributionBlocked, observecontribution.SourceContributionReasonUnsupportedShape, artifactIdentity), nil
	}
	return observation, nil
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
