//go:build windows

package commit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func TestWindowsDirectPreparedTreeCommit(t *testing.T) {
	root := t.TempDir()
	staged := filepath.Join(root, "staged")
	destination := filepath.Join(root, "published")
	if err := os.Mkdir(staged, 0o700); err != nil {
		t.Fatal(err)
	}
	opened := openWindowsNativeTestDirectory(t, staged)
	if _, err := applyWindowsCanonicalSecurity(opened.Handle(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
	child, err := NewFileCreate(filepath.Join(staged, "entry.txt"), []byte("payload"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitFile(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	identity, err := CaptureEntryIdentity(t.Context(), staged)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewPreparedTreeCommit(staged, destination, identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitPreparedTree(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(staged); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged tree remains: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(destination, "entry.txt"))
	if err != nil || string(content) != "payload" {
		t.Fatalf("published content = %q, %v", content, err)
	}
}

func TestWindowsPreparedRootedTreeCommitAndSnapshot(t *testing.T) {
	root := t.TempDir()
	destination := root + `\tree`
	capability := acquireWindowsTestCommitCapability(t, destination)
	directory := mustWindowsTreePath(t, "nested")
	file := mustWindowsTreePath(t, "nested", "entry.txt")
	prepared, err := PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		if err := writer.SetRootMode(0o755); err != nil {
			return err
		}
		if err := writer.CreateDirectory(directory, 0o755); err != nil {
			return err
		}
		return writer.WriteFile(file, 0o644, strings.NewReader("payload"))
	})
	if err != nil {
		t.Fatal(err)
	}
	outcome, identity, err := prepared.CommitWithPublishedIdentity(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.State() != mutationfs.CommitOutcomeComplete || identity.Kind() != mutationfs.EntryKindDirectory {
		t.Fatalf("prepared tree outcome = %q identity = %#v", outcome.State(), identity)
	}
	assertWindowsRegularFileSnapshot(t, destination+`\nested\entry.txt`, "payload", 0o644)
	snapshot, err := SnapshotDirectory(t.Context(), destination, 10)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RootMode() != 0o755 || len(snapshot.Entries()) != 1 || snapshot.Entries()[0].Name() != "nested" {
		t.Fatalf("prepared tree root snapshot = mode %04o entries %+v", snapshot.RootMode(), snapshot.Entries())
	}
	assertNoWindowsStorageResidue(t, root)
}

func TestWindowsPreparedRootedTreeAbortAndPopulateFailureRemoveStage(t *testing.T) {
	for _, testCase := range []struct {
		name string
		run  func(*testing.T, rootedpath.CommitCapability, string) error
	}{
		{
			name: "abort",
			run: func(t *testing.T, capability rootedpath.CommitCapability, destination string) error {
				prepared, err := PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
					return writer.WriteFile(mustWindowsTreePath(t, "entry"), 0o600, strings.NewReader("payload"))
				})
				if err != nil {
					return err
				}
				return prepared.Abort(t.Context())
			},
		},
		{
			name: "populate failure",
			run: func(t *testing.T, capability rootedpath.CommitCapability, destination string) error {
				_, err := PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
					if writeErr := writer.WriteFile(mustWindowsTreePath(t, "entry"), 0o600, strings.NewReader("payload")); writeErr != nil {
						return writeErr
					}
					return errors.New("populate failed")
				})
				if err == nil {
					return errors.New("populate failure unexpectedly succeeded")
				}
				return nil
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			destination := root + `\tree`
			if err := testCase.run(t, acquireWindowsTestCommitCapability(t, destination), destination); err != nil {
				t.Fatal(err)
			}
			assertNoWindowsStorageResidue(t, root)
		})
	}
}

func acquireWindowsTestCommitCapability(t *testing.T, path string) rootedpath.CommitCapability {
	t.Helper()
	root, destination, err := rootedpath.CaptureDestination(path)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := root.Acquire(destination)
	closeErr := root.Close()
	if err != nil || closeErr != nil {
		t.Fatal(errors.Join(err, closeErr))
	}
	return capability
}

func mustWindowsTreePath(t *testing.T, components ...string) mutationfs.TreeRelativePath {
	t.Helper()
	path, err := mutationfs.NewTreeRelativePath(components...)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
