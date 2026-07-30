//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"golang.org/x/sys/unix"
)

func TestSnapshotDirectoryReportsImmediateNoFollowFacts(t *testing.T) {
	root := canonicalTempDir(t)
	writeTestFile(t, filepath.Join(root, "file"), "abc", 0o640)
	if err := os.Mkdir(filepath.Join(root, "directory"), 0o750); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := os.Symlink("file", filepath.Join(root, "link")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "fifo"), 0o600); err != nil {
		t.Fatalf("create fifo: %v", err)
	}

	snapshot, err := SnapshotDirectory(t.Context(), root)
	if err != nil {
		t.Fatalf("SnapshotDirectory: %v", err)
	}
	if snapshot.RootIdentity().Kind() != mutationfs.EntryKindDirectory ||
		!snapshot.RootOwnedByInvoker() {
		t.Fatalf("root facts = (%s, %t)", snapshot.RootIdentity().Kind(), snapshot.RootOwnedByInvoker())
	}
	entries := snapshot.Entries()
	if len(entries) != 4 {
		t.Fatalf("entry count = %d, want 4", len(entries))
	}
	want := []struct {
		name string
		kind mutationfs.EntryKind
		mode os.FileMode
		size int64
	}{
		{name: "directory", kind: mutationfs.EntryKindDirectory, mode: 0o750, size: -1},
		{name: "fifo", kind: mutationfs.EntryKindSpecial, mode: 0o600, size: -1},
		{name: "file", kind: mutationfs.EntryKindFile, mode: 0o640, size: 3},
		{name: "link", kind: mutationfs.EntryKindSymlink, size: 4},
	}
	for index, expected := range want {
		actual := entries[index]
		if actual.Name() != expected.name || actual.Kind() != expected.kind ||
			(expected.mode != 0 && actual.Mode() != expected.mode) ||
			(expected.size >= 0 && actual.Size() != expected.size) ||
			!actual.OwnedByInvoker() {
			t.Fatalf(
				"entries[%d] = (%q, %s, %04o, %d, %t), want (%q, %s, %04o, %d, true)",
				index,
				actual.Name(),
				actual.Kind(),
				actual.Mode(),
				actual.Size(),
				actual.OwnedByInvoker(),
				expected.name,
				expected.kind,
				expected.mode,
				expected.size,
			)
		}
	}
}

func TestSnapshotDirectoryRejectsConcurrentEntrySetChange(t *testing.T) {
	root := canonicalTempDir(t)
	writeTestFile(t, filepath.Join(root, "before"), "before", 0o600)
	var actionErr error
	_, err := snapshotDirectoryWithFaults(t.Context(), root, faultPlan{actions: map[phase]func(){
		phaseReadPayload: func() {
			actionErr = os.Rename(
				filepath.Join(root, "before"),
				filepath.Join(root, "after"),
			)
		},
	}})
	if actionErr != nil {
		t.Fatalf("replace directory entry: %v", actionErr)
	}
	assertFailure(t, err, failureUncommitted, phaseRevalidateEntry)
}

func TestSnapshotDirectoryRejectsConcurrentEntryIdentityChange(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "entry")
	writeTestFile(t, path, "before", 0o600)
	var actionErr error
	_, err := snapshotDirectoryWithFaults(t.Context(), root, faultPlan{actions: map[phase]func(){
		phaseRevalidateEntry: func() {
			actionErr = os.Remove(path)
			if actionErr == nil {
				actionErr = os.WriteFile(path, []byte("after"), 0o600)
			}
		},
	}})
	if actionErr != nil {
		t.Fatalf("replace directory entry: %v", actionErr)
	}
	assertFailure(t, err, failureUncommitted, phaseRevalidateEntry)
}

func TestSnapshotDirectoryRejectsMissingNonDirectoryAndSymlinkRoot(t *testing.T) {
	root := canonicalTempDir(t)
	file := filepath.Join(root, "file")
	writeTestFile(t, file, "content", 0o600)
	link := filepath.Join(root, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatalf("create root symlink: %v", err)
	}
	for _, path := range []string{filepath.Join(root, "missing"), file, link} {
		if _, err := SnapshotDirectory(t.Context(), path); err == nil {
			t.Fatalf("SnapshotDirectory(%q) succeeded", path)
		}
	}
}

func TestSnapshotDirectoryCancellationReturnsNoSnapshot(t *testing.T) {
	root := canonicalTempDir(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	snapshot, err := SnapshotDirectory(ctx, root)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if snapshot.RootIdentity() != nil || snapshot.Entries() != nil {
		t.Fatal("cancelled directory snapshot exposed facts")
	}
}
