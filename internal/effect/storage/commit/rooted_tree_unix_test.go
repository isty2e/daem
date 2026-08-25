//go:build darwin || linux

package commit

import (
	"context"
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

func TestPreparedTreeMetadataFactsDistinguishXattrValues(t *testing.T) {
	left := preparedTreeMetadataFacts{xattrs: []preparedTreeXattrFact{{
		name: "platform.metadata", size: 4, digest: sha256.Sum256([]byte("left")),
	}}}
	right := preparedTreeMetadataFacts{xattrs: []preparedTreeXattrFact{{
		name: "platform.metadata", size: 5, digest: sha256.Sum256([]byte("right")),
	}}}
	if left.equal(right) {
		t.Fatal("prepared tree metadata facts treated different xattr values as equal")
	}
	if left.creationMetadata().equal(right.creationMetadata()) {
		t.Fatal("prepared tree creation metadata treated different xattr values as equal")
	}
}

func TestPreparedRootedTreePublishesRootedTreeAndConsumesWriter(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/skills/review")
	publishedRoot := filepath.Join(root, ".agents", "skills", "review")
	t.Cleanup(func() {
		_ = os.Chmod(filepath.Join(publishedRoot, "nested"), 0o700)
		_ = os.Chmod(publishedRoot, 0o700)
	})
	var retained mutationfs.RootedTreeWriter
	prepared, err := PrepareRootedTree(context.Background(), capability, func(writer mutationfs.RootedTreeWriter) error {
		retained = writer
		if err := writer.SetRootMode(0o550); err != nil {
			return err
		}
		nested := treePathForTest(t, "nested")
		if err := writer.CreateDirectory(nested, 0o500); err != nil {
			return err
		}
		return writer.WriteFile(treePathForTest(t, "nested", "SKILL.md"), 0o640, strings.NewReader("skill"))
	})
	if err != nil {
		t.Fatalf("PrepareRootedTree returned error: %v", err)
	}
	if err := retained.WriteFile(treePathForTest(t, "late"), 0o600, strings.NewReader("late")); err == nil {
		t.Fatal("retained rooted tree writer remained active")
	}
	if err := prepared.Commit(context.Background()); err != nil {
		t.Fatalf("PreparedRootedTree.Commit returned error: %v", err)
	}
	assertClosedRootedCapability(t, capability)
	assertFile(t, filepath.Join(publishedRoot, "nested", "SKILL.md"), "skill", 0o640)
	rootInfo, err := os.Stat(publishedRoot)
	if err != nil {
		t.Fatalf("stat published root: %v", err)
	}
	if rootInfo.Mode().Perm() != 0o550 {
		t.Fatalf("published root mode = %o, want 550", rootInfo.Mode().Perm())
	}
	info, err := os.Stat(filepath.Join(publishedRoot, "nested"))
	if err != nil {
		t.Fatalf("stat published directory: %v", err)
	}
	if info.Mode().Perm() != 0o500 {
		t.Fatalf("published directory mode = %o, want 500", info.Mode().Perm())
	}
	if err := prepared.Abort(context.Background()); err != nil {
		t.Fatalf("Abort after Commit returned error: %v", err)
	}
}

func TestPreparedRootedTreePublishesRestrictiveModes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "published")
	publishedRoot := filepath.Join(root, "published")
	publishedDirectory := filepath.Join(publishedRoot, "nested")
	publishedFile := filepath.Join(publishedDirectory, "entry")
	t.Cleanup(func() {
		_ = os.Chmod(publishedRoot, 0o700)
		_ = os.Chmod(publishedDirectory, 0o700)
		_ = os.Chmod(publishedFile, 0o600)
	})
	prepared, err := PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		if err := writer.SetRootMode(0o000); err != nil {
			return err
		}
		if err := writer.CreateDirectory(treePathForTest(t, "nested"), 0o000); err != nil {
			return err
		}
		return writer.WriteFile(treePathForTest(t, "nested", "entry"), 0o000, strings.NewReader("planned"))
	})
	if err != nil {
		t.Fatalf("PrepareRootedTree returned error: %v", err)
	}
	if err := prepared.Commit(t.Context()); err != nil {
		t.Fatalf("PreparedRootedTree.Commit returned error: %v", err)
	}
	assertClosedRootedCapability(t, capability)
	rootInfo, err := os.Stat(publishedRoot)
	if err != nil {
		t.Fatalf("stat published root: %v", err)
	}
	if rootInfo.Mode().Perm() != 0o000 {
		t.Fatalf("published root mode = %o, want 0", rootInfo.Mode().Perm())
	}
	if err := os.Chmod(publishedRoot, 0o700); err != nil {
		t.Fatalf("make published root inspectable: %v", err)
	}
	directoryInfo, err := os.Stat(publishedDirectory)
	if err != nil {
		t.Fatalf("stat published directory: %v", err)
	}
	if directoryInfo.Mode().Perm() != 0o000 {
		t.Fatalf("published directory mode = %o, want 0", directoryInfo.Mode().Perm())
	}
	if err := os.Chmod(publishedDirectory, 0o700); err != nil {
		t.Fatalf("make published directory inspectable: %v", err)
	}
	fileInfo, err := os.Stat(publishedFile)
	if err != nil {
		t.Fatalf("stat published file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o000 {
		t.Fatalf("published file mode = %o, want 0", fileInfo.Mode().Perm())
	}
	if err := os.Chmod(publishedFile, 0o600); err != nil {
		t.Fatalf("make published file readable: %v", err)
	}
	assertFileContent(t, publishedFile, "planned")
}

func TestPrepareRootedTreeCallbackFailureCleansStageAndAncestors(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/skills/review")
	want := errors.New("populate failed")
	prepared, err := PrepareRootedTree(context.Background(), capability, func(writer mutationfs.RootedTreeWriter) error {
		if err := writer.WriteFile(treePathForTest(t, "partial"), 0o600, strings.NewReader("partial")); err != nil {
			return err
		}
		return want
	})
	if prepared != nil {
		t.Fatal("PrepareRootedTree returned a stage after callback failure")
	}
	assertFailure(t, err, failureUncommitted, phaseWritePayload)
	if !errors.Is(err, want) {
		t.Fatalf("PrepareRootedTree error = %v, want callback cause", err)
	}
	assertClosedRootedCapability(t, capability)
	if _, statErr := os.Lstat(filepath.Join(root, ".agents")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("failed preparation retained project ancestry: %v", statErr)
	}
}

func TestPrepareRootedTreeRejectsStructureOutsideCleanupLimitsWithoutResidue(t *testing.T) {
	for _, test := range []struct {
		name     string
		limits   mutationfs.TreeTraversalLimits
		populate func(*testing.T, mutationfs.RootedTreeWriter) error
		want     string
	}{
		{
			name:   "entry count",
			limits: treeLimitsForTest(t, 2, 4),
			populate: func(t *testing.T, writer mutationfs.RootedTreeWriter) error {
				t.Helper()
				for _, name := range []string{"one", "two"} {
					if err := writer.WriteFile(treePathForTest(t, name), 0o600, strings.NewReader(name)); err != nil {
						return err
					}
				}
				return writer.WriteFile(treePathForTest(t, "three"), 0o600, strings.NewReader("three"))
			},
			want: "tree exceeds 2 entries",
		},
		{
			name:   "directory depth",
			limits: treeLimitsForTest(t, 8, 1),
			populate: func(t *testing.T, writer mutationfs.RootedTreeWriter) error {
				t.Helper()
				if err := writer.CreateDirectory(treePathForTest(t, "one"), 0o700); err != nil {
					return err
				}
				return writer.CreateDirectory(treePathForTest(t, "one", "two"), 0o700)
			},
			want: "tree exceeds maximum depth 1",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "project")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			captured := captureRootForCommitTest(t, root)
			capability := rootedCapabilityForCommitTest(t, captured, ".agents/skills/review")
			prepared, err := PrepareRootedTreeWithLimits(
				context.Background(),
				capability,
				test.limits,
				func(writer mutationfs.RootedTreeWriter) error {
					return test.populate(t, writer)
				},
			)
			if prepared != nil {
				t.Fatal("prepare returned an over-limit stage")
			}
			assertFailure(t, err, failureUncommitted, phaseWritePayload)
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("prepare error = %v, want %q", err, test.want)
			}
			assertClosedRootedCapability(t, capability)
			if _, statErr := os.Lstat(filepath.Join(root, ".agents")); !errors.Is(statErr, fs.ErrNotExist) {
				t.Fatalf("over-limit preparation retained staging ancestry: %v", statErr)
			}
		})
	}
}

func TestRootedTreeStagingStructureLimitMatchesCleanupAdmission(t *testing.T) {
	defaults := defaultTreeTraversalLimits()
	if defaults.MaximumEntries() != 100_000 ||
		defaults.MaximumDepth() != 64 ||
		defaults.MaximumBytes() != 4<<30 {
		t.Fatalf(
			"staging publication capability = entries:%d depth:%d bytes:%d, want 100000/64/4GiB",
			defaults.MaximumEntries(),
			defaults.MaximumDepth(),
			defaults.MaximumBytes(),
		)
	}
}

func TestPrepareRootedTreeAcceptsExactCleanupStructureBoundary(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/skills/review")
	prepared, err := PrepareRootedTreeWithLimits(
		context.Background(),
		capability,
		treeLimitsForTest(t, 2, 1),
		func(writer mutationfs.RootedTreeWriter) error {
			if err := writer.CreateDirectory(treePathForTest(t, "one"), 0o700); err != nil {
				return err
			}
			return writer.WriteFile(
				treePathForTest(t, "one", "file"),
				0o600,
				strings.NewReader("content"),
			)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Abort(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Lstat(filepath.Join(root, ".agents")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("aborted boundary tree retained staging ancestry: %v", statErr)
	}
}

func TestPrepareRootedTreeWithLimitsEnforcesRegularFileBytes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "bounded")
	limits, err := mutationfs.NewTreeTraversalLimits(1, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareRootedTreeWithLimits(
		t.Context(),
		capability,
		limits,
		func(writer mutationfs.RootedTreeWriter) error {
			return writer.WriteFile(
				treePathForTest(t, "content"),
				0o600,
				strings.NewReader("four"),
			)
		},
	)
	if prepared != nil {
		t.Fatal("prepare returned an over-limit stage")
	}
	assertFailure(t, err, failureUncommitted, phaseWritePayload)
	if !strings.Contains(err.Error(), "tree exceeds 3 regular-file bytes") {
		t.Fatalf("prepare error = %v, want byte-bound rejection", err)
	}
	assertClosedRootedCapability(t, capability)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("over-limit preparation retained entries: %v", entries)
	}
}

func TestPrepareRootedTreeWithLimitsAcceptsExactRegularFileBytes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "bounded")
	limits, err := mutationfs.NewTreeTraversalLimits(1, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareRootedTreeWithLimits(
		t.Context(),
		capability,
		limits,
		func(writer mutationfs.RootedTreeWriter) error {
			return writer.WriteFile(
				treePathForTest(t, "content"),
				0o600,
				strings.NewReader("yes"),
			)
		},
	)
	if err != nil {
		t.Fatalf("PrepareRootedTreeWithLimits returned error: %v", err)
	}
	if err := prepared.Abort(t.Context()); err != nil {
		t.Fatalf("Abort returned error: %v", err)
	}
}

func TestPrepareRootedTreeWithLimitsRejectsDelayedExtraByte(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "bounded")
	limits, err := mutationfs.NewTreeTraversalLimits(1, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareRootedTreeWithLimits(
		t.Context(),
		capability,
		limits,
		func(writer mutationfs.RootedTreeWriter) error {
			return writer.WriteFile(
				treePathForTest(t, "content"),
				0o600,
				&delayedExtraByteReader{},
			)
		},
	)
	if prepared != nil {
		t.Fatal("prepare returned a stage with delayed over-limit content")
	}
	assertFailure(t, err, failureUncommitted, phaseWritePayload)
	if !strings.Contains(err.Error(), "tree exceeds 3 regular-file bytes") {
		t.Fatalf("prepare error = %v, want byte-bound rejection", err)
	}
}

func TestPrepareRootedTreeRejectsDefaultCleanupDepthBoundaryWithoutResidue(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/skills/review")
	prepared, err := PrepareRootedTree(context.Background(), capability, func(writer mutationfs.RootedTreeWriter) error {
		maximumDepth := defaultTreeTraversalLimits().MaximumDepth()
		components := make([]string, 0, maximumDepth+1)
		for depth := 1; depth <= maximumDepth+1; depth++ {
			components = append(components, "nested")
			if err := writer.CreateDirectory(treePathForTest(t, components...), 0o700); err != nil {
				return err
			}
		}
		return nil
	})
	if prepared != nil {
		t.Fatal("prepare returned a stage deeper than cleanup can traverse")
	}
	assertFailure(t, err, failureUncommitted, phaseWritePayload)
	if !strings.Contains(err.Error(), "tree exceeds maximum depth 64") {
		t.Fatalf("prepare error = %v, want depth-bound rejection", err)
	}
	assertClosedRootedCapability(t, capability)
	if _, statErr := os.Lstat(filepath.Join(root, ".agents")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("depth-bound preparation retained staging ancestry: %v", statErr)
	}
}

func TestPrepareRootedTreeCallbackPanicCleansStageAndAncestors(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/skills/review")

	func() {
		defer func() {
			if recovered := recover(); recovered != "populate panic" {
				t.Fatalf("recovered value = %v, want populate panic", recovered)
			}
		}()
		_, _ = PrepareRootedTree(context.Background(), capability, func(writer mutationfs.RootedTreeWriter) error {
			if err := writer.WriteFile(treePathForTest(t, "partial"), 0o600, strings.NewReader("partial")); err != nil {
				t.Fatalf("write partial rooted tree: %v", err)
			}
			panic("populate panic")
		})
	}()

	assertClosedRootedCapability(t, capability)
	if _, statErr := os.Lstat(filepath.Join(root, ".agents")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("panicked preparation retained project ancestry: %v", statErr)
	}
}

func TestPreparedRootedTreeAbortCleansPrivateStage(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/skills/review")
	prepared := prepareRootedTreeForTest(t, capability)
	stagePath := prepared.stagePath
	if err := prepared.Abort(context.Background()); err != nil {
		t.Fatalf("PreparedRootedTree.Abort returned error: %v", err)
	}
	assertClosedRootedCapability(t, capability)
	if _, err := os.Lstat(stagePath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("aborted stage remains: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".agents")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("aborted stage ancestry remains: %v", err)
	}
}

func TestPreparedRootedTreeAbortUsesItsCustomEnvelopeDepth(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "bounded")
	envelopeDepth := defaultTreeTraversalLimits().MaximumDepth() + 1
	limits, err := mutationfs.NewTreeTraversalLimits(envelopeDepth, envelopeDepth, 0)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareRootedTreeWithLimits(
		t.Context(),
		capability,
		limits,
		func(writer mutationfs.RootedTreeWriter) error {
			components := make([]string, 0, envelopeDepth)
			for range envelopeDepth {
				components = append(components, "nested")
				if err := writer.CreateDirectory(treePathForTest(t, components...), 0o700); err != nil {
					return err
				}
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("PrepareRootedTreeWithLimits returned error: %v", err)
	}
	stagePath := prepared.stagePath
	if err := prepared.Abort(t.Context()); err != nil {
		t.Fatalf("Abort returned error: %v", err)
	}
	if _, err := os.Lstat(stagePath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("custom-depth stage stat error = %v, want missing", err)
	}
}

func TestPreparedRootedTreeRejectsFinalSymlink(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.MkdirAll(filepath.Join(root, ".agents", "skills"), 0o700); err != nil {
		t.Fatalf("create project ancestry: %v", err)
	}
	referent := filepath.Join(parent, "outside")
	if err := os.Mkdir(referent, 0o700); err != nil {
		t.Fatalf("create symlink referent: %v", err)
	}
	writeTestFile(t, filepath.Join(referent, "keep"), "keep", 0o600)
	if err := os.Symlink(referent, filepath.Join(root, ".agents", "skills", "review")); err != nil {
		t.Fatalf("create final symlink: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/skills/review")
	prepared := prepareRootedTreeForTest(t, capability)
	err := prepared.Commit(context.Background())
	if !hasRootedPathFailureKind(err, rootedpath.FailureFinalSymlink) {
		t.Fatalf("PreparedRootedTree.Commit error = %v, want %s", err, rootedpath.FailureFinalSymlink)
	}
	assertClosedRootedCapability(t, capability)
	assertFileContent(t, filepath.Join(referent, "keep"), "keep")
}

func TestPreparedRootedTreeRejectsStageMutationAfterPreparation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/skills/review")
	prepared := prepareRootedTreeForTest(t, capability)
	writeTestFile(t, filepath.Join(prepared.stagePath, "late"), "late", 0o600)
	err := prepared.Commit(context.Background())
	assertFailure(t, err, failureUncommitted, phaseValidate)
	assertClosedRootedCapability(t, capability)
	if _, statErr := os.Lstat(filepath.Join(root, ".agents")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("mutated stage or ancestry remains: %v", statErr)
	}
}

func TestPreparedRootedTreeRejectsNestedFileContentMutationAfterPreparation(t *testing.T) {
	root, prepared, capability := prepareNestedRootedTreeForMutationTest(t)
	if err := os.WriteFile(
		filepath.Join(prepared.stagePath, "nested", "entry"),
		[]byte("changed"),
		0o600,
	); err != nil {
		t.Fatalf("mutate nested file content: %v", err)
	}

	err := prepared.Commit(t.Context())
	assertFailure(t, err, failureUncommitted, phaseValidate)
	assertClosedRootedCapability(t, capability)
	if _, statErr := os.Lstat(filepath.Join(root, "published")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("content-mutated tree was published: %v", statErr)
	}
}

func TestPreparedRootedTreeRejectsNestedFileModeMutationAfterPreparation(t *testing.T) {
	root, prepared, capability := prepareNestedRootedTreeForMutationTest(t)
	if err := os.Chmod(filepath.Join(prepared.stagePath, "nested", "entry"), 0o400); err != nil {
		t.Fatalf("mutate nested file mode: %v", err)
	}

	err := prepared.Commit(t.Context())
	assertFailure(t, err, failureUncommitted, phaseValidate)
	assertClosedRootedCapability(t, capability)
	if _, statErr := os.Lstat(filepath.Join(root, "published")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("mode-mutated tree was published: %v", statErr)
	}
}

func TestPreparedRootedTreeRejectsNestedFileMetadataMutationAfterPreparation(t *testing.T) {
	root, prepared, capability := prepareNestedRootedTreeForMutationTest(t)
	xattrName := "user.daem.rooted-tree-test"
	if runtime.GOOS == "darwin" {
		xattrName = "com.daem.rooted-tree-test"
	}
	entry := filepath.Join(prepared.stagePath, "nested", "entry")
	if err := unix.Setxattr(entry, xattrName, []byte("changed"), 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
			t.Skipf("extended attributes unavailable: %v", err)
		}
		t.Fatalf("mutate nested file metadata: %v", err)
	}

	err := prepared.Commit(t.Context())
	assertFailure(t, err, failureUncommitted, phaseValidate)
	assertClosedRootedCapability(t, capability)
	if _, statErr := os.Lstat(filepath.Join(root, "published")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("metadata-mutated tree was published: %v", statErr)
	}
}

func TestPreparedRootedTreeRejectsNestedFileMutationAcrossCommitPhases(t *testing.T) {
	tests := []struct {
		name      string
		phase     phase
		wantPhase phase
	}{
		{name: "during file sync", phase: phaseSyncTreeFile, wantPhase: phaseValidate},
		{name: "before mode transition", phase: phaseApplyMode, wantPhase: phaseApplyMode},
		{name: "before publication revalidation", phase: phaseRevalidateEntry, wantPhase: phaseRevalidateEntry},
		{name: "at publication boundary", phase: phaseCommitEntry, wantPhase: phaseCommitEntry},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, prepared, capability := prepareNestedRootedTreeForMutationTest(t)
			entry := filepath.Join(prepared.stagePath, "nested", "entry")
			faults := faultPlan{actions: map[phase]func(){
				test.phase: func() {
					if err := os.WriteFile(entry, []byte("changed"), 0o600); err != nil {
						t.Fatalf("mutate nested file: %v", err)
					}
				},
			}}

			err := commitPreparedRootedTreeWithFaults(t.Context(), prepared, faults)
			assertFailure(t, err, failureUncommitted, test.wantPhase)
			assertClosedRootedCapability(t, capability)
			if _, statErr := os.Lstat(filepath.Join(root, "published")); !errors.Is(statErr, fs.ErrNotExist) {
				t.Fatalf("mutated tree was published: %v", statErr)
			}
		})
	}
}

func TestPreparedRootedTreeCleansRestrictiveStageAfterModeTransitionFailure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "published")
	prepared, err := PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		if err := writer.SetRootMode(0o000); err != nil {
			return err
		}
		if err := writer.CreateDirectory(treePathForTest(t, "nested"), 0o000); err != nil {
			return err
		}
		return writer.WriteFile(treePathForTest(t, "nested", "entry"), 0o000, strings.NewReader("planned"))
	})
	if err != nil {
		t.Fatalf("PrepareRootedTree returned error: %v", err)
	}
	stagePath := prepared.stagePath
	prepared.mu.Lock()
	if err := prepared.applyTreeModesLocked(t.Context()); err != nil {
		prepared.mu.Unlock()
		t.Fatalf("apply restrictive modes: %v", err)
	}
	failure := prepared.failBeforeVisibilityLocked(phaseCommitEntry, errors.New("injected failure"), faultPlan{})
	prepared.mu.Unlock()

	assertFailure(t, failure, failureUncommitted, phaseCommitEntry)
	assertClosedRootedCapability(t, capability)
	if _, statErr := os.Lstat(stagePath); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("restrictive private stage remains after failure cleanup: %v; failure: %v", statErr, failure)
	}
	if _, statErr := os.Lstat(filepath.Join(root, "published")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("failed restrictive tree was published: %v", statErr)
	}
}

func TestPreparedRootedTreeCancellationAfterFinalSyncPreventsPublication(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "published")
	prepared, err := PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		if err := writer.SetRootMode(0o500); err != nil {
			return err
		}
		return writer.WriteFile(treePathForTest(t, "entry"), 0o600, strings.NewReader("planned"))
	})
	if err != nil {
		t.Fatalf("PrepareRootedTree returned error: %v", err)
	}
	ctx := &cancelWhenModeAppliedContext{
		Context: t.Context(),
		path:    prepared.stagePath,
		mode:    0o500,
	}

	err = prepared.Commit(ctx)
	assertFailure(t, err, failureUncommitted, phaseCommitEntry)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PreparedRootedTree.Commit error = %v, want context cancellation", err)
	}
	assertClosedRootedCapability(t, capability)
	if _, statErr := os.Lstat(filepath.Join(root, "published")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("tree was published after cancellation during final synchronization: %v", statErr)
	}
	assertNoPrivateEntries(t, root)
}

func TestPreparedRootedTreeSynchronizesEveryPlannedDescendant(t *testing.T) {
	root, prepared, capability := prepareNestedRootedTreeForMutationTest(t)
	fileSyncs := 0
	directorySyncs := 0
	faults := faultPlan{actions: map[phase]func(){
		phaseSyncTreeFile:      func() { fileSyncs++ },
		phaseSyncTreeDirectory: func() { directorySyncs++ },
	}}

	if err := commitPreparedRootedTreeWithFaults(t.Context(), prepared, faults); err != nil {
		t.Fatalf("commit prepared rooted tree: %v", err)
	}
	assertClosedRootedCapability(t, capability)
	if fileSyncs != 1 {
		t.Fatalf("file sync count = %d, want 1", fileSyncs)
	}
	if directorySyncs != 2 {
		t.Fatalf("directory sync count = %d, want 2", directorySyncs)
	}
	assertFileContent(t, filepath.Join(root, "published", "nested", "entry"), "planned")
}

func TestPreparedRootedTreeValidatesEveryPlannedDescendantMount(t *testing.T) {
	_, prepared, _ := prepareNestedRootedTreeForMutationTest(t)
	t.Cleanup(func() { _ = prepared.Abort(context.Background()) })
	budget, err := newTreeTraversalBudget(prepared.limits)
	if err != nil {
		t.Fatalf("create traversal budget: %v", err)
	}
	validated := 0
	err = verifyPreparedTreeSnapshotDirectory(
		t.Context(),
		prepared.stageFD,
		prepared.stagePath,
		prepared.snapshot.root,
		0,
		func(uintptr) error {
			validated++
			return nil
		},
		budget,
	)
	if err != nil {
		t.Fatalf("verify prepared tree snapshot: %v", err)
	}
	if validated != 2 {
		t.Fatalf("validated descendant mounts = %d, want 2", validated)
	}
}

func TestPrepareRootedTreeRejectsUnrepresentedExtendedAttribute(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "published")
	xattrName := "user.daem.rooted-tree-test"
	if runtime.GOOS == "darwin" {
		xattrName = "com.daem.rooted-tree-test"
	}
	prepared, err := PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		if err := writer.WriteFile(treePathForTest(t, "entry"), 0o600, strings.NewReader("planned")); err != nil {
			return err
		}
		concrete := writer.(*rootedTreeWriterUnix)
		return unix.Setxattr(
			filepath.Join(concrete.prepared.stagePath, "entry"),
			xattrName,
			[]byte("unrepresented"),
			0,
		)
	})
	if prepared != nil {
		t.Fatal("PrepareRootedTree returned a stage with unrepresented metadata")
	}
	assertFailure(t, err, failureUnsupportedGuarantee, phaseValidate)
	assertClosedRootedCapability(t, capability)
	if _, statErr := os.Lstat(filepath.Join(root, "published")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("metadata-bearing tree was published: %v", statErr)
	}
}

func TestPrepareRootedTreeRejectsUnrepresentedModeAndLinkAuthority(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(string, string) error
	}{
		{
			name: "set-user-ID mode",
			mutate: func(entry string, _ string) error {
				return unix.Chmod(entry, 0o4600)
			},
		},
		{
			name: "external hard link",
			mutate: func(entry string, root string) error {
				return os.Link(entry, filepath.Join(root, "outside-link"))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "project")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatalf("create captured root: %v", err)
			}
			captured := captureRootForCommitTest(t, root)
			capability := rootedCapabilityForCommitTest(t, captured, "published")
			prepared, err := PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
				if err := writer.WriteFile(treePathForTest(t, "entry"), 0o600, strings.NewReader("planned")); err != nil {
					return err
				}
				concrete := writer.(*rootedTreeWriterUnix)
				return test.mutate(filepath.Join(concrete.prepared.stagePath, "entry"), root)
			})
			if prepared != nil {
				t.Fatal("PrepareRootedTree returned a stage with unrepresented authority")
			}
			assertFailure(t, err, failureUnsupportedGuarantee, phaseValidate)
			assertClosedRootedCapability(t, capability)
			if _, statErr := os.Lstat(filepath.Join(root, "published")); !errors.Is(statErr, fs.ErrNotExist) {
				t.Fatalf("authority-bearing tree was published: %v", statErr)
			}
		})
	}
}

func TestPreparedRootedTreeRejectsRootReplacementBeforePublish(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/skills/review")
	prepared := prepareRootedTreeForTest(t, capability)
	moved := filepath.Join(parent, "moved")
	if err := os.Rename(root, moved); err != nil {
		t.Fatalf("move captured root: %v", err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create replacement root: %v", err)
	}
	err := prepared.Commit(context.Background())
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("PreparedRootedTree.Commit error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
	assertClosedRootedCapability(t, capability)
	for _, destination := range []string{
		filepath.Join(root, ".agents", "skills", "review"),
		filepath.Join(moved, ".agents", "skills", "review"),
	} {
		if _, statErr := os.Lstat(destination); !errors.Is(statErr, fs.ErrNotExist) {
			t.Fatalf("root replacement published %q: %v", destination, statErr)
		}
	}
}

func TestPreparedRootedTreeRejectsAncestorMoveBeforeVisibility(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(filepath.Join(root, ".agents", "skills"), 0o700); err != nil {
		t.Fatalf("create project ancestry: %v", err)
	}
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatalf("create outside: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/skills/review")
	prepared := prepareRootedTreeForTest(t, capability)
	movedAgents := filepath.Join(outside, "moved-agents")
	faults := faultPlan{actions: map[phase]func(){
		phaseCommitEntry: func() {
			if err := os.Rename(filepath.Join(root, ".agents"), movedAgents); err != nil {
				t.Fatalf("move project ancestor: %v", err)
			}
			if err := os.Mkdir(filepath.Join(root, ".agents"), 0o700); err != nil {
				t.Fatalf("replace project ancestor: %v", err)
			}
		},
	}}
	err := commitPreparedRootedTreeWithFaults(context.Background(), prepared, faults)
	assertFailure(t, err, failureIndeterminateCommit, phaseCommitEntry)
	if !hasRootedPathFailureKind(err, rootedpath.FailureAncestorChanged) {
		t.Fatalf("PreparedRootedTree.Commit error = %v, want %s", err, rootedpath.FailureAncestorChanged)
	}
	assertClosedRootedCapability(t, capability)
	if _, statErr := os.Lstat(filepath.Join(movedAgents, "skills", "review")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("moved ancestor received rooted tree: %v", statErr)
	}
	entries, readErr := os.ReadDir(filepath.Join(movedAgents, "skills"))
	if readErr != nil {
		t.Fatalf("read moved ancestor: %v", readErr)
	}
	privateEntries := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), temporaryPrefix) {
			privateEntries++
		}
	}
	if privateEntries != 1 {
		t.Fatalf("retained private stage count = %d, want 1", privateEntries)
	}
	if _, statErr := os.Lstat(filepath.Join(root, ".agents", "skills")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("replacement ancestor received rooted tree: %v", statErr)
	}
}

func TestPreparedRootedTreeVisibleFailuresRemainIndeterminate(t *testing.T) {
	for _, failedPhase := range []phase{phaseVerifyEntry, phaseSyncParent} {
		t.Run(string(failedPhase), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "project")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			captured := captureRootForCommitTest(t, root)
			capability := rootedCapabilityForCommitTest(t, captured, "published")
			prepared := prepareRootedTreeForTest(t, capability)

			err := commitPreparedRootedTreeWithFaults(
				t.Context(),
				prepared,
				faultAt(failedPhase),
			)
			assertFailure(t, err, failureIndeterminateCommit, failedPhase)
			assertCommitOutcome(
				t,
				outcomeFromError(err),
				mutationfs.CommitOutcomeIndeterminate,
			)
			assertFileContent(t, filepath.Join(root, "published", "entry"), "payload")
		})
	}
}

func prepareRootedTreeForTest(t *testing.T, capability rootedpath.CommitCapability) *PreparedRootedTree {
	t.Helper()
	prepared, err := PrepareRootedTree(context.Background(), capability, func(writer mutationfs.RootedTreeWriter) error {
		return writer.WriteFile(treePathForTest(t, "entry"), 0o600, strings.NewReader("payload"))
	})
	if err != nil {
		t.Fatalf("PrepareRootedTree returned error: %v", err)
	}
	return prepared
}

func prepareNestedRootedTreeForMutationTest(
	t *testing.T,
) (string, *PreparedRootedTree, rootedpath.CommitCapability) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "published")
	prepared, err := PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		if err := writer.CreateDirectory(treePathForTest(t, "nested"), 0o700); err != nil {
			return err
		}
		return writer.WriteFile(
			treePathForTest(t, "nested", "entry"),
			0o600,
			strings.NewReader("planned"),
		)
	})
	if err != nil {
		t.Fatalf("PrepareRootedTree returned error: %v", err)
	}
	return root, prepared, capability
}

func treePathForTest(t *testing.T, components ...string) mutationfs.TreeRelativePath {
	t.Helper()
	path, err := mutationfs.NewTreeRelativePath(components...)
	if err != nil {
		t.Fatalf("NewTreeRelativePath(%q) returned error: %v", components, err)
	}
	return path
}

func treeLimitsForTest(t *testing.T, maximumEntries int, maximumDepth int) mutationfs.TreeTraversalLimits {
	t.Helper()
	limits, err := mutationfs.NewTreeTraversalLimits(maximumEntries, maximumDepth, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return limits
}

type delayedExtraByteReader struct {
	step int
}

type cancelWhenModeAppliedContext struct {
	context.Context
	path string
	mode fs.FileMode
}

func (ctx *cancelWhenModeAppliedContext) Err() error {
	info, err := os.Stat(ctx.path)
	if err == nil && info.Mode().Perm() == ctx.mode {
		return context.Canceled
	}
	return ctx.Context.Err()
}

func (reader *delayedExtraByteReader) Read(payload []byte) (int, error) {
	reader.step++
	switch reader.step {
	case 1:
		return copy(payload, "yes"), nil
	case 2:
		return 0, nil
	default:
		return copy(payload, "x"), nil
	}
}
