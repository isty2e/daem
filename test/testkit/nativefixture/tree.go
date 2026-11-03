package nativefixture

import (
	"context"
	"errors"
	"fmt"
	"os"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func (store *nativeRecoveryStore) PrepareRootedTree(ctx context.Context, capability rootedpath.CommitCapability, populate func(mutationfs.RootedTreeWriter) error) (mutationfs.PreparedRootedTree, error) {
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return nil, err
	}
	prepared, err := store.Adapter.PrepareRootedTree(ctx, capability, populate)
	if err != nil || path != store.host {
		return prepared, err
	}
	return nativeRecoveryPreparedTree{PreparedRootedTree: prepared, store: store, path: path}, nil
}

type nativeRecoveryPreparedTree struct {
	mutationfs.PreparedRootedTree
	store *nativeRecoveryStore
	path  string
}

func (prepared nativeRecoveryPreparedTree) Commit(ctx context.Context) error {
	if err := prepared.PreparedRootedTree.Commit(ctx); err != nil {
		return err
	}
	return prepared.afterPublication()
}

func (prepared nativeRecoveryPreparedTree) CommitWithOutcome(ctx context.Context) (mutationfs.CommitOutcome, error) {
	outcome, err := prepared.PreparedRootedTree.CommitWithOutcome(ctx)
	if err != nil {
		return outcome, err
	}
	return outcome, prepared.afterPublication()
}

func (prepared nativeRecoveryPreparedTree) afterPublication() error {
	if info, err := os.Stat(prepared.path); err != nil || !info.IsDir() {
		return fmt.Errorf("tree publication was not confirmed: %v", err)
	}
	fmt.Fprintln(os.Stderr, RecoveryReceipt(prepared.path))
	store := prepared.store
	if store.boundary == "host" {
		os.Exit(RecoveryExit)
	}
	if !store.failed && store.boundary == "return-error" {
		store.failed = true
		return errors.New("native recovery fixture: returned tree publication error")
	}
	if !store.failed && store.boundary == "cancel" {
		store.failed = true
		store.cancel()
		return context.Canceled
	}
	return nil
}

func (store *nativeRecoveryStore) RemoveRootedEntryWithResidue(ctx context.Context, capability rootedpath.CommitCapability, expected mutationfs.EntryIdentity, names mutationfs.LogicalRemovalNames, limits mutationfs.TreeTraversalLimits) (mutationfs.CommitOutcome, error) {
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return mutationfs.CommitOutcome{}, err
	}
	outcome, err := store.Adapter.RemoveRootedEntryWithResidue(ctx, capability, expected, names, limits)
	if err == nil && path == store.host && (store.boundary == "skill-removal" || store.boundary == "host-removal") {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return outcome, fmt.Errorf("ordinary retirement was not confirmed: %v", err)
		}
		if store.boundary == "host-removal" {
			fmt.Fprintln(os.Stderr, "native recovery confirmed document retirement")
		} else {
			fmt.Fprintln(os.Stderr, "native recovery confirmed ordinary retirement")
		}
		os.Exit(RecoveryExit)
	}
	return outcome, err
}
