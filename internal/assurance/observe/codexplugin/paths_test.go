package codexplugin

import (
	"os"
	"path/filepath"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
)

func TestResolveHostPathsUsesCanonicalCodexHomeAndExactCacheIdentity(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "codex-home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "codex-home-alias")
	if err := os.Symlink(home, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", alias)

	paths, err := ResolveHostPaths()
	if err != nil {
		t.Fatalf("ResolveHostPaths: %v", err)
	}
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	if paths.ConfigPath() != filepath.Join(canonicalHome, "config.toml") {
		t.Fatalf("config path = %q, want canonical home", paths.ConfigPath())
	}
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		"documents@openai-primary-runtime",
	)
	if err != nil {
		t.Fatal(err)
	}
	key, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierCodexPlugin,
		target.TargetCodex,
		target.ScopeGlobal,
		source,
	)
	if err != nil {
		t.Fatal(err)
	}
	cachePath, err := paths.PluginCachePath(key)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(canonicalHome, "plugins", "cache", "openai-primary-runtime", "documents")
	if cachePath != want {
		t.Fatalf("cache path = %q, want %q", cachePath, want)
	}
}

func TestResolveHostPathsRejectsInvalidCodexHomeOverride(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	t.Setenv("CODEX_HOME", missing)
	if _, err := ResolveHostPaths(); err == nil {
		t.Fatal("missing CODEX_HOME was accepted")
	}

	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", file)
	if _, err := ResolveHostPaths(); err == nil {
		t.Fatal("file CODEX_HOME was accepted")
	}
}
