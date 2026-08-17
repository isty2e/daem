package codexplugin

import (
	"context"
	"strings"

	observeconfig "github.com/isty2e/daem/internal/assurance/observe/config"
	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
)

// ObserveConfiguredPluginDiagnostics reports one Codex plugin diagnostic
// observation. Config keys and contribution source inspection share one budget.
func ObserveConfiguredPluginDiagnostics(
	ctx context.Context,
	homeDirectory string,
	configPath string,
) (observeconfig.Observation, []observecontribution.SourceContributionObservation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	budget := &observationBudget{}
	observation, err := observeConfigFile(ctx, configPath, budget)
	if err != nil || !observation.ConfigExists() || !observation.EntrySetObserved() {
		return observation, nil, err
	}
	if budget.remainingEntries() == 0 {
		return observation, nil, nil
	}
	return observation, observeConfiguredPluginContributions(
		ctx,
		strings.TrimSpace(homeDirectory),
		observation,
		budget,
		false,
	), nil
}
