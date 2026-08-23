//go:build windows

package commit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestWindowsLogicalRemovalAndRootedRename(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.json")
	create, err := NewFileCreate(source, []byte("payload"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitFile(t.Context(), create); err != nil {
		t.Fatal(err)
	}
	capability := acquireWindowsTestCommitCapability(t, source)
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		_ = capability.Close()
		t.Fatal(err)
	}
	rename, err := NewRootedEntryRename(capability, "renamed.json", expected)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := CommitRootedEntryRename(t.Context(), rename)
	if err != nil || outcome.State() != mutationfs.CommitOutcomeComplete {
		t.Fatalf("rooted rename = %q, %v", outcome.State(), err)
	}
	renamed := filepath.Join(root, "renamed.json")
	assertWindowsRegularFileSnapshot(t, renamed, "payload", 0o600)
	if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("renamed source = %v, want missing", err)
	}

	expected, err = CaptureEntryIdentity(t.Context(), renamed)
	if err != nil {
		t.Fatal(err)
	}
	removal, err := NewLogicalRemoval(renamed, expected)
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitLogicalRemoval(t.Context(), removal); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(renamed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed destination = %v, want missing", err)
	}
	assertNoWindowsStorageResidue(t, root)
}

func TestWindowsRootedDirectoryCleanupRemovesExactPreparedTree(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "tree")
	prepared, err := PrepareRootedTree(
		t.Context(),
		acquireWindowsTestCommitCapability(t, destination),
		func(writer mutationfs.RootedTreeWriter) error {
			if err := writer.SetRootMode(0o700); err != nil {
				return err
			}
			if err := writer.CreateDirectory(mustWindowsTreePath(t, "nested"), 0o700); err != nil {
				return err
			}
			return writer.WriteFile(mustWindowsTreePath(t, "nested", "entry"), 0o600, strings.NewReader("payload"))
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, identity, err := prepared.CommitWithPublishedIdentity(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	limits, err := mutationfs.NewTreeTraversalLimits(10, 4, 1024)
	if err != nil {
		t.Fatal(err)
	}
	cleanup, err := NewRootedEntryCleanup(acquireWindowsTestCommitCapability(t, destination), identity, limits)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := CommitRootedEntryCleanup(t.Context(), cleanup)
	if err != nil || outcome.State() != mutationfs.CommitOutcomeComplete {
		t.Fatalf("rooted cleanup = %q, %v", outcome.State(), err)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleaned tree = %v, want missing", err)
	}
}

func TestWindowsPrepareCommitParentAndAncestorCleanup(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "one", "two", "state.json")
	if err := PrepareCommitParent(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	request, err := NewFileCreate(target, []byte("payload"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitFile(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	assertWindowsRegularFileSnapshot(t, target, "payload", 0o600)

	rollbackRoot := t.TempDir()
	rollbackTarget := filepath.Join(rollbackRoot, "one", "two", "state.json")
	var cleanup AncestorCleanup
	if err := cleanup.PrepareParent(t.Context(), rollbackTarget); err != nil {
		t.Fatal(err)
	}
	if err := cleanup.RemoveEmpty(t.Context()); err != nil {
		t.Fatal(err)
	}
	cleanup.Close()
	if _, err := os.Lstat(filepath.Join(rollbackRoot, "one")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rolled-back ancestor = %v, want missing", err)
	}
}

func TestWindowsConfirmRootedEntryAbsentAndRejectPresent(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	outcome, err := ConfirmRootedEntryAbsentWithOutcome(
		t.Context(),
		acquireWindowsTestCommitCapability(t, missing),
	)
	if err != nil || outcome.State() != mutationfs.CommitOutcomeComplete {
		t.Fatalf("confirm missing = %q, %v", outcome.State(), err)
	}

	present := filepath.Join(root, "present")
	request, err := NewFileCreate(present, []byte("payload"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitFile(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	outcome, err = ConfirmRootedEntryAbsentWithOutcome(
		t.Context(),
		acquireWindowsTestCommitCapability(t, present),
	)
	if err == nil || outcome.State() != mutationfs.CommitOutcomeUncommitted {
		t.Fatalf("confirm present = %q, %v, want uncommitted", outcome.State(), err)
	}
}

func TestWindowsDestinationAnchorRejectsMovedAncestorBinding(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "state.json")
	capability := acquireWindowsTestCommitCapability(t, target)
	anchor, err := openWindowsDestinationAnchor(t.Context(), capability, true)
	if err != nil {
		t.Fatal(err)
	}
	defer anchor.close()

	moved := filepath.Join(root, "moved")
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := anchor.revalidate(t.Context()); err == nil {
		t.Fatal("destination anchor accepted a replaced ancestor binding")
	}
}

func TestWindowsAncestorCleanupRejectsMovedBinding(t *testing.T) {
	root := t.TempDir()
	created := filepath.Join(root, "created")
	target := filepath.Join(created, "state.json")
	var cleanup AncestorCleanup
	if err := cleanup.PrepareParent(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(root, "moved")
	if err := os.Rename(created, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(created, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := cleanup.RemoveEmpty(t.Context()); err == nil {
		t.Fatal("ancestor cleanup removed a replaced name binding")
	}
	cleanup.Close()
	for _, path := range []string{created, moved} {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			t.Fatalf("preserved directory %q = %#v, %v", path, info, err)
		}
	}
}

func TestWindowsReadRootedSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target"), []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink("target", link); err != nil {
		if windowsNativeFeatureUnavailable(err) {
			t.Skipf("symbolic links unavailable: %v", err)
		}
		t.Fatal(err)
	}
	capability := acquireWindowsTestCommitCapability(t, link)
	defer capability.Close()
	target, identity, err := ReadRootedSymlinkTarget(t.Context(), capability)
	if err != nil {
		t.Fatal(err)
	}
	if target != "target" || identity.Kind() != mutationfs.EntryKindSymlink {
		t.Fatalf("symlink target = %q identity = %#v", target, identity)
	}
}
