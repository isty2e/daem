package diagnose

import (
	"context"
	"fmt"
	"path/filepath"

	observecodexplugin "github.com/isty2e/daem/internal/assurance/observe/codexplugin"
	observeconfig "github.com/isty2e/daem/internal/assurance/observe/config"
	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
	"github.com/isty2e/daem/internal/findings"
	targetpkg "github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

// CodexPluginChecks reports doctor-only static Codex plugin config and contribution source observations.
func CodexPluginChecks(ctx context.Context, homeDirectory string, selection targetselection.Selection) []findings.Check {
	if !selection.Includes(targetpkg.TargetCodex) {
		return nil
	}

	configPath := filepath.Join(homeDirectory, ".codex", "config.toml")
	observation, err := observecodexplugin.ObserveConfigFile(configPath)
	checks := codexPluginConfigChecks(configPath, observation, err)
	if err != nil || !observation.ConfigExists() || !observation.EntrySetObserved() {
		return checks
	}
	contributions := observecodexplugin.ObserveConfiguredPluginContributions(ctx, homeDirectory, observation)
	checks = append(checks, codexPluginContributionChecks(contributions)...)
	return checks
}

func codexPluginConfigChecks(
	configPath string,
	observation observeconfig.Observation,
	err error,
) []findings.Check {
	if err != nil {
		return []findings.Check{warnCheck(
			"target=codex plugin_config",
			fmt.Sprintf("observe-only Codex plugin config entries blocked: read or parse %s: %v; no daem ownership, lock, install, readiness, or mutation authority", configPath, err),
		)}
	}
	if !observation.ConfigExists() {
		return []findings.Check{warnCheck(
			"target=codex plugin_config",
			fmt.Sprintf("observe-only Codex plugin config entries unavailable: %s is missing", configPath),
		)}
	}
	if observation.EntrySetUnsupported() {
		return []findings.Check{warnCheck(
			"target=codex plugin_config",
			"observe-only Codex plugin config entries blocked: plugins table has unsupported shape; no daem ownership, lock, install, readiness, or mutation authority",
		)}
	}
	entries := observation.Entries()
	if len(entries) == 0 {
		return []findings.Check{okCheck(
			"target=codex plugin_config",
			"observe-only Codex plugin config entries absent in user config; no daem ownership or mutation authority",
		)}
	}

	checks := make([]findings.Check, 0, len(entries))
	for _, entry := range entries {
		checks = append(checks, codexPluginConfigEntryCheck(entry))
	}
	return checks
}

func codexPluginContributionChecks(contributions []observecontribution.SourceContributionObservation) []findings.Check {
	checks := make([]findings.Check, 0, len(contributions))
	for _, contribution := range contributions {
		for _, row := range contribution.DiagnosticRows() {
			checks = append(checks, codexPluginContributionCheck(row))
		}
	}
	return checks
}

func codexPluginConfigEntryCheck(entry observeconfig.Entry) findings.Check {
	name := fmt.Sprintf("target=codex plugin_config_entry key=%q", string(entry.Key()))
	detail := fmt.Sprintf(
		"observe-only Codex plugin config entry from user config; activation %s; no daem ownership, lock, install, readiness, or mutation authority",
		activationDisclosureText(entry.Activation()),
	)
	if entry.Unsupported() {
		if entry.Reason() != observeconfig.ReasonNone {
			detail = fmt.Sprintf(
				"observe-only Codex plugin config entry from user config; unsupported schema reason=%s; no daem ownership, lock, install, readiness, or mutation authority",
				entry.Reason(),
			)
		}
		return warnCheck(name, detail)
	}
	return okCheck(name, detail)
}

func codexPluginContributionCheck(row observecontribution.SourceContributionDiagnosticRow) findings.Check {
	name := codexPluginContributionCheckName(row)
	detail := codexPluginContributionDetail(row)
	if row.State() == observecontribution.SourceContributionDeclared {
		if _, ok := row.ProviderSubject(); !ok {
			return warnCheck(name, detail+"; canonical provider correlation unavailable")
		}
		if row.HasContribution() {
			if _, ok := row.ContributionSubject(); !ok {
				return warnCheck(name, detail+"; canonical contribution correlation unavailable")
			}
		}
		return okCheck(name, detail)
	}
	return warnCheck(name, detail)
}

func codexPluginContributionCheckName(row observecontribution.SourceContributionDiagnosticRow) string {
	if !row.HasContribution() {
		return fmt.Sprintf("target=codex plugin_contribution provided_by=%q", string(row.ProvidedBy()))
	}
	return fmt.Sprintf(
		"target=codex plugin_contribution provided_by=%q kind=%s key=%q",
		string(row.ProvidedBy()),
		row.Kind(),
		row.Key(),
	)
}

func codexPluginContributionDetail(row observecontribution.SourceContributionDiagnosticRow) string {
	common := fmt.Sprintf(
		"provided_by=%q provenance=%s current=%s freshness=%s artifact=%q state=%s",
		string(row.ProvidedBy()),
		row.Provenance(),
		row.Currentness(),
		row.Freshness(),
		row.ArtifactIdentity(),
		row.State(),
	)
	authority := "no current inventory; no daem ownership, lock, install, readiness, mutation, disablement, uninstall, prune, or apply skip authority"
	if !row.HasContribution() {
		if row.Reason() == observecontribution.SourceContributionReasonNone {
			return fmt.Sprintf("source-declared Codex plugin contributions from source_artifact_inspection; %s contributions=none; %s", common, authority)
		}
		return fmt.Sprintf(
			"Codex plugin contribution source diagnostic from source_artifact_inspection; %s reason=%s; %s",
			common,
			row.Reason(),
			authority,
		)
	}
	return fmt.Sprintf(
		"source-declared Codex plugin contribution from source_artifact_inspection; %s kind=%s key=%q source_marker=%q; %s",
		common,
		row.Kind(),
		row.Key(),
		row.SourceMarker(),
		authority,
	)
}

func activationDisclosureText(disclosure observeconfig.ActivationDisclosure) string {
	switch disclosure {
	case observeconfig.ActivationConfiguredTrue:
		return "configured true"
	case observeconfig.ActivationConfiguredFalse:
		return "configured false"
	case observeconfig.ActivationUnsupportedType:
		return "unsupported schema"
	default:
		return "not declared"
	}
}
