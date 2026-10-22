//go:build darwin || linux

package fileset

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestRollbackPastReadonlyParentLeavesNoFence(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires an unprivileged process for directory permission refusal")
	}
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	manifest := filepath.Join(root, "a-manifest")
	readonlyParent := filepath.Join(root, "z-readonly")
	lockfile := filepath.Join(readonlyParent, "lockfile")
	if err := os.Mkdir(readonlyParent, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, manifest, "manifest-before", 0o600)
	writeFixture(t, lockfile, "lock-before", 0o400)
	if err := os.Chmod(readonlyParent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(readonlyParent, 0o700); err != nil {
			t.Error(err)
		}
	})

	err := CommitFileSet(context.Background(), FileSetInput{
		StateDir: stateDir,
		Targets: []FileTarget{
			mustWriteTarget(t, manifest, "manifest-after"),
			mustWriteTarget(t, lockfile, "lock-after"),
		},
	})
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("commit error = %v, want permission refusal", err)
	}
	assertContent(t, manifest, "manifest-before")
	assertContent(t, lockfile, "lock-before")
	assertMode(t, manifest, 0o600)
	assertMode(t, lockfile, 0o400)
	assertMissing(t, transactionDir(stateDir))

	err = CommitFileSet(context.Background(), FileSetInput{
		StateDir: stateDir,
		Targets: []FileTarget{
			mustWriteTarget(t, manifest, "next-manifest"),
			mustRetainedTarget(t, lockfile),
		},
	})
	if err != nil {
		t.Fatalf("next write blocked by prior failure: %v", err)
	}
	assertContent(t, manifest, "next-manifest")
	assertContent(t, lockfile, "lock-before")
}
