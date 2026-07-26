//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

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

func TestPreparedRootedTreeReportsAncestorMoveAtVisibility(t *testing.T) {
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
	assertFailure(t, err, failureIndeterminateCommit, phaseVerifyEntry)
	if !hasRootedPathFailureKind(err, rootedpath.FailureAncestorChanged) {
		t.Fatalf("PreparedRootedTree.Commit error = %v, want %s", err, rootedpath.FailureAncestorChanged)
	}
	assertClosedRootedCapability(t, capability)
	assertFileContent(t, filepath.Join(movedAgents, "skills", "review", "entry"), "payload")
	if _, statErr := os.Lstat(filepath.Join(root, ".agents", "skills")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("replacement ancestor received rooted tree: %v", statErr)
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

func treePathForTest(t *testing.T, components ...string) mutationfs.TreeRelativePath {
	t.Helper()
	path, err := mutationfs.NewTreeRelativePath(components...)
	if err != nil {
		t.Fatalf("NewTreeRelativePath(%q) returned error: %v", components, err)
	}
	return path
}
