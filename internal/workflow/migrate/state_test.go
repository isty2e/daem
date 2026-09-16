package migrate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

func migrationFixture(t *testing.T) (daempaths.Paths, daempaths.Paths) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for key, dir := range map[string]string{
		"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state",
		"XDG_CACHE_HOME": "cache", "XDG_DATA_HOME": "data",
	} {
		t.Setenv(key, filepath.Join(root, dir))
	}
	manifest := filepath.Join(root, "config", "daem", "daem.toml")
	writeFixture(t, manifest, []byte("version = 1\ntargets = [\"pi\"]\n[defaults]\nscope = \"global\"\n"))
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := paths.LegacyUserState()
	if err != nil {
		t.Fatal(err)
	}
	content, err := statefile.Marshal(durable.EmptySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, legacy.StatefilePath, content)
	return paths, legacy
}

func writeFixture(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStateMigrationPreviewExecutionAndRepeat(t *testing.T) {
	paths, legacy := migrationFixture(t)
	before, err := os.ReadFile(legacy.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := recoverygate.RequireClear(t.Context(), paths); err == nil {
		t.Fatal("legacy state was ignored")
	}
	prepared, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if _, err := os.Lstat(paths.StateDir); !os.IsNotExist(err) {
		t.Fatalf("preview created state: %v", err)
	}
	if _, err := os.Lstat(paths.DataDir); !os.IsNotExist(err) {
		t.Fatalf("preview created data: %v", err)
	}
	unchanged, err := os.ReadFile(legacy.StatefilePath)
	if err != nil || !bytes.Equal(before, unchanged) {
		t.Fatalf("preview changed source: %v", err)
	}
	result, err := prepared.Execute(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "migrated" {
		t.Fatalf("result = %#v", result)
	}
	if _, err := statefile.Load(t.Context(), legacy.StatefilePath); err == nil {
		t.Fatal("retired source still loads as a snapshot")
	}
	if _, err := statefile.Load(t.Context(), paths.StatefilePath); err != nil {
		t.Fatal(err)
	}
	if err := recoverygate.RequireClear(t.Context(), paths); err != nil {
		t.Fatal(err)
	}
	repeat, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer repeat.Close()
	if repeat.Disclosure().Action != "already_migrated" {
		t.Fatalf("repeat = %#v", repeat.Disclosure())
	}
	if _, err := repeat.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Execute(t.Context()); err == nil {
		t.Fatal("preparation reused")
	}
}
