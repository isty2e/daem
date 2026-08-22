package codexplugin

import (
	"context"
	"path/filepath"
	"strings"

	observeconfig "github.com/isty2e/daem/internal/assurance/observe/config"
	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// observeIndependentPluginContributions reports source-declared Codex plugin
// contributions from bounded cache artifacts using a fresh observation budget.
func observeIndependentPluginContributions(
	ctx context.Context,
	homeDirectory string,
	config observeconfig.Observation,
) []observecontribution.SourceContributionObservation {
	return observeConfiguredPluginContributions(ctx, homeDirectory, config, &observationBudget{}, true)
}

func observeConfiguredPluginContributions(
	ctx context.Context,
	homeDirectory string,
	config observeconfig.Observation,
	budget *observationBudget,
	chargeProviderKeys bool,
) []observecontribution.SourceContributionObservation {
	if ctx == nil {
		ctx = context.Background()
	}
	homeDirectory = strings.TrimSpace(homeDirectory)
	if homeDirectory == "" {
		return nil
	}
	if budget == nil {
		budget = &observationBudget{}
	}
	if budget.remainingEntries() == 0 {
		return nil
	}

	entries := observedConfigEntries(config)
	observations := make([]observecontribution.SourceContributionObservation, 0, min(len(entries), MaximumObservationEntries))
	cacheRoot, layoutReason, layoutErr := openPluginCacheLayout(
		filepath.Join(homeDirectory, ".codex", "plugins", "cache"),
		budget,
	)
	if cacheRoot != nil {
		defer cacheRoot.close()
	}
	if observationCanceled(layoutErr) {
		return observations
	}
	for index, entry := range entries {
		if err := ctx.Err(); err != nil {
			return observations
		}
		provider := observecontribution.SourceProviderLabel(entry.Key())
		moreProviders := index < len(entries)-1
		if contributionInspectionBlockedByBudget(budget, moreProviders) {
			return finishWithContributionBudgetBlocker(
				ctx,
				cacheRoot,
				layoutReason,
				provider,
				budget,
				observations,
			)
		}
		if chargeProviderKeys {
			if budget.consumeNames([]string{string(entry.Key())}) {
				return finishWithContributionBudgetBlocker(
					ctx,
					cacheRoot,
					layoutReason,
					provider,
					budget,
					observations,
				)
			}
		}
		observation, err := observeConfiguredPluginContributionResult(
			ctx,
			cacheRoot,
			layoutReason,
			provider,
			budget,
		)
		if observationCanceled(err) {
			return observations
		}
		if !chargeProviderKeys &&
			!sourceObservationBudgetExceeded(observation) &&
			!sourceObservationHasContributionKeep(observation) {
			if budget.consumeKeep() {
				return finishWithContributionBudgetBlocker(
					ctx,
					cacheRoot,
					layoutReason,
					provider,
					budget,
					observations,
				)
			}
		}
		observations = append(observations, observation)
		if sourceObservationBudgetExceeded(observation) {
			return observations
		}
	}
	return observations
}

func contributionInspectionBlockedByBudget(budget *observationBudget, moreProviders bool) bool {
	if budget == nil {
		return false
	}
	if budget.exceeded || budget.remainingEntries() == 0 {
		return true
	}
	return moreProviders && budget.remainingEntries() == 1
}

func finishWithContributionBudgetBlocker(
	ctx context.Context,
	cacheRoot *pluginObservation,
	layoutReason observecontribution.SourceContributionReason,
	provider observecontribution.SourceProviderLabel,
	budget *observationBudget,
	observations []observecontribution.SourceContributionObservation,
) []observecontribution.SourceContributionObservation {
	if budget != nil {
		budget.exhaust()
	}
	observation, err := observeConfiguredPluginContributionResult(
		ctx,
		cacheRoot,
		layoutReason,
		provider,
		budget,
	)
	if observationCanceled(err) {
		return observations
	}
	return append(observations, observation)
}

func observedConfigEntries(config observeconfig.Observation) []observeconfig.Entry {
	entries := config.Entries()
	observed := make([]observeconfig.Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.Observed() {
			observed = append(observed, entry)
		}
	}
	return observed
}

func observeConfiguredPluginContributionResult(
	ctx context.Context,
	cacheRoot *pluginObservation,
	layoutReason observecontribution.SourceContributionReason,
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
	if layoutReason != observecontribution.SourceContributionReasonNone {
		if blocked, observation := contributionInspectionOutcome(
			provider,
			providerLabel,
			layoutReason,
			cacheArtifactIdentity(id, "<blocked>"),
		); blocked {
			return observation, nil
		}
	}

	plugin, version, ok, ambiguous, reason, err := activePluginCacheVersion(
		ctx,
		cacheRoot,
		id.marketplace,
		id.plugin,
	)
	if observationCanceled(err) {
		return observecontribution.SourceContributionObservation{}, err
	}
	if blocked, observation := contributionInspectionOutcome(provider, providerLabel, reason, cacheArtifactIdentity(id, "<blocked>")); blocked {
		return observation, nil
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
	defer plugin.close()

	artifactIdentity := cacheArtifactIdentity(id, version)
	content, exists, reason, err := plugin.snapshot(ctx, ".codex-plugin/plugin.json")
	if observationCanceled(err) {
		return observecontribution.SourceContributionObservation{}, err
	}
	if blocked, observation := contributionInspectionOutcome(provider, providerLabel, reason, artifactIdentity); blocked {
		return observation, nil
	}
	if !exists {
		return sourceContributionBlocker(
			provider,
			providerLabel,
			observecontribution.SourceContributionUnavailable,
			observecontribution.SourceContributionReasonArtifactUnavailable,
			artifactIdentity,
		), nil
	}

	manifest, reason := decodePluginContributionManifest(content, budget)
	if reason != observecontribution.SourceContributionReasonNone {
		return sourceContributionBlocker(
			provider,
			providerLabel,
			observecontribution.SourceContributionBlocked,
			reason,
			artifactIdentity,
		), nil
	}

	contributions, reason, err := sourceContributionsFromManifest(ctx, plugin, artifactIdentity, id.plugin, manifest)
	if observationCanceled(err) {
		return observecontribution.SourceContributionObservation{}, err
	}
	if err != nil {
		reason, err = classifyDirectoryError(err)
		if observationCanceled(err) {
			return observecontribution.SourceContributionObservation{}, err
		}
	}
	if blocked, observation := contributionInspectionOutcome(provider, providerLabel, reason, artifactIdentity); blocked {
		return observation, nil
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
		return sourceContributionBlocker(
			provider,
			providerLabel,
			observecontribution.SourceContributionBlocked,
			observecontribution.SourceContributionReasonUnsupportedShape,
			artifactIdentity,
		), nil
	}
	return observation, nil
}

func sourceObservationBudgetExceeded(observation observecontribution.SourceContributionObservation) bool {
	for _, row := range observation.DiagnosticRows() {
		if row.Reason() == observecontribution.SourceContributionReasonArtifactBudgetExceeded {
			return true
		}
	}
	return false
}

func sourceObservationHasContributionKeep(observation observecontribution.SourceContributionObservation) bool {
	for _, row := range observation.DiagnosticRows() {
		if row.HasContribution() {
			return true
		}
	}
	return false
}

func contributionInspectionOutcome(
	provider extensiontopology.Carrier,
	providerLabel observecontribution.SourceProviderLabel,
	reason observecontribution.SourceContributionReason,
	artifactIdentity string,
) (bool, observecontribution.SourceContributionObservation) {
	switch reason {
	case observecontribution.SourceContributionReasonNone:
		return false, observecontribution.SourceContributionObservation{}
	case observecontribution.SourceContributionReasonArtifactUnavailable:
		return true, sourceContributionBlocker(
			provider,
			providerLabel,
			observecontribution.SourceContributionUnavailable,
			reason,
			artifactIdentity,
		)
	default:
		return true, sourceContributionBlocker(
			provider,
			providerLabel,
			observecontribution.SourceContributionBlocked,
			reason,
			artifactIdentity,
		)
	}
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
