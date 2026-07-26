package diagnose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	observeconfig "github.com/isty2e/daem/internal/assurance/observe/config"
	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/findings"
	targetpkg "github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestCodexPluginChecksRequireSelectedCodexTarget(t *testing.T) {
	homeDirectory := t.TempDir()
	writeDiagnoseCodexConfig(t, homeDirectory, `
	[plugins."alpha@market"]
	enabled = true
	`)
	selection, err := targetselection.ForDiagnostics([]string{"opencode"})
	if err != nil {
		t.Fatalf("ForDiagnostics returned error: %v", err)
	}

	checks := CodexPluginChecks(homeDirectory, selection)
	if len(checks) != 0 {
		t.Fatalf("full checks = %#v, want none for non-Codex target", checks)
	}
}

func TestCodexPluginChecksReportObserveOnlyEntries(t *testing.T) {
	homeDirectory := t.TempDir()
	writeDiagnoseCodexConfig(t, homeDirectory, `
[plugins."alpha@market"]
enabled = true

[plugins."beta@market"]
enabled = false

[plugins."gamma@market"]
`)
	selection, err := targetselection.ForDiagnostics([]string{"codex"})
	if err != nil {
		t.Fatalf("ForDiagnostics returned error: %v", err)
	}

	checks := CodexPluginChecks(homeDirectory, selection)
	assertCodexPluginConfigCheck(t, checks, findings.SeverityOK, `target=codex plugin_config_entry key="alpha@market"`, "activation configured true")
	assertCodexPluginConfigCheck(t, checks, findings.SeverityOK, `target=codex plugin_config_entry key="beta@market"`, "activation configured false")
	assertCodexPluginConfigCheck(t, checks, findings.SeverityOK, `target=codex plugin_config_entry key="gamma@market"`, "activation not declared")
	for _, check := range checks {
		for _, forbidden := range []string{"lock subject", "state subject", "ready", "converged", "managed"} {
			if strings.Contains(check.Detail, forbidden) {
				t.Fatalf("check detail %q contains forbidden ownership/readiness wording %q", check.Detail, forbidden)
			}
		}
	}
}

func TestCodexPluginChecksReportUnsupportedActivationAsWarning(t *testing.T) {
	homeDirectory := t.TempDir()
	writeDiagnoseCodexConfig(t, homeDirectory, `
[plugins."alpha@market"]
enabled = "yes"
`)
	selection, err := targetselection.ForDiagnostics([]string{"codex"})
	if err != nil {
		t.Fatalf("ForDiagnostics returned error: %v", err)
	}

	checks := CodexPluginChecks(homeDirectory, selection)
	assertCodexPluginConfigCheck(t, checks, findings.SeverityWarn, `target=codex plugin_config_entry key="alpha@market"`, "unsupported schema reason=activation_not_boolean")
}

func TestCodexPluginChecksReportMissingAndMalformedConfig(t *testing.T) {
	selection, err := targetselection.ForDiagnostics([]string{"codex"})
	if err != nil {
		t.Fatalf("ForDiagnostics returned error: %v", err)
	}
	missingChecks := CodexPluginChecks(t.TempDir(), selection)
	assertCodexPluginConfigCheck(t, missingChecks, findings.SeverityWarn, "target=codex plugin_config", "unavailable")

	homeDirectory := t.TempDir()
	writeDiagnoseCodexConfig(t, homeDirectory, "[plugins.\"alpha@market\"\n")
	malformedChecks := CodexPluginChecks(homeDirectory, selection)
	assertCodexPluginConfigCheck(t, malformedChecks, findings.SeverityWarn, "target=codex plugin_config", "blocked: read or parse")
}

func TestCodexPluginConfigChecksReportPermissionDeniedAsBlockedObservation(t *testing.T) {
	observation, err := observeconfig.NewObservation(observeconfig.ObservationSpec{
		SourcePath:    "/tmp/codex/config.toml",
		EntrySetState: observeconfig.EntrySetNotDeclared,
	})
	if err != nil {
		t.Fatalf("NewObservation returned error: %v", err)
	}

	checks := codexPluginConfigChecks("/tmp/codex/config.toml", observation, os.ErrPermission)
	assertCodexPluginConfigCheck(t, checks, findings.SeverityWarn, "target=codex plugin_config", "blocked: read or parse")
	assertCodexPluginConfigCheck(t, checks, findings.SeverityWarn, "target=codex plugin_config", "no daem ownership, lock, install, readiness, or mutation authority")
}

func TestCodexPluginChecksReportSourceDeclaredContributionsWithoutPayloads(t *testing.T) {
	homeDirectory := t.TempDir()
	writeDiagnoseCodexConfig(t, homeDirectory, `
[plugins."alpha@market"]
enabled = true
`)
	pluginRoot := filepath.Join(homeDirectory, ".codex", "plugins", "cache", "market", "alpha", "local")
	writeDiagnoseFile(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), `{
  "name": "alpha",
  "skills": "./skills/",
  "mcpServers": {
    "context7": {
      "command": "node",
      "env": {"SECRET_TOKEN": "must-not-leak"}
    }
  },
  "apps": "./.app.json"
}`)
	writeDiagnoseFile(t, filepath.Join(pluginRoot, "skills", "review", "SKILL.md"), "---\nname: review\n---\n")
	writeDiagnoseFile(t, filepath.Join(pluginRoot, ".app.json"), `{"secret": "must-not-leak"}`)
	selection, err := targetselection.ForDiagnostics([]string{"codex"})
	if err != nil {
		t.Fatalf("ForDiagnostics returned error: %v", err)
	}

	checks := CodexPluginChecks(homeDirectory, selection)
	assertCodexPluginConfigCheck(
		t,
		checks,
		findings.SeverityOK,
		`target=codex plugin_contribution provided_by="alpha@market" kind=mcp-server key="context7"`,
		`source-declared Codex plugin contribution from source_artifact_inspection`,
	)
	contributionCheck := findCodexPluginCheck(t, checks, findings.SeverityOK, `target=codex plugin_contribution provided_by="alpha@market" kind=mcp-server key="context7"`)
	for _, want := range []string{
		`provided_by="alpha@market"`,
		`provenance=source_artifact_inspection`,
		`current=non-current`,
		`freshness=fresh`,
		`artifact="plugins/cache/market/alpha/local"`,
		`kind=mcp-server`,
		`key="context7"`,
		`source_marker="mcpServers"`,
		"no current inventory",
		"no daem ownership",
		"no current inventory; no daem ownership, lock, install, readiness, mutation, disablement, uninstall, prune, or apply skip authority",
	} {
		if !strings.Contains(contributionCheck.Detail, want) {
			t.Fatalf("contribution detail = %q, want %q", contributionCheck.Detail, want)
		}
	}
	assertCodexPluginConfigCheck(t, checks, findings.SeverityOK, `target=codex plugin_contribution provided_by="alpha@market" kind=app key="alpha"`, `source_marker=".app.json"`)
	assertCodexPluginConfigCheck(t, checks, findings.SeverityOK, `target=codex plugin_contribution provided_by="alpha@market" kind=skill key="review"`, `source_marker="skills/review/SKILL.md"`)
	for _, forbidden := range []string{"SECRET_TOKEN", "must-not-leak", "command", "env", "lock subject", "state subject"} {
		for _, check := range checks {
			if strings.Contains(check.Name, "plugin_contribution") && strings.Contains(check.Detail, forbidden) {
				t.Fatalf("contribution detail leaked forbidden payload %q: %q", forbidden, check.Detail)
			}
		}
	}
}

func TestCodexPluginChecksReportContributionSourceBlockers(t *testing.T) {
	homeDirectory := t.TempDir()
	writeDiagnoseCodexConfig(t, homeDirectory, `
[plugins."missing@market"]
enabled = true

[plugins."bad@market"]
enabled = true
`)
	badRoot := filepath.Join(homeDirectory, ".codex", "plugins", "cache", "market", "bad", "local")
	writeDiagnoseFile(t, filepath.Join(badRoot, ".codex-plugin", "plugin.json"), `{
  "skills": "../outside"
}`)
	selection, err := targetselection.ForDiagnostics([]string{"codex"})
	if err != nil {
		t.Fatalf("ForDiagnostics returned error: %v", err)
	}

	checks := CodexPluginChecks(homeDirectory, selection)
	assertCodexPluginConfigCheck(t, checks, findings.SeverityWarn, `target=codex plugin_contribution provided_by="missing@market"`, "state=source-artifact-unavailable")
	assertCodexPluginConfigCheck(t, checks, findings.SeverityWarn, `target=codex plugin_contribution provided_by="missing@market"`, "reason=SOURCE_ARTIFACT_UNAVAILABLE")
	assertCodexPluginConfigCheck(t, checks, findings.SeverityWarn, `target=codex plugin_contribution provided_by="bad@market"`, "state=source-artifact-blocked")
	assertCodexPluginConfigCheck(t, checks, findings.SeverityWarn, `target=codex plugin_contribution provided_by="bad@market"`, "reason=SOURCE_ARTIFACT_PATH_BLOCKED")
}

func TestCodexPluginContributionChecksRenderGenericFacts(t *testing.T) {
	declaredContributions := []observecontribution.SourceContribution{
		mustDiagnoseSourceContribution(t, observecontribution.SourceContributionSkill, "search", "skills/search/SKILL.md"),
		mustDiagnoseSourceContribution(t, observecontribution.SourceContributionMCPServer, "context7", "mcpServers"),
		mustDiagnoseSourceContribution(t, observecontribution.SourceContributionSkill, "review", "skills/review/SKILL.md"),
		mustDiagnoseSourceContribution(t, observecontribution.SourceContributionApp, "alpha", "apps/alpha.json"),
	}
	declared, err := observecontribution.NewSourceContributionObservation(observecontribution.SourceContributionObservationSpec{
		Provider:         mustDiagnoseCodexProvider(t, "alpha@market"),
		ProviderLabel:    "alpha@market",
		State:            observecontribution.SourceContributionDeclared,
		Reason:           observecontribution.SourceContributionReasonNone,
		ArtifactIdentity: "plugins/cache/market/alpha/local",
		Contributions:    declaredContributions,
	})
	if err != nil {
		t.Fatalf("NewSourceContributionObservation declared: %v", err)
	}
	empty, err := observecontribution.NewSourceContributionObservation(observecontribution.SourceContributionObservationSpec{
		Provider:         mustDiagnoseCodexProvider(t, "empty@market"),
		ProviderLabel:    "empty@market",
		State:            observecontribution.SourceContributionDeclared,
		Reason:           observecontribution.SourceContributionReasonNone,
		ArtifactIdentity: "plugins/cache/market/empty/local",
	})
	if err != nil {
		t.Fatalf("NewSourceContributionObservation declared: %v", err)
	}
	blocked, err := observecontribution.NewSourceContributionObservation(observecontribution.SourceContributionObservationSpec{
		Provider:         mustDiagnoseCodexProvider(t, "blocked@market"),
		ProviderLabel:    "blocked@market",
		State:            observecontribution.SourceContributionBlocked,
		Reason:           observecontribution.SourceContributionReasonUnsupportedShape,
		ArtifactIdentity: "plugins/cache/market/blocked/local",
	})
	if err != nil {
		t.Fatalf("NewSourceContributionObservation blocked: %v", err)
	}

	checks := codexPluginContributionChecks([]observecontribution.SourceContributionObservation{declared, empty, blocked})
	assertCodexPluginConfigCheck(t, checks, findings.SeverityOK, `target=codex plugin_contribution provided_by="alpha@market" kind=skill key="search"`, `source_marker="skills/search/SKILL.md"`)
	assertCodexPluginConfigCheck(t, checks, findings.SeverityOK, `target=codex plugin_contribution provided_by="alpha@market" kind=mcp-server key="context7"`, `source_marker="mcpServers"`)
	assertCodexPluginConfigCheck(t, checks, findings.SeverityOK, `target=codex plugin_contribution provided_by="alpha@market" kind=skill key="review"`, `source_marker="skills/review/SKILL.md"`)
	assertCodexPluginConfigCheck(t, checks, findings.SeverityOK, `target=codex plugin_contribution provided_by="alpha@market" kind=app key="alpha"`, `source_marker="apps/alpha.json"`)
	assertCodexPluginConfigCheck(t, checks, findings.SeverityOK, `target=codex plugin_contribution provided_by="empty@market"`, "contributions=none")
	assertCodexPluginConfigCheck(t, checks, findings.SeverityWarn, `target=codex plugin_contribution provided_by="blocked@market"`, "reason=UNSUPPORTED_CONTRIBUTION_SHAPE")
	for _, check := range checks {
		for _, forbidden := range []string{"ready", "converged", "managed", "lock subject", "state subject", "installability"} {
			if strings.Contains(check.Detail, forbidden) {
				t.Fatalf("generic-fact diagnostic detail %q contains forbidden wording %q", check.Detail, forbidden)
			}
		}
	}
}

func TestCodexPluginContributionChecksKeepDuplicateVisibleKeysProviderScoped(t *testing.T) {
	alpha, err := observecontribution.NewSourceContributionObservation(observecontribution.SourceContributionObservationSpec{
		Provider:         mustDiagnoseCodexProvider(t, "alpha@market"),
		ProviderLabel:    "alpha@market",
		State:            observecontribution.SourceContributionDeclared,
		Reason:           observecontribution.SourceContributionReasonNone,
		ArtifactIdentity: "plugins/cache/market/alpha/local",
		Contributions: []observecontribution.SourceContribution{
			mustDiagnoseSourceContribution(t, observecontribution.SourceContributionSkill, "review", "skills/review/SKILL.md"),
		},
	})
	if err != nil {
		t.Fatalf("NewSourceContributionObservation alpha: %v", err)
	}
	beta, err := observecontribution.NewSourceContributionObservation(observecontribution.SourceContributionObservationSpec{
		Provider:         mustDiagnoseCodexProvider(t, "beta@market"),
		ProviderLabel:    "beta@market",
		State:            observecontribution.SourceContributionDeclared,
		Reason:           observecontribution.SourceContributionReasonNone,
		ArtifactIdentity: "plugins/cache/market/beta/local",
		Contributions: []observecontribution.SourceContribution{
			mustDiagnoseSourceContribution(t, observecontribution.SourceContributionSkill, "review", "skills/review/SKILL.md"),
		},
	})
	if err != nil {
		t.Fatalf("NewSourceContributionObservation beta: %v", err)
	}

	checks := codexPluginContributionChecks([]observecontribution.SourceContributionObservation{alpha, beta})
	alphaCheck := findCodexPluginCheck(t, checks, findings.SeverityOK, `target=codex plugin_contribution provided_by="alpha@market" kind=skill key="review"`)
	betaCheck := findCodexPluginCheck(t, checks, findings.SeverityOK, `target=codex plugin_contribution provided_by="beta@market" kind=skill key="review"`)
	if alphaCheck.Detail == betaCheck.Detail ||
		!strings.Contains(alphaCheck.Detail, `provided_by="alpha@market"`) ||
		!strings.Contains(betaCheck.Detail, `provided_by="beta@market"`) {
		t.Fatalf("duplicate visible key checks merged provider scope: alpha=%q beta=%q", alphaCheck.Detail, betaCheck.Detail)
	}
}

func mustDiagnoseSourceContribution(
	t *testing.T,
	kind observecontribution.SourceContributionKind,
	key string,
	sourceMarker string,
) observecontribution.SourceContribution {
	t.Helper()
	contribution, err := observecontribution.NewSourceContribution(observecontribution.SourceContributionSpec{
		Kind:         kind,
		Key:          key,
		SourceMarker: sourceMarker,
	})
	if err != nil {
		t.Fatalf("NewSourceContribution: %v", err)
	}
	return contribution
}

func mustDiagnoseCodexProvider(t *testing.T, label string) extensiontopology.Carrier {
	t.Helper()
	source, err := desiredextension.NewSourceRef(desiredextension.SourceKindMarketplace, label)
	if err != nil {
		t.Fatalf("NewSourceRef(%q): %v", label, err)
	}
	key, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierCodexPlugin,
		targetpkg.TargetCodex,
		targetpkg.ScopeGlobal,
		source,
	)
	if err != nil {
		t.Fatalf("NewCarrierKey(%q): %v", label, err)
	}
	provider, err := extensiontopology.NewCarrier(key)
	if err != nil {
		t.Fatalf("NewCarrier(%q): %v", label, err)
	}
	return provider
}

func TestCodexPluginChecksDoNotInspectContributionsForUnsupportedConfig(t *testing.T) {
	selection, err := targetselection.ForDiagnostics([]string{"codex"})
	if err != nil {
		t.Fatalf("ForDiagnostics returned error: %v", err)
	}
	cases := []struct {
		name   string
		config string
	}{
		{
			name:   "plugins table unsupported",
			config: `plugins = "not a table"`,
		},
		{
			name: "plugin entry unsupported",
			config: `
[plugins."alpha@market"]
enabled = "yes"
`,
		},
		{
			name: "plugins absent",
			config: `
[marketplaces.private]
source = "https://token@example.invalid/repo.git"
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			homeDirectory := t.TempDir()
			writeDiagnoseCodexConfig(t, homeDirectory, tc.config)
			pluginRoot := filepath.Join(homeDirectory, ".codex", "plugins", "cache", "market", "alpha", "local")
			writeDiagnoseFile(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), `{
  "skills": "./skills/"
}`)
			writeDiagnoseFile(t, filepath.Join(pluginRoot, "skills", "review", "SKILL.md"), "---\nname: review\n---\n")

			checks := CodexPluginChecks(homeDirectory, selection)
			assertNoCodexPluginContributionChecks(t, checks)
		})
	}
}

func assertCodexPluginConfigCheck(t *testing.T, checks []findings.Check, severity findings.Severity, name string, detailSubstring string) {
	t.Helper()

	check := findCodexPluginCheck(t, checks, severity, name)
	if strings.Contains(check.Detail, detailSubstring) {
		return
	}
	t.Fatalf("checks = %#v, want %s %s containing %q", checks, severity, name, detailSubstring)
}

func assertNoCodexPluginContributionChecks(t *testing.T, checks []findings.Check) {
	t.Helper()
	for _, check := range checks {
		if strings.Contains(check.Name, "plugin_contribution") {
			t.Fatalf("checks = %#v, want no contribution source inspection checks", checks)
		}
	}
}

func findCodexPluginCheck(t *testing.T, checks []findings.Check, severity findings.Severity, name string) findings.Check {
	t.Helper()
	for _, check := range checks {
		if check.Severity == severity && check.Name == name {
			return check
		}
	}
	t.Fatalf("checks = %#v, want %s %s", checks, severity, name)
	return findings.Check{}
}

func writeDiagnoseCodexConfig(t *testing.T, homeDirectory string, content string) {
	t.Helper()

	configPath := filepath.Join(homeDirectory, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func writeDiagnoseFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
