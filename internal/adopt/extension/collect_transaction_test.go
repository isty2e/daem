package extension_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	adoptextension "github.com/isty2e/daem/internal/adopt/extension"
	"github.com/isty2e/daem/internal/target"
)

func TestAntigravityLaterIncompleteBundleRollsBackEmittedRows(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, ".pi-agent"))
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	writeExtensionTransactionFixture(
		t,
		filepath.Join(root, ".gemini", "config", "import_manifest.json"),
		`{"imports":[{"name":"a"},{"name":"b"}]}`,
	)
	writeExtensionTransactionFixture(
		t,
		filepath.Join(root, ".gemini", "config", "plugins", "a", "plugin.json"),
		`{"name":"a"}`,
	)

	collector := adoptmodel.NewSkippedCollector()
	var result adoptextension.Result
	err := collector.Collect(func(skipped adoptmodel.SkipEmitter) error {
		var collectErr error
		result, collectErr = adoptextension.Collect(t.Context(), adoptextension.Input{
			ManifestRoot: root,
			Targets:      []target.Target{target.TargetAntigravityCLI},
			Scopes:       []target.Scope{target.ScopeGlobal},
		}, func(skip adoptextension.Skip) error {
			return skipped.Add(adoptmodel.Skipped{
				Target:   skip.Target,
				Scope:    skip.Scope,
				LivePath: skip.LivePath,
				Reason:   adoptmodel.SkipReason(skip.Reason),
			})
		})
		return collectErr
	})
	if err == nil || !strings.Contains(err.Error(), "partial import/bundle relation") {
		t.Fatalf("Collect error = %v, want incomplete bundle", err)
	}
	if len(collector.Skipped()) != 0 || len(result.Skipped()) != 0 {
		t.Fatalf("collector skipped = %#v, result skipped = %#v, want transaction rollback", collector.Skipped(), result.Skipped())
	}
}

func writeExtensionTransactionFixture(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
