//go:build darwin || linux

package execute

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

type rootSwapOnJournalGCCleanupStore struct {
	mutationfs.Store
	swap  func()
	once  sync.Once
	swaps int
}

func (filesystem *rootSwapOnJournalGCCleanupStore) CleanupRootedEntry(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	expected mutationfs.EntryIdentity,
) (mutationfs.CommitOutcome, error) {
	if capability != nil {
		path, err := capability.Destination().LexicalPath()
		if err == nil && strings.HasPrefix(filepath.Base(path), ".daem-journal-gc-") {
			filesystem.once.Do(func() {
				filesystem.swaps++
				filesystem.swap()
			})
		}
	}
	return filesystem.Store.CleanupRootedEntry(ctx, capability, expected)
}

func (filesystem *rootSwapOnJournalGCCleanupStore) swapCount() int {
	return filesystem.swaps
}

type rootSwapAfterProjectJournalGCCleanupStore struct {
	mutationfs.Store
	swap  func()
	once  sync.Once
	swaps int
}

func (filesystem *rootSwapAfterProjectJournalGCCleanupStore) CleanupRootedEntry(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	expected mutationfs.EntryIdentity,
) (mutationfs.CommitOutcome, error) {
	isJournalGC := false
	if capability != nil {
		path, pathErr := capability.Destination().LexicalPath()
		isJournalGC = pathErr == nil &&
			strings.HasPrefix(filepath.Base(path), ".daem-journal-gc-")
	}
	outcome, err := filesystem.Store.CleanupRootedEntry(ctx, capability, expected)
	if err != nil {
		return outcome, err
	}
	if isJournalGC {
		filesystem.once.Do(func() {
			filesystem.swaps++
			filesystem.swap()
		})
	}
	return outcome, nil
}

func (filesystem *rootSwapAfterProjectJournalGCCleanupStore) swapCount() int {
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
