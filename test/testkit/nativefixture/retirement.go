package nativefixture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func (store *nativeRecoveryStore) RenameRootedEntry(ctx context.Context, capability rootedpath.CommitCapability, destinationName string, expected mutationfs.EntryIdentity) (mutationfs.CommitOutcome, error) {
	if store.boundary != "retirement" {
		return store.Adapter.RenameRootedEntry(ctx, capability, destinationName, expected)
	}
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return mutationfs.CommitOutcome{}, err
	}
	_, journalErr := os.Stat(filepath.Join(path, "journal.json"))
	outcome, err := store.Adapter.RenameRootedEntry(ctx, capability, destinationName, expected)
	if err != nil || journalErr != nil {
		return outcome, err
	}
	retired := filepath.Join(filepath.Dir(path), destinationName)
	if _, err := os.Stat(filepath.Join(retired, "journal.json")); err != nil {
		return outcome, fmt.Errorf("retired journal was not published: %w", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return outcome, fmt.Errorf("active journal was not retired: %v", err)
	}
	fmt.Fprintln(os.Stderr, "native recovery confirmed retired journal")
	os.Exit(RecoveryExit)
	return outcome, nil
}
