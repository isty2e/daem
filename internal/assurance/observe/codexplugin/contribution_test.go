package codexplugin

import (
	"os"
	"path/filepath"
	"testing"

	observeconfig "github.com/isty2e/daem/internal/assurance/observe/config"
	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
)

func TestObserveConfiguredPluginContributionsReportsSourceDeclaredRows(t *testing.T) {
	homeDirectory := t.TempDir()
	pluginRoot := codexPluginRoot(homeDirectory, "market", "alpha", "local")
	writeFile(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), `{
  "name": "alpha",
  "skills": "./skills/",
  "mcpServers": {
    "context7": {
      "command": "node",
      "env": {"SECRET_TOKEN": "must-not-leak"}
    }
  },
  "apps": "./.app.json",
  "hooks": "./hooks/hooks.json"
}`)
	writeFile(t, filepath.Join(pluginRoot, "skills", "review", "SKILL.md"), "---\nname: review\n---\n")
	writeFile(t, filepath.Join(pluginRoot, "skills", "search", "SKILL.md"), "---\nname: search\n---\n")
	writeFile(t, filepath.Join(pluginRoot, ".app.json"), `{"secret": "must-not-leak"}`)
	writeFile(t, filepath.Join(pluginRoot, "hooks", "hooks.json"), `{"secret": "must-not-leak"}`)

	observations := ObserveConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market"),
	)
	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want one", observations)
	}
	observation := observations[0]
	rows := observation.DiagnosticRows()
	if rows[0].State() != observecontribution.SourceContributionDeclared ||
		rows[0].Reason() != observecontribution.SourceContributionReasonNone ||
		rows[0].ArtifactIdentity() != "plugins/cache/market/alpha/local" {
		t.Fatalf("observation = %#v, want source-declared local artifact", observation)
	}
	providerSubject, ok := rows[0].ProviderSubject()
	if !ok || providerSubject.IsZero() {
		t.Fatalf("provider subject = %s/%t, want canonical alpha@market carrier", providerSubject, ok)
	}
	assertSourceContribution(t, rows, observecontribution.SourceContributionApp, "alpha")
	assertSourceContribution(t, rows, observecontribution.SourceContributionHook, "hooks.json")
	assertSourceContribution(t, rows, observecontribution.SourceContributionMCPServer, "context7")
	assertSourceContribution(t, rows, observecontribution.SourceContributionSkill, "review")
	assertSourceContribution(t, rows, observecontribution.SourceContributionSkill, "search")
}

func TestObserveConfiguredPluginContributionsClassifiesMissingAndAmbiguousArtifacts(t *testing.T) {
	homeDirectory := t.TempDir()
	writeFile(t, filepath.Join(codexPluginRoot(homeDirectory, "market", "multi", "1.0.0"), ".keep"), "")
	writeFile(t, filepath.Join(codexPluginRoot(homeDirectory, "market", "multi", "2.0.0"), ".keep"), "")

	observations := ObserveConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "missing@market", "multi@market"),
	)
	if len(observations) != 2 {
		t.Fatalf("observations = %#v, want two", observations)
	}
	missing := firstDiagnosticRow(t, observations[0])
	if missing.State() != observecontribution.SourceContributionUnavailable ||
		missing.Reason() != observecontribution.SourceContributionReasonArtifactUnavailable {
		t.Fatalf("missing observation = %#v, want unavailable", observations[0])
	}
	ambiguous := firstDiagnosticRow(t, observations[1])
	if ambiguous.State() != observecontribution.SourceContributionAmbiguous ||
		ambiguous.Reason() != observecontribution.SourceContributionReasonArtifactAmbiguous {
		t.Fatalf("ambiguous observation = %#v, want ambiguous", observations[1])
	}
}

func TestObserveConfiguredPluginContributionsPrefersLocalCacheVersion(t *testing.T) {
	homeDirectory := t.TempDir()
	writeFile(t, filepath.Join(codexPluginRoot(homeDirectory, "market", "alpha", "1.0.0"), ".codex-plugin", "plugin.json"), `{
  "mcpServers": {"old": {}}
}`)
	writeFile(t, filepath.Join(codexPluginRoot(homeDirectory, "market", "alpha", "local"), ".codex-plugin", "plugin.json"), `{
  "mcpServers": {"local": {}}
}`)

	observations := ObserveConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market"),
	)
	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want one", observations)
	}
	observation := observations[0]
	rows := observation.DiagnosticRows()
	if rows[0].State() != observecontribution.SourceContributionDeclared ||
		rows[0].ArtifactIdentity() != "plugins/cache/market/alpha/local" {
		t.Fatalf("observation = %#v, want local artifact", observation)
	}
	assertSourceContribution(t, rows, observecontribution.SourceContributionMCPServer, "local")
}

func TestObserveConfiguredPluginContributionsKeepsProviderScopedDuplicateNames(t *testing.T) {
	homeDirectory := t.TempDir()
	for _, plugin := range []string{"alpha", "beta"} {
		pluginRoot := codexPluginRoot(homeDirectory, "market", plugin, "local")
		writeFile(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), `{
  "skills": "./skills/"
}`)
		writeFile(t, filepath.Join(pluginRoot, "skills", "review", "SKILL.md"), "---\nname: review\n---\n")
	}

	observations := ObserveConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market", "beta@market"),
	)
	if len(observations) != 2 {
		t.Fatalf("observations = %#v, want two", observations)
	}
	for _, observation := range observations {
		rows := observation.DiagnosticRows()
		if rows[0].State() != observecontribution.SourceContributionDeclared {
			t.Fatalf("observation = %#v, want declared", observation)
		}
		assertSourceContribution(t, rows, observecontribution.SourceContributionSkill, "review")
	}
	firstRow := firstDiagnosticRow(t, observations[0])
	secondRow := firstDiagnosticRow(t, observations[1])
	firstProvider, firstOK := firstRow.ProviderSubject()
	secondProvider, secondOK := secondRow.ProviderSubject()
	if !firstOK || !secondOK ||
		firstRow.ProvidedBy() == secondRow.ProvidedBy() ||
		firstProvider == secondProvider ||
		firstRow.ArtifactIdentity() == secondRow.ArtifactIdentity() {
		t.Fatalf("observations = %#v, want provider-scoped duplicate contributions", observations)
	}
	firstContribution, firstOK := firstRow.ContributionSubject()
	secondContribution, secondOK := secondRow.ContributionSubject()
	if !firstOK || !secondOK || firstContribution == secondContribution {
		t.Fatalf("contribution subjects = %v/%t and %v/%t, want provider-scoped identities", firstContribution, firstOK, secondContribution, secondOK)
	}
}

func TestObserveConfiguredPluginContributionsSkipsUnsupportedConfigEntries(t *testing.T) {
	homeDirectory := t.TempDir()
	pluginRoot := codexPluginRoot(homeDirectory, "market", "alpha", "local")
	writeFile(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), `{
  "skills": "./skills/"
}`)
	writeFile(t, filepath.Join(pluginRoot, "skills", "review", "SKILL.md"), "---\nname: review\n---\n")

	observations := ObserveConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configObservationFromEntries(
			t,
			mustObservedConfigEntry(t, "alpha@market"),
			mustUnsupportedConfigEntry(t, "beta@market"),
		),
	)
	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want only observed config entry inspected", observations)
	}
	row := firstDiagnosticRow(t, observations[0])
	if row.ProvidedBy() != "alpha@market" ||
		row.State() != observecontribution.SourceContributionDeclared {
		t.Fatalf("observation = %#v, want only alpha source inspection", observations[0])
	}
}

func TestObserveConfiguredPluginContributionsRedactsUnsafeProviderKeys(t *testing.T) {
	homeDirectory := t.TempDir()

	observations := ObserveConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "bad\nprovider@market"),
	)
	if len(observations) != 1 {
		t.Fatalf("observations = %#v, want one", observations)
	}
	row := firstDiagnosticRow(t, observations[0])
	if row.ProvidedBy() != "<invalid-provider>" ||
		row.State() != observecontribution.SourceContributionBlocked ||
		row.Reason() != observecontribution.SourceContributionReasonProviderProvenanceRequired {
		t.Fatalf("observation = %#v, want redaction-safe provenance blocker", observations[0])
	}
	if _, ok := row.ProviderSubject(); ok {
		t.Fatal("invalid provider observation unexpectedly carried canonical provider correlation")
	}
}

func TestObserveConfiguredPluginContributionsBlocksMalformedAndUnsupportedShapes(t *testing.T) {
	homeDirectory := t.TempDir()
	writeFile(t, filepath.Join(codexPluginRoot(homeDirectory, "market", "malformed", "local"), ".codex-plugin", "plugin.json"), `{
  "mcpServers": {
`)
	writeFile(t, filepath.Join(codexPluginRoot(homeDirectory, "market", "unsupported", "local"), ".codex-plugin", "plugin.json"), `{
  "skills": {"review": true}
}`)

	observations := ObserveConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "malformed@market", "unsupported@market"),
	)
	if len(observations) != 2 {
		t.Fatalf("observations = %#v, want two", observations)
	}
	malformed := firstDiagnosticRow(t, observations[0])
	if malformed.State() != observecontribution.SourceContributionBlocked ||
		malformed.Reason() != observecontribution.SourceContributionReasonArtifactMalformed {
		t.Fatalf("malformed observation = %#v, want malformed blocker", observations[0])
	}
	unsupported := firstDiagnosticRow(t, observations[1])
	if unsupported.State() != observecontribution.SourceContributionBlocked ||
		unsupported.Reason() != observecontribution.SourceContributionReasonUnsupportedShape {
		t.Fatalf("unsupported observation = %#v, want unsupported-shape blocker", observations[1])
	}
}

func TestObserveConfiguredPluginContributionsBlocksUnsafeDiagnosticTokens(t *testing.T) {
	homeDirectory := t.TempDir()
	writeFile(t, filepath.Join(codexPluginRoot(homeDirectory, "market", "bad-mcp", "local"), ".codex-plugin", "plugin.json"), `{
  "mcpServers": {"bad\nname": {}}
}`)
	writeFile(t, filepath.Join(codexPluginRoot(homeDirectory, "market", "bad-hook", "local"), ".codex-plugin", "plugin.json"), `{
  "hooks": ["./hooks/bad\nhook.json"]
}`)
	appRoot := codexPluginRoot(homeDirectory, "market", "safe-app", "local")
	writeFile(t, filepath.Join(appRoot, ".codex-plugin", "plugin.json"), `{
  "name": "bad\napp",
  "apps": "./apps/app.json"
}`)
	writeFile(t, filepath.Join(appRoot, "apps", "app.json"), `{}`)

	observations := ObserveConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "bad-mcp@market", "bad-hook@market", "safe-app@market"),
	)
	if len(observations) != 3 {
		t.Fatalf("observations = %#v, want three", observations)
	}
	badMCP := firstDiagnosticRow(t, observations[0])
	if badMCP.State() != observecontribution.SourceContributionBlocked ||
		badMCP.Reason() != observecontribution.SourceContributionReasonUnsupportedShape {
		t.Fatalf("bad mcp observation = %#v, want unsupported-shape blocker", observations[0])
	}
	badHook := firstDiagnosticRow(t, observations[1])
	if badHook.State() != observecontribution.SourceContributionBlocked ||
		badHook.Reason() != observecontribution.SourceContributionReasonArtifactPathBlocked {
		t.Fatalf("bad hook observation = %#v, want path blocker", observations[1])
	}
	safeAppRows := observations[2].DiagnosticRows()
	if safeAppRows[0].State() != observecontribution.SourceContributionDeclared {
		t.Fatalf("safe app observation = %#v, want declared fallback", observations[2])
	}
	assertSourceContribution(t, safeAppRows, observecontribution.SourceContributionApp, "safe-app")
}

func TestObserveConfiguredPluginContributionsBlocksInvalidProviderAndEscapingPaths(t *testing.T) {
	homeDirectory := t.TempDir()
	traversalRoot := codexPluginRoot(homeDirectory, "market", "traversal", "local")
	writeFile(t, filepath.Join(traversalRoot, ".codex-plugin", "plugin.json"), `{
  "skills": "../outside"
}`)

	symlinkRoot := codexPluginRoot(homeDirectory, "market", "symlink", "local")
	writeFile(t, filepath.Join(symlinkRoot, ".codex-plugin", "plugin.json"), `{
  "skills": "./skills/"
}`)
	outside := filepath.Join(homeDirectory, "outside-skills")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatalf("MkdirAll outside returned error: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(symlinkRoot, "skills")); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	observations := ObserveConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "invalid", "traversal@market", "symlink@market"),
	)
	if len(observations) != 3 {
		t.Fatalf("observations = %#v, want three", observations)
	}
	if firstDiagnosticRow(t, observations[0]).Reason() != observecontribution.SourceContributionReasonProviderProvenanceRequired {
		t.Fatalf("invalid provider observation = %#v, want provenance blocker", observations[0])
	}
	if _, ok := observations[0].DiagnosticRows()[0].ProviderSubject(); ok {
		t.Fatal("invalid provider observation unexpectedly carried canonical provider correlation")
	}
	if firstDiagnosticRow(t, observations[1]).Reason() != observecontribution.SourceContributionReasonArtifactPathBlocked {
		t.Fatalf("traversal observation = %#v, want path blocker", observations[1])
	}
	if firstDiagnosticRow(t, observations[2]).Reason() != observecontribution.SourceContributionReasonArtifactPathBlocked {
		t.Fatalf("symlink observation = %#v, want path blocker", observations[2])
	}
	for index := 1; index < len(observations); index++ {
		provider, ok := observations[index].DiagnosticRows()[0].ProviderSubject()
		if !ok || provider.IsZero() {
			t.Fatalf("artifact blocker[%d] lost canonical provider correlation", index)
		}
	}
}

func TestObserveConfiguredPluginContributionsBlocksSymlinkParentComponents(t *testing.T) {
	homeDirectory := t.TempDir()

	manifestRoot := codexPluginRoot(homeDirectory, "market", "manifest-link", "local")
	outsideManifestDirectory := filepath.Join(homeDirectory, "outside-manifest")
	writeFile(t, filepath.Join(outsideManifestDirectory, "plugin.json"), `{
  "mcpServers": {"outside": {}}
}`)
	if err := os.MkdirAll(manifestRoot, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.Symlink(outsideManifestDirectory, filepath.Join(manifestRoot, ".codex-plugin")); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	mcpRoot := codexPluginRoot(homeDirectory, "market", "mcp-link", "local")
	writeFile(t, filepath.Join(mcpRoot, ".codex-plugin", "plugin.json"), `{
  "mcpServers": "./links/mcp.json"
}`)
	outsideMCPDirectory := filepath.Join(homeDirectory, "outside-mcp")
	writeFile(t, filepath.Join(outsideMCPDirectory, "mcp.json"), `{"outside": {}}`)
	if err := os.Symlink(outsideMCPDirectory, filepath.Join(mcpRoot, "links")); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	appRoot := codexPluginRoot(homeDirectory, "market", "app-link", "local")
	writeFile(t, filepath.Join(appRoot, ".codex-plugin", "plugin.json"), `{
  "apps": "./links/app.json"
}`)
	outsideAppDirectory := filepath.Join(homeDirectory, "outside-app")
	writeFile(t, filepath.Join(outsideAppDirectory, "app.json"), `{"secret": "must-not-leak"}`)
	if err := os.Symlink(outsideAppDirectory, filepath.Join(appRoot, "links")); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	hookRoot := codexPluginRoot(homeDirectory, "market", "hook-link", "local")
	writeFile(t, filepath.Join(hookRoot, ".codex-plugin", "plugin.json"), `{
  "hooks": "./links/hooks.json"
}`)
	outsideHookDirectory := filepath.Join(homeDirectory, "outside-hook")
	writeFile(t, filepath.Join(outsideHookDirectory, "hooks.json"), `{"secret": "must-not-leak"}`)
	if err := os.Symlink(outsideHookDirectory, filepath.Join(hookRoot, "links")); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	cacheMarketplace := filepath.Join(homeDirectory, ".codex", "plugins", "cache", "market")
	if err := os.MkdirAll(cacheMarketplace, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	outsideCache := filepath.Join(homeDirectory, "outside-cache")
	writeFile(t, filepath.Join(outsideCache, "local", ".codex-plugin", "plugin.json"), `{
  "mcpServers": {"outside": {}}
}`)
	if err := os.Symlink(outsideCache, filepath.Join(cacheMarketplace, "cache-link")); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	observations := ObserveConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(
			t,
			"manifest-link@market",
			"mcp-link@market",
			"app-link@market",
			"hook-link@market",
			"cache-link@market",
		),
	)
	if len(observations) != 5 {
		t.Fatalf("observations = %#v, want five", observations)
	}
	for _, observation := range observations {
		row := firstDiagnosticRow(t, observation)
		if row.State() != observecontribution.SourceContributionBlocked ||
			row.Reason() != observecontribution.SourceContributionReasonArtifactPathBlocked ||
			row.HasContribution() {
			t.Fatalf("observation = %#v, want blocked path without contributions", observation)
		}
	}
}

func firstDiagnosticRow(
	t *testing.T,
	observation observecontribution.SourceContributionObservation,
) observecontribution.SourceContributionDiagnosticRow {
	t.Helper()
	rows := observation.DiagnosticRows()
	if len(rows) == 0 {
		t.Fatal("source contribution observation has no diagnostic row")
	}
	return rows[0]
}

func assertSourceContribution(
	t *testing.T,
	rows []observecontribution.SourceContributionDiagnosticRow,
	kind observecontribution.SourceContributionKind,
	key string,
) {
	t.Helper()
	for _, row := range rows {
		if row.HasContribution() && row.Kind() == kind && row.Key() == key {
			return
		}
	}
	t.Fatalf("diagnostic rows = %#v, want %s %q", rows, kind, key)
}

func codexPluginRoot(homeDirectory string, marketplace string, plugin string, version string) string {
	return filepath.Join(homeDirectory, ".codex", "plugins", "cache", marketplace, plugin, version)
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) returned error: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", path, err)
	}
}

func configuredPluginObservation(t *testing.T, keys ...string) observeconfig.Observation {
	t.Helper()
	entries := make([]observeconfig.Entry, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, mustObservedConfigEntry(t, key))
	}
	return configObservationFromEntries(t, entries...)
}

func configObservationFromEntries(t *testing.T, entries ...observeconfig.Entry) observeconfig.Observation {
	t.Helper()
	observation, err := observeconfig.NewObservation(observeconfig.ObservationSpec{
		SourcePath:    "/tmp/codex/config.toml",
		ConfigExists:  true,
		EntrySetState: observeconfig.EntrySetObserved,
		Entries:       entries,
	})
	if err != nil {
		t.Fatalf("NewObservation: %v", err)
	}
	return observation
}

func mustObservedConfigEntry(t *testing.T, key string) observeconfig.Entry {
	t.Helper()
	entry, err := observeconfig.NewEntry(observeconfig.EntrySpec{
		Key:        observeconfig.Key(key),
		State:      observeconfig.EntryObserved,
		Activation: observeconfig.ActivationNotDeclared,
	})
	if err != nil {
		t.Fatalf("NewEntry(%q): %v", key, err)
	}
	return entry
}

func mustUnsupportedConfigEntry(t *testing.T, key string) observeconfig.Entry {
	t.Helper()
	entry, err := observeconfig.NewEntry(observeconfig.EntrySpec{
		Key:        observeconfig.Key(key),
		State:      observeconfig.EntryUnsupported,
		Activation: observeconfig.ActivationUnsupportedType,
		Reason:     observeconfig.ReasonActivationNotBoolean,
	})
	if err != nil {
		t.Fatalf("NewEntry(%q): %v", key, err)
	}
	return entry
}
