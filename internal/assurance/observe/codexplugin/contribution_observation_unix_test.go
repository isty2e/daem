//go:build darwin || linux

package codexplugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
	"golang.org/x/sys/unix"
)

func TestObserveConfiguredPluginContributionsBlocksSpecialFileManifest(t *testing.T) {
	homeDirectory := t.TempDir()
	pluginRoot := codexPluginRoot(homeDirectory, "market", "alpha", "local")
	path := filepath.Join(pluginRoot, ".codex-plugin", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("Mkfifo returned error: %v", err)
	}

	observations := ObserveConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market"),
	)
	row := firstDiagnosticRow(t, observations[0])
	if row.State() != observecontribution.SourceContributionBlocked ||
		row.Reason() != observecontribution.SourceContributionReasonUnsupportedShape ||
		row.HasContribution() {
		t.Fatalf("observation = %#v, want unsupported special-file shape", observations[0])
	}
}

func TestObserveConfiguredPluginContributionsBlocksMarketplaceSymlink(t *testing.T) {
	homeDirectory := t.TempDir()
	outside := filepath.Join(homeDirectory, "outside-market")
	writeFile(t, filepath.Join(outside, "alpha", "local", ".codex-plugin", "plugin.json"), `{
  "mcpServers": {"outside": {}}
}`)
	cacheRoot := filepath.Join(homeDirectory, ".codex", "plugins", "cache")
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(cacheRoot, "market")); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	observations := ObserveConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market"),
	)
	row := firstDiagnosticRow(t, observations[0])
	if row.State() != observecontribution.SourceContributionBlocked ||
		row.Reason() != observecontribution.SourceContributionReasonArtifactPathBlocked ||
		row.HasContribution() {
		t.Fatalf("observation = %#v, want marketplace symlink path-blocked", observations[0])
	}
}

func TestObserveConfiguredPluginContributionsKeepsSiblingWhenNestedSkillIsBlocked(t *testing.T) {
	homeDirectory := t.TempDir()
	pluginRoot := codexPluginRoot(homeDirectory, "market", "alpha", "local")
	writeFile(t, filepath.Join(pluginRoot, "skills", "review", "SKILL.md"), "---\nname: review\n---\n")
	if err := os.MkdirAll(filepath.Join(pluginRoot, "skills", "blocked"), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.Symlink(
		filepath.Join(homeDirectory, "outside-skill.md"),
		filepath.Join(pluginRoot, "skills", "blocked", "SKILL.md"),
	); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}
	writeFile(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), `{
  "skills": ["./skills"]
}`)

	observations := ObserveConfiguredPluginContributions(
		t.Context(),
		homeDirectory,
		configuredPluginObservation(t, "alpha@market"),
	)
	row := firstDiagnosticRow(t, observations[0])
	if row.State() != observecontribution.SourceContributionDeclared {
		t.Fatalf("observation = %#v, want declared sibling skill", observations[0])
	}
	assertSourceContribution(t, observations[0].DiagnosticRows(), observecontribution.SourceContributionSkill, "review")
	for _, diagnostic := range observations[0].DiagnosticRows() {
		if diagnostic.HasContribution() && diagnostic.Key() == "blocked" {
			t.Fatalf("observation = %#v, want blocked nested skill skipped", observations[0])
		}
	}
}

func TestObserveConfiguredPluginContributionsIgnoresReplacedPluginRoot(t *testing.T) {
	homeDirectory := t.TempDir()
	pluginRoot := codexPluginRoot(homeDirectory, "market", "alpha", "local")
	writeFile(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), `{
  "mcpServers": {"inside": {}}
}`)
	outside := filepath.Join(homeDirectory, "outside")
	writeFile(t, filepath.Join(outside, ".codex-plugin", "plugin.json"), `{
  "mcpServers": {"outside": {}}
}`)

	dir, err := os.Open(pluginRoot)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = dir.Close() })
	if err := os.Rename(pluginRoot, pluginRoot+".moved"); err != nil {
		t.Fatalf("Rename returned error: %v", err)
	}
	if err := os.Symlink(outside, pluginRoot); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}

	observation := &pluginObservation{
		dir:       &observationDir{file: dir},
		budget:    &observationBudget{},
		snapshots: map[string]snapshotRecord{},
	}
	content, exists, reason, err := observation.snapshot(t.Context(), ".codex-plugin/plugin.json")
	if err != nil || !exists || reason != observecontribution.SourceContributionReasonNone {
		t.Fatalf("snapshot after replacement = (%q, %t, %q, %v)", content, exists, reason, err)
	}
	if !strings.Contains(string(content), `"inside"`) || strings.Contains(string(content), `"outside"`) {
		t.Fatalf("snapshot content = %q, want retained inside plugin.json", content)
	}
}
