package cli_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativeLockAuthoringLifecycle(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	lockPath := filepath.Join(workspace, "daem.lock.toml")
	testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["codex", "pi"]
[[host_config]]
id = "preferences"
targets = ["codex"]
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
[[host_config]]
id = "pi-preferences"
targets = ["pi"]
path = "settings.json"
source = "./preferences.json"
ownership = "keys"
`)
	testkit.WriteFile(t, workspace, "preferences.toml", "model = 'test'\n[tui]\nnotifications = false\n")
	testkit.WriteFile(t, workspace, "preferences.json", `{"retry":{"enabled":false},"a/b":true,"~":0,"":[]}`)
	testkit.WriteFile(t, workspace, "instructions.md", "project guidance\n")
	runNativeLifecycleCommand(t, "lock", "--manifest", manifest)
	before, err := lockfile.Load(t.Context(), lockPath)
	if err != nil {
		t.Fatal(err)
	}

	for _, command := range [][]string{
		{"add", "instruction", "project", filepath.Join(workspace, "instructions.md"), "--target", "codex"},
		{"add", "mcp-server", "example", "example-mcp", "--target", "codex"},
		{"remove", "instruction", "project"},
		{"remove", "mcp-server", "example"},
	} {
		args := append(append([]string(nil), command...), "--manifest", manifest)
		oldManifest := testkit.ReadFile(t, manifest)
		oldLock := testkit.ReadFile(t, lockPath)
		runNativeLifecycleCommand(t, append(args, "--dry-run")...)
		if !bytes.Equal(oldManifest, testkit.ReadFile(t, manifest)) || !bytes.Equal(oldLock, testkit.ReadFile(t, lockPath)) {
			t.Fatalf("preview published authoring changes: %v", command)
		}
		runNativeLifecycleCommand(t, args...)
		after, err := lockfile.Load(t.Context(), lockPath)
		if err != nil {
			t.Fatalf("read authoring result %v: %v", command, err)
		}
		if after.Version != lock.CurrentVersion {
			t.Fatalf("authoring selected a different schema: %d", after.Version)
		}
		for _, subject := range before.Locked.Subjects() {
			if subject.EntityID().Kind() != entity.KindHostConfig {
				continue
			}
			actual, found := after.Locked.Subject(subject.SubjectID())
			if !found || !subject.Equal(actual) {
				t.Fatalf("authoring %v lost native source/key contract %s", command, subject.SubjectID())
			}
		}
		for _, path := range []string{"AGENTS.md", ".codex/config.toml", ".pi/settings.json", ".daem/state.json"} {
			testkit.AssertPathMissing(t, filepath.Join(workspace, path))
		}
	}
	runNativeLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
	requireNativeLifecycleClean(t, runNativeLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))

	testkit.WriteFile(t, workspace, "daem.toml", "version = 1\ntargets = ['codex']\n")
	runNativeLifecycleCommand(t, "lock", "--manifest", manifest)
	ordinary, err := lockfile.Load(t.Context(), lockPath)
	if err != nil || ordinary.Version != before.Version || ordinary.Locked.Len() != 0 {
		t.Fatalf("native/ordinary locks do not share one schema: before=%d after=%#v err=%v", before.Version, ordinary, err)
	}
}

func TestLockSchemaSixRequiresRegeneration(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	manifest := filepath.Join(root, "daem.toml")
	lockPath := filepath.Join(root, "daem.lock.toml")
	testkit.WriteFile(t, root, "daem.toml", "version = 1\ntargets = ['codex']\n")
	legacy := []byte("version = 6\n[locked]\n")
	testkit.WriteFile(t, root, "daem.lock.toml", string(legacy))
	for _, command := range [][]string{{"status"}, {"apply", "--yes"}} {
		args := append(command, "--manifest", manifest)
		code, stdout, stderr := runOwnershipCLI(args...)
		if code == 0 || !bytes.Equal(legacy, testkit.ReadFile(t, lockPath)) {
			t.Fatalf("old-schema %v was used or modified: exit=%d stdout=%s stderr=%s", command, code, stdout, stderr)
		}
		testkit.AssertPathMissing(t, filepath.Join(root, ".daem", "state.json"))
	}
	runNativeLifecycleCommand(t, "lock", "--manifest", manifest, "--dry-run")
	if !bytes.Equal(legacy, testkit.ReadFile(t, lockPath)) {
		t.Fatal("upgrade preview replaced the legacy lock")
	}
	runNativeLifecycleCommand(t, "lock", "--manifest", manifest)
	current, err := lockfile.Load(t.Context(), lockPath)
	if err != nil || current.Version != lock.CurrentVersion {
		t.Fatalf("upgrade did not produce the current schema: %#v %v", current, err)
	}
	requireNativeLifecycleClean(t, runNativeLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
	runNativeLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
}
