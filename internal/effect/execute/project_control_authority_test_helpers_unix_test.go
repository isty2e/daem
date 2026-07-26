//go:build darwin || linux

package execute

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func replaceSelectedRoot(t *testing.T, selectedRoot string, movedRoot string) {
	t.Helper()
	if err := os.Rename(selectedRoot, movedRoot); err != nil {
		t.Fatalf("move selected project root: %v", err)
	}
	if err := os.Mkdir(selectedRoot, 0o700); err != nil {
		t.Fatalf("create replacement project root: %v", err)
	}
}

func movedFixturePath(
	fixture *applyEventFixture,
	movedRoot string,
	selectedPath string,
) string {
	relative, err := filepath.Rel(fixture.root, selectedPath)
	if err != nil {
		panic(err)
	}
	return filepath.Join(movedRoot, relative)
}

func assertActiveRecoveryOperationCount(t *testing.T, recoveryDir string, want int) {
	t.Helper()
	entries, err := os.ReadDir(recoveryDir)
	if errors.Is(err, os.ErrNotExist) && want == 0 {
		return
	}
	if err != nil {
		t.Fatalf("read recovery directory %q: %v", recoveryDir, err)
	}
	if len(entries) != want {
		t.Fatalf("recovery entries under %q = %d, want %d", recoveryDir, len(entries), want)
	}
}

type rootSwapOnJournalRemovalStore struct {
	mutationfs.Store
	journalPath string
	swap        func()
	once        sync.Once
	swaps       int
}

func (filesystem *rootSwapOnJournalRemovalStore) RemoveRootedEntry(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	expected mutationfs.EntryIdentity,
) error {
	if capability != nil {
		path, err := capability.Destination().LexicalPath()
		if err == nil && filepath.Clean(path) == filepath.Clean(filesystem.journalPath) {
			filesystem.once.Do(func() {
				filesystem.swaps++
				filesystem.swap()
			})
		}
	}
	return filesystem.Store.RemoveRootedEntry(ctx, capability, expected)
}

func (filesystem *rootSwapOnJournalRemovalStore) swapCount() int {
	return filesystem.swaps
}

type rootSwapAfterProjectJournalRemovalStore struct {
	mutationfs.Store
	swap  func()
	once  sync.Once
	swaps int
}

func (filesystem *rootSwapAfterProjectJournalRemovalStore) RemoveRootedEntry(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	expected mutationfs.EntryIdentity,
) error {
	if err := filesystem.Store.RemoveRootedEntry(ctx, capability, expected); err != nil {
		return err
	}
	filesystem.once.Do(func() {
		filesystem.swaps++
		filesystem.swap()
	})
	return nil
}

func (filesystem *rootSwapAfterProjectJournalRemovalStore) swapCount() int {
	return filesystem.swaps
}

type rootSwapBeforeRootedTreeStore struct {
	mutationfs.Store
	swap  func()
	once  sync.Once
	swaps int
}

func (filesystem *rootSwapBeforeRootedTreeStore) PrepareRootedTree(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	populate func(mutationfs.RootedTreeWriter) error,
) (mutationfs.PreparedRootedTree, error) {
	filesystem.once.Do(func() {
		filesystem.swaps++
		filesystem.swap()
	})
	return filesystem.Store.PrepareRootedTree(ctx, capability, populate)
}

func (filesystem *rootSwapBeforeRootedTreeStore) swapCount() int {
	return filesystem.swaps
}
