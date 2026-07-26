//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestCommitLogicalRemovalRemovesFileTreeAndSymlink(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{
			name: "file",
			setup: func(t *testing.T, path string) {
				writeTestFile(t, path, "payload", 0o600)
			},
		},
		{
			name: "tree",
			setup: func(t *testing.T, path string) {
				if err := os.MkdirAll(filepath.Join(path, "nested"), 0o700); err != nil {
					t.Fatalf("MkdirAll returned error: %v", err)
				}
				writeTestFile(t, filepath.Join(path, "nested", "file"), "payload", 0o600)
				if err := os.Symlink("file", filepath.Join(path, "nested", "link")); err != nil {
					t.Fatalf("Symlink returned error: %v", err)
				}
			},
		},
		{
			name: "read-only tree",
			setup: func(t *testing.T, path string) {
				if err := os.MkdirAll(filepath.Join(path, "nested"), 0o700); err != nil {
					t.Fatalf("MkdirAll returned error: %v", err)
				}
				writeTestFile(t, filepath.Join(path, "nested", "file"), "payload", 0o600)
				if err := os.Chmod(filepath.Join(path, "nested"), 0o500); err != nil {
					t.Fatalf("Chmod nested returned error: %v", err)
				}
				if err := os.Chmod(path, 0o500); err != nil {
					t.Fatalf("Chmod root returned error: %v", err)
				}
			},
		},
		{
			name: "symlink",
			setup: func(t *testing.T, path string) {
				if err := os.Symlink("outside", path); err != nil {
					t.Fatalf("Symlink returned error: %v", err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := canonicalTempDir(t)
			target := filepath.Join(root, "active")
			test.setup(t, target)
			request, err := NewLogicalRemoval(target, captureIdentity(t, target))
			if err != nil {
				t.Fatalf("NewLogicalRemoval returned error: %v", err)
			}
			if err := CommitLogicalRemoval(context.Background(), request); err != nil {
				t.Fatalf("CommitLogicalRemoval returned error: %v", err)
			}
			if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("target error = %v, want not exist", err)
			}
			assertNoPrivateEntries(t, root)
		})
	}
}

func TestCommitLogicalRemovalRejectsStaleIdentity(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "active")
	writeTestFile(t, target, "before", 0o600)
	request, err := NewLogicalRemoval(target, captureIdentity(t, target))
	if err != nil {
		t.Fatalf("NewLogicalRemoval returned error: %v", err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	writeTestFile(t, target, "external", 0o600)
	assertFailure(t, CommitLogicalRemoval(context.Background(), request), failureUncommitted, phaseValidate)
	assertFile(t, target, "external", 0o600)
}

func TestCommitLogicalRemovalRejectsRecreatedDirectory(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "active")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}
	request, err := NewLogicalRemoval(target, captureIdentity(t, target))
	if err != nil {
		t.Fatalf("NewLogicalRemoval returned error: %v", err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("recreate directory: %v", err)
	}
	assertFailure(t, CommitLogicalRemoval(context.Background(), request), failureUncommitted, phaseValidate)
	if _, statErr := os.Stat(target); statErr != nil {
		t.Fatalf("recreated directory was removed: %v", statErr)
	}
}

func TestCommitLogicalRemovalFaultClassification(t *testing.T) {
	tests := []struct {
		phase phase
		kind  mutationfs.FailureKind
		seen  bool
		tomb  bool
	}{
		{phase: phaseValidate, kind: failureUncommitted, seen: true},
		{phase: phaseRevalidateEntry, kind: failureUncommitted, seen: true},
		{phase: phaseCommitTombstone, kind: failureUncommitted, seen: true},
		{phase: phaseVerifyEntry, kind: failureIndeterminateCommit, tomb: true},
		{phase: phaseSyncParent, kind: failureIndeterminateCommit, tomb: true},
		{phase: phaseCleanupTombstone, kind: failureRetainedResidue, tomb: true},
		{phase: phaseSyncCleanupParent, kind: failureRetainedResidue},
	}
	for _, test := range tests {
		t.Run(string(test.phase), func(t *testing.T) {
			root := canonicalTempDir(t)
			target := filepath.Join(root, "active")
			writeTestFile(t, target, "payload", 0o600)
			request, err := NewLogicalRemoval(target, captureIdentity(t, target))
			if err != nil {
				t.Fatalf("NewLogicalRemoval returned error: %v", err)
			}
			err = commitLogicalRemovalWithFaults(context.Background(), request, faultAt(test.phase))
			failure := assertFailure(t, err, test.kind, test.phase)
			_, targetErr := os.Lstat(target)
			if test.seen && targetErr != nil {
				t.Fatalf("target error = %v, want visible", targetErr)
			}
			if !test.seen && !errors.Is(targetErr, os.ErrNotExist) {
				t.Fatalf("target error = %v, want absent", targetErr)
			}
			hasTombstone := directoryHasPrefix(t, root, tombstonePrefix)
			if hasTombstone != test.tomb {
				t.Fatalf("has tombstone = %t, want %t; residue=%v", hasTombstone, test.tomb, failure.retainedResidue())
			}
		})
	}
}

func TestCommitLogicalRemovalKeepsUnsupportedNamespaceSyncIndeterminate(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "active")
	writeTestFile(t, target, "payload", 0o600)
	request, err := NewLogicalRemoval(target, captureIdentity(t, target))
	if err != nil {
		t.Fatalf("NewLogicalRemoval returned error: %v", err)
	}
	faults := faultPlan{failures: map[phase]error{
		phaseSyncParent: unsupported("injected unsupported directory sync", nil),
	}}
	err = commitLogicalRemovalWithFaults(context.Background(), request, faults)
	failure := assertFailure(t, err, failureIndeterminateCommit, phaseSyncParent)
	if _, statErr := os.Lstat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("target error = %v, want absent after visibility commit", statErr)
	}
	if len(failure.retainedResidue()) != 1 || !strings.Contains(filepath.Base(failure.retainedResidue()[0]), tombstonePrefix) {
		t.Fatalf("residue = %v, want retained tombstone", failure.retainedResidue())
	}
}

func TestCommitLogicalRemovalHonorsCancellation(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "active")
	writeTestFile(t, target, "payload", 0o600)
	request, err := NewLogicalRemoval(target, captureIdentity(t, target))
	if err != nil {
		t.Fatalf("NewLogicalRemoval returned error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assertFailure(t, CommitLogicalRemoval(ctx, request), failureUncommitted, phaseValidate)
	assertFile(t, target, "payload", 0o600)
}

func TestCommitLogicalRemovalAncestorSwapDoesNotRemoveAttackerEntry(t *testing.T) {
	root := canonicalTempDir(t)
	activeParent := filepath.Join(root, "owned")
	movedParent := filepath.Join(root, "moved")
	attackerParent := filepath.Join(root, "attacker")
	if err := os.Mkdir(activeParent, 0o700); err != nil {
		t.Fatalf("Mkdir active parent returned error: %v", err)
	}
	if err := os.Mkdir(attackerParent, 0o700); err != nil {
		t.Fatalf("Mkdir attacker parent returned error: %v", err)
	}
	target := filepath.Join(activeParent, "active")
	attackerTarget := filepath.Join(attackerParent, "active")
	writeTestFile(t, target, "managed", 0o600)
	writeTestFile(t, attackerTarget, "attacker", 0o600)
	request, err := NewLogicalRemoval(target, captureIdentity(t, target))
	if err != nil {
		t.Fatalf("NewLogicalRemoval returned error: %v", err)
	}
	var actionErr error
	faults := faultPlan{actions: map[phase]func(){
		phaseCommitTombstone: func() {
			if err := os.Rename(activeParent, movedParent); err != nil {
				actionErr = err
				return
			}
			actionErr = os.Symlink(attackerParent, activeParent)
		},
	}}
	err = commitLogicalRemovalWithFaults(context.Background(), request, faults)
	if actionErr != nil {
		t.Fatalf("ancestor swap returned error: %v", actionErr)
	}
	assertFailure(t, err, failureIndeterminateCommit, phaseVerifyEntry)
	assertFile(t, attackerTarget, "attacker", 0o600)
	if _, err := os.Lstat(filepath.Join(movedParent, "active")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("moved active error = %v, want absent after tombstone rename", err)
	}
	if !directoryHasPrefix(t, movedParent, tombstonePrefix) {
		t.Fatal("moved parent has no retained tombstone")
	}
}

func TestCommitLogicalRemovalCancellationDuringCleanupRetainsTombstone(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "active")
	writeTestFile(t, target, "payload", 0o600)
	request, err := NewLogicalRemoval(target, captureIdentity(t, target))
	if err != nil {
		t.Fatalf("NewLogicalRemoval returned error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	faults := faultPlan{actions: map[phase]func(){phaseCleanupTombstone: cancel}}
	err = commitLogicalRemovalWithFaults(ctx, request, faults)
	assertFailure(t, err, failureRetainedResidue, phaseCleanupTombstone)
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("active path error = %v, want absent", err)
	}
	if !directoryHasPrefix(t, root, tombstonePrefix) {
		t.Fatal("cancelled cleanup did not retain tombstone")
	}
}

func directoryHasPrefix(t *testing.T, directory string, prefix string) bool {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			return true
		}
	}
	return false
}
