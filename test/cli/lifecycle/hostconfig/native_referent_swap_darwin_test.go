package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeAcquisitionRefusesPostPublicationReferentSwap(t *testing.T) {
	selected := nativeKeyScopeNamed(t, "codex-global")
	selected.filename = "extension-settings/Café.toml"
	fixture := newNativeKeyScopeFixture(t, selected, false)
	workspace := filepath.Dir(fixture.Manifest)
	testkit.WriteFile(t, workspace, "preferences.toml", "native_other=true\n"+nativeKeyScopeSource(selected, "after-model"))
	nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "lock")
	code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture.RecoveryFixture, "replace-host-with-symlink", "apply", "--yes", "--verbose")
	if code != 1 {
		t.Fatalf("changed referent was not refused: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	evidencePaths := []string{fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath, fixture.Paths.RecoveryDir}
	evidence := nativefixture.SnapshotRecoveryPaths(t, evidencePaths...)
	for _, args := range [][]string{{"recover", "--dry-run", "--json"}, {"recover", "--yes"}} {
		code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture.RecoveryFixture, "none", args...)
		if code != 1 {
			t.Fatalf("changed referent was not refused by %v: exit=%d stdout=%s stderr=%s", args, code, stdout, stderr)
		}
		if !reflect.DeepEqual(evidence, nativefixture.SnapshotRecoveryPaths(t, evidencePaths...)) {
			t.Fatal("refused recovery changed State, ownership or journal evidence", args)
		}
	}
	other := filepath.Join(filepath.Dir(fixture.Destination), "unselected-other.toml")
	before, err := os.ReadFile(other + ".before")
	if err != nil {
		t.Fatal("referent-swap boundary was not reached", err)
	}
	actual, err := os.ReadFile(other)
	if err != nil || !bytes.Equal(actual, before) {
		t.Fatal("unselected referent content changed", err)
	}
	initialMode, err := os.Stat(other + ".before")
	if err != nil {
		t.Fatal(err)
	}
	currentMode, err := os.Lstat(other)
	if err != nil || currentMode.Mode().Perm() != initialMode.Mode().Perm() {
		t.Fatal("unselected referent permissions changed", err)
	}
	if target, err := os.Readlink(fixture.Destination); err != nil || target != filepath.Base(other) {
		t.Fatal("external replacement was changed by compensation", err)
	}
	requireUnchangedNativeSwapEntry(t, other)
	requireUnchangedNativeSwapEntry(t, fixture.Destination)
	for _, path := range []string{fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("refused acquisition published ownership or State", path, err)
		}
	}
	nativefixture.RequireRecoveryJournalState(t, fixture.RecoveryFixture, "host")
}

func requireUnchangedNativeSwapEntry(t *testing.T, path string) {
	t.Helper()
	metadata, err := os.ReadFile(path + ".stat")
	if err != nil {
		t.Fatal(err)
	}
	var initial syscall.Stat_t
	if err := json.Unmarshal(metadata, &initial); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	current, ok := info.Sys().(*syscall.Stat_t)
	if !ok || current.Dev != initial.Dev || current.Ino != initial.Ino {
		t.Fatal("unselected entry object was replaced", path)
	}
	if current.Mtimespec != initial.Mtimespec || current.Ctimespec != initial.Ctimespec {
		t.Fatal("unselected entry was written or its metadata mutated", path)
	}
}
