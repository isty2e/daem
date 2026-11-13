//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
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

	snapshot, err := SnapshotDirectory(t.Context(), root, 16)
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

func TestSnapshotRootedDirectoryEntriesAdmitsExactReservedArtifact(t *testing.T) {
	root := canonicalTempDir(t)
	legacy := filepath.Join(root, ".daem-tombstone-"+strings.Repeat("a", 32))
	if err := os.Mkdir(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(legacy, "journal.json"), "{}", 0o600)

	if _, err := SnapshotDirectory(t.Context(), legacy, 16); err == nil {
		t.Fatal("unrooted snapshot admitted reserved artifact")
	}

	captured, err := rootedpath.CaptureRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = captured.Close() })
	authority, err := rootedpath.BindSelectedEntryAuthority(captured, root, legacy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authority.Close() })
	capability, err := authority.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer capability.Close()

	snapshot, err := SnapshotRootedDirectoryEntries(t.Context(), capability, 16)
	if err != nil {
		t.Fatalf("SnapshotRootedDirectoryEntries: %v", err)
	}
	entries := snapshot.Entries()
	if len(entries) != 1 ||
		entries[0].Name() != "journal.json" ||
		entries[0].Kind() != mutationfs.EntryKindFile {
		t.Fatalf("entries = %#v, want exact journal entry", entries)
	}
}

func TestRootedDirectoryBatchSeparatesPartialAndCompleteCoverage(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "selected")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "beta", "gamma"} {
		writeTestFile(t, filepath.Join(path, name), name, 0o600)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "selected")
	defer capability.Close()

	for _, limit := range []int{0, 2, 3} {
		snapshot, more, err := SnapshotRootedDirectoryBatch(t.Context(), capability, limit)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Entries()) != limit || more != (limit < 3) || snapshot.RootMode() != 0o700 || !snapshot.RootOwnedByInvoker() {
			t.Fatal("batch lost its coverage or root facts", limit, more, snapshot)
		}
	}
	if _, err := SnapshotRootedDirectoryEntries(t.Context(), capability, 2); err == nil {
		t.Fatal("complete snapshot silently accepted hidden entries")
	}
	if _, err := SnapshotRootedDirectoryEntries(t.Context(), capability, 0); err == nil {
		t.Fatal("complete snapshot accepted root-only observation as complete")
	}
	complete, err := SnapshotRootedDirectoryEntries(t.Context(), capability, 3)
	if err != nil || len(complete.Entries()) != 3 {
		t.Fatal("batch consumed the capability or changed complete observation", err)
	}
}

func TestDirectoryPrefixRequiresEOFOrAnOverflowEntry(t *testing.T) {
	for _, count := range []int{2, 3, 1} {
		remaining := count
		observedEOF := false
		read := func(maximum int) ([]string, error) {
			if maximum <= 0 {
				t.Fatal("prefix requested an unbounded read")
			}
			if remaining == 0 {
				observedEOF = true
				return nil, io.EOF
			}
			remaining--
			return []string{fmt.Sprint(remaining)}, nil
		}
		names, more, err := readDirectoryPrefix(t.Context(), read, 2)
		if err != nil || len(names) != min(2, count) || more != (count > 2) || !more && !observedEOF {
			t.Fatal("short read was mistaken for exhaustive coverage", count, names, more, observedEOF, err)
		}
	}
	failed := errors.New("reader failed after a short result")
	first := true
	_, _, err := readDirectoryPrefix(t.Context(), func(int) ([]string, error) {
		if first {
			first = false
			return []string{"observed"}, nil
		}
		return nil, failed
	}, 2)
	if !errors.Is(err, failed) {
		t.Fatal("short result hid a later observation failure", err)
	}
}

func TestSnapshotDirectoryRejectsConcurrentEntrySetChange(t *testing.T) {
	root := canonicalTempDir(t)
	writeTestFile(t, filepath.Join(root, "before"), "before", 0o600)
	var actionErr error
	_, err := snapshotDirectoryWithFaults(t.Context(), root, 16, faultPlan{actions: map[phase]func(){
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
	_, err := snapshotDirectoryWithFaults(t.Context(), root, 16, faultPlan{actions: map[phase]func(){
		phaseRevalidateEntry: func() {
			actionErr = os.Rename(path, filepath.Join(t.TempDir(), "original"))
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
		if _, err := SnapshotDirectory(t.Context(), path, 16); err == nil {
			t.Fatalf("SnapshotDirectory(%q) succeeded", path)
		}
	}
}

func TestSnapshotDirectoryCancellationReturnsNoSnapshot(t *testing.T) {
	root := canonicalTempDir(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	snapshot, err := SnapshotDirectory(ctx, root, 16)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if snapshot.RootIdentity() != nil || snapshot.Entries() != nil {
		t.Fatal("cancelled directory snapshot exposed facts")
	}
}

func TestSnapshotDirectoryEnforcesExactEntryBound(t *testing.T) {
	root := canonicalTempDir(t)
	for index := range 3 {
		writeTestFile(
			t,
			filepath.Join(root, fmt.Sprintf("entry-%d", index)),
			"payload",
			0o600,
		)
	}
	if _, err := SnapshotDirectory(t.Context(), root, 3); err != nil {
		t.Fatalf("SnapshotDirectory at exact bound: %v", err)
	}
	if _, err := SnapshotDirectory(t.Context(), root, 2); err == nil {
		t.Fatal("SnapshotDirectory above bound succeeded")
	}
}

func TestReadDirectoryNamesCancelsBetweenBatches(t *testing.T) {
	root := canonicalTempDir(t)
	for index := range 300 {
		writeTestFile(
			t,
			filepath.Join(root, fmt.Sprintf("entry-%03d", index)),
			"",
			0o600,
		)
	}
	directory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()

	ctx := &cancelAfterErrChecksContext{
		Context: t.Context(),
		allowed: 1,
	}
	if _, err := readDirectoryNames(ctx, int(directory.Fd()), root, 300); !errors.Is(err, context.Canceled) {
		t.Fatalf("readDirectoryNames error = %v, want cancellation", err)
	}
}

type cancelAfterErrChecksContext struct {
	context.Context
	allowed int
	checks  int
}

func (ctx *cancelAfterErrChecksContext) Err() error {
	ctx.checks++
	if ctx.checks > ctx.allowed {
		return context.Canceled
	}
	return ctx.Context.Err()
}
