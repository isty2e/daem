package mutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureRevisionDistinguishesAbsentFileDirectoryAndSymlink(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "subject")
	absent := captureMutationTestRevision(t, path, PathEffectDirectoryEntry)
	if !absent.Equal(absent) {
		t.Fatal("captured absent revision is invalid")
	}
	if (SnapshotRevision{}).Equal(SnapshotRevision{}) {
		t.Fatal("zero revisions compared equal")
	}

	writeMutationTestFile(t, path, "one", 0o600)
	fileBefore := captureMutationTestRevision(t, path, PathEffectDirectoryEntry)
	if absent.Equal(fileBefore) {
		t.Fatal("absent and file revisions compared equal")
	}
	writeMutationTestFile(t, path, "two", 0o600)
	fileAfter := captureMutationTestRevision(t, path, PathEffectDirectoryEntry)
	if fileBefore.Equal(fileAfter) {
		t.Fatal("file content change did not change revision")
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := mkdirMutationTest(path); err != nil {
		t.Fatal(err)
	}
	writeMutationTestFile(t, filepath.Join(path, "child"), "child", 0o600)
	directory := captureMutationTestRevision(t, path, PathEffectDirectoryEntry)
	if fileAfter.Equal(directory) {
		t.Fatal("file and directory revisions compared equal")
	}

	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(root, "target")
	writeMutationTestFile(t, targetPath, "target", 0o600)
	if err := symlinkForMutationTest(targetPath, path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	link := captureMutationTestRevision(t, path, PathEffectDirectoryEntry)
	if directory.Equal(link) {
		t.Fatal("directory and symlink revisions compared equal")
	}
	referent := captureMutationTestRevision(t, path, PathEffectReferent)
	if link.Equal(referent) {
		t.Fatal("symlink entry and referent revisions compared equal")
	}
}

func TestCaptureRevisionIncludesExecutableSemantics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "script")
	writeMutationTestFile(t, path, "echo ok\n", 0o600)
	before := captureMutationTestRevision(t, path, PathEffectDirectoryEntry)
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	after := captureMutationTestRevision(t, path, PathEffectDirectoryEntry)
	if before.Equal(after) {
		t.Fatal("executable-bit change did not change revision")
	}
}

func TestDirectoryRevisionIsDeterministicAndIncludesNestedSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	writeMutationTestFile(t, filepath.Join(root, "b"), "b", 0o600)
	writeMutationTestFile(t, filepath.Join(root, "a"), "a", 0o600)
	link := filepath.Join(root, "nested-link")
	if err := symlinkForMutationTest("a", link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	first := captureMutationTestRevision(t, root, PathEffectDirectoryEntry)
	second := captureMutationTestRevision(t, root, PathEffectDirectoryEntry)
	if !first.Equal(second) {
		t.Fatal("unchanged directory produced nondeterministic revision")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := symlinkForMutationTest("b", link); err != nil {
		t.Fatal(err)
	}
	changed := captureMutationTestRevision(t, root, PathEffectDirectoryEntry)
	if first.Equal(changed) {
		t.Fatal("nested symlink target change did not change revision")
	}
}

func TestCaptureRevisionRejectsCanceledAndDanglingRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := CaptureRevision(ctx, RevisionRequest{Path: t.TempDir(), Effect: PathEffectDirectoryEntry})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled revision error = %v", err)
	}

	dangling := filepath.Join(t.TempDir(), "dangling")
	if err := symlinkForMutationTest("missing-target", dangling); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err = CaptureRevision(context.Background(), RevisionRequest{Path: dangling, Effect: PathEffectReferent})
	if err == nil {
		t.Fatal("dangling referent revision succeeded")
	}
}

func captureMutationTestRevision(t *testing.T, path string, effect PathEffect) SnapshotRevision {
	t.Helper()
	revision, err := CaptureRevision(context.Background(), RevisionRequest{Path: path, Effect: effect})
	if err != nil {
		t.Fatalf("CaptureRevision(%q) error: %v", path, err)
	}
	return revision
}
