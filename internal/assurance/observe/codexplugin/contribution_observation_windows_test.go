//go:build windows

package codexplugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
)

func TestWindowsPluginObservationStaysOnRetainedRootHandle(t *testing.T) {
	homeDirectory := t.TempDir()
	pluginRoot := codexPluginRoot(homeDirectory, "market", "alpha", "local")
	writeFile(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), `{
  "mcpServers": {"inside": {}}
}`)
	outside := filepath.Join(homeDirectory, "outside")
	writeFile(t, filepath.Join(outside, ".codex-plugin", "plugin.json"), `{
  "mcpServers": {"outside": {}}
}`)

	observation, reason, err := openPluginCacheLayout(pluginRoot, &observationBudget{})
	if err != nil || reason != observecontribution.SourceContributionReasonNone || observation == nil {
		t.Fatalf("openPluginCacheLayout = %v, %q, %v", observation, reason, err)
	}
	t.Cleanup(observation.close)
	if err := os.Rename(pluginRoot, pluginRoot+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(outside, pluginRoot); err != nil {
		t.Fatal(err)
	}

	content, exists, reason, err := observation.snapshot(t.Context(), ".codex-plugin/plugin.json")
	if err != nil || !exists || reason != observecontribution.SourceContributionReasonNone {
		t.Fatalf("snapshot after replacement = (%q, %t, %q, %v)", content, exists, reason, err)
	}
	if !strings.Contains(string(content), `"inside"`) || strings.Contains(string(content), `"outside"`) {
		t.Fatalf("snapshot content = %q, want retained inside plugin.json", content)
	}
}
