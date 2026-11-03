package nativefixture

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func NewPresentation(t *testing.T, source, current string) RecoveryFixture {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	destination := filepath.Join(workspace, ".codex", "config.toml")
	testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["codex"]
[[host_config]]
id = "preferences"
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
`)
	testkit.WriteFile(t, workspace, "preferences.toml", source)
	testkit.WriteFile(t, filepath.Dir(destination), filepath.Base(destination), current)
	RunLifecycleCommand(t, "lock", "--manifest", manifest)
	return RecoveryFixture{Manifest: manifest, Destination: destination}
}
