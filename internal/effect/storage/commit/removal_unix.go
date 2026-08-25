//go:build darwin || linux

package commit

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"golang.org/x/sys/unix"
)

// CommitLogicalRemoval durably retires the active name, then removes its
// private tombstone.
func CommitLogicalRemoval(ctx context.Context, request LogicalRemoval) error {
	_, err := CommitLogicalRemovalWithOutcome(ctx, request)
	return err
}

// CommitLogicalRemovalWithOutcome is the outcome-bearing rooted-removal
// boundary used by journal-authorized execution.
func CommitLogicalRemovalWithOutcome(
	ctx context.Context,
	request LogicalRemoval,
) (mutationfs.CommitOutcome, error) {
	err := commitLogicalRemovalWithFaults(ctx, request, faultPlan{})
	return outcomeFromError(err), err
}

func commitLogicalRemovalWithFaults(ctx context.Context, request LogicalRemoval, faults faultPlan) error {
	if request.capability != nil {
		defer request.capability.Close()
	}
	if err := validateRemovalRequest(request); err != nil {
		return newFailure(failureUncommitted, phaseValidate, request.path, err)
	}
	if err := faults.check(ctx, phaseValidate); err != nil {
		return newFailure(failureUncommitted, phaseValidate, request.path, err)
	}
	anchor, err := openCommitParent(request.path, request.capability, false)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return failureBeforeVisibility(phaseValidate, request.path, err)
	}
	if err := validateRemovalEntry(anchor, request); err != nil {
		return failureBeforeVisibility(phaseValidate, request.path, err)
	}
	if err := anchor.verifyChain(); err != nil {
		return failureBeforeVisibility(phaseValidate, request.path, err)
	}

	tombstoneName := tombstonePrefix
	cleanupName := ""
	if request.names != nil {
		tombstoneName = request.names.Residue()
		cleanupName = request.names.Cleanup()
		for _, name := range []string{tombstoneName, cleanupName} {
			exists, err := entryExists(anchor.parentFD(), name)
			if err != nil {
				return failureBeforeVisibility(phaseCommitTombstone, request.path, err)
			}
			if exists {
				return failureBeforeVisibility(
					phaseCommitTombstone,
					request.path,
					fmt.Errorf("journal-authorized removal name %q is already occupied", name),
				)
			}
		}
	} else {
		var err error
		tombstoneName, err = unusedSiblingName(anchor.parentFD(), tombstonePrefix)
		if err != nil {
			return failureBeforeVisibility(phaseCommitTombstone, request.path, err)
		}
	}
	tombstonePath := filepath.Join(filepath.Dir(request.path), tombstoneName)
	var tombstoneIdentity EntryIdentity
	err = faults.run(ctx, phaseRevalidateEntry, func() error {
		_, _, err := anchor.requireExpected(anchor.base, request.path, request.expected)
		return err
	})
	if err != nil {
		return failureBeforeVisibility(phaseRevalidateEntry, request.path, err)
	}
	if err := anchor.verifyChain(); err != nil {
		return failureBeforeVisibility(phaseValidate, request.path, err)
	}
	err = faults.run(ctx, phaseCommitTombstone, func() error {
		return renameNoReplace(anchor.parentFD(), anchor.base, anchor.parentFD(), tombstoneName)
	})
	if err != nil {
		return failureBeforeVisibility(phaseCommitTombstone, request.path, err)
	}
	err = faults.run(ctx, phaseVerifyEntry, func() error {
		if err := anchor.verifyChain(); err != nil {
			return err
		}
		var moved EntryIdentity
		moved, _, observeErr := anchor.observe(tombstoneName, tombstonePath)
		if observeErr != nil {
			return observeErr
		}
		if !request.expected.sameObject(moved) {
			return fmt.Errorf("tombstone identity does not match removed entry")
		}
		tombstoneIdentity = moved
		return nil
	})
	if err != nil {
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err, tombstonePath)
	}
	err = faults.run(ctx, phaseSyncParent, func() error { return syncDirectory(anchor.parentFD()) })
	if err != nil {
		return newFailure(failureIndeterminateCommit, phaseSyncParent, request.path, err, tombstonePath)
	}
	if err := anchor.verifyChain(); err != nil {
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err, tombstonePath)
	}

	cleanupEntryName := tombstoneName
	cleanupPath := tombstonePath
	cleanupIdentity := tombstoneIdentity
	if cleanupName != "" {
		cleanupPath = filepath.Join(filepath.Dir(request.path), cleanupName)
		err = faults.run(ctx, phasePromoteCleanup, func() error {
			return renameNoReplace(anchor.parentFD(), tombstoneName, anchor.parentFD(), cleanupName)
		})
		if err != nil {
			return newFailure(failureRetainedResidue, phasePromoteCleanup, request.path, err, tombstonePath)
		}
		err = faults.run(ctx, phaseVerifyEntry, func() error {
			if err := anchor.verifyChain(); err != nil {
				return err
			}
			moved, _, observeErr := anchor.observe(cleanupName, cleanupPath)
			if observeErr != nil {
				return observeErr
			}
			if !tombstoneIdentity.sameObject(moved) {
				return fmt.Errorf("cleanup-stage identity does not match validated residue")
			}
			cleanupIdentity = moved
			return nil
		})
		if err != nil {
			return newFailure(
				failureIndeterminateCommit,
				phaseVerifyEntry,
				request.path,
				err,
				tombstonePath,
				cleanupPath,
			)
		}
		err = faults.run(ctx, phaseSyncParent, func() error { return syncDirectory(anchor.parentFD()) })
		if err != nil {
			return newFailure(failureIndeterminateCommit, phaseSyncParent, request.path, err, cleanupPath)
		}
		cleanupEntryName = cleanupName
	}

	err = faults.run(ctx, phaseCleanupTombstone, func() error {
		limits := defaultTreeTraversalLimits()
		if request.names != nil {
			limits = request.limits
		}
		return removeEntryAtWithFaults(
			ctx,
			anchor.parentFD(),
			cleanupEntryName,
			cleanupPath,
			cleanupIdentity,
			limits,
			faults,
			anchor.verifyChain,
		)
	})
	if err != nil {
		return newFailure(failureRetainedResidue, phaseCleanupTombstone, request.path, err, cleanupPath)
	}
	err = faults.run(ctx, phaseSyncCleanupParent, func() error { return syncDirectory(anchor.parentFD()) })
	if err != nil {
		return newFailure(failureRetainedResidue, phaseSyncCleanupParent, request.path, err, cleanupPath)
	}
	if err := anchor.verifyChain(); err != nil {
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err)
	}
	return nil
}

func validateRemovalRequest(request LogicalRemoval) error {
	if err := validateCommitPath(request.path); err != nil {
		return err
	}
	if !request.expected.valid() || request.expected.path != request.path {
		return fmt.Errorf("logical removal requires matching expected identity")
	}
	if err := validateRootedCapability(request.path, request.capability); err != nil {
		return err
	}
	if request.capability != nil && request.expected.kind == entryKindSymlink {
		return rootedFinalSymlinkFailure(request.path)
	}
	if request.names != nil {
		if err := request.limits.Validate(); err != nil {
			return fmt.Errorf("journal-authorized removal traversal limits: %w", err)
		}
		defaults := defaultTreeTraversalLimits()
		if request.limits.MaximumEntries() > defaults.MaximumEntries() ||
			request.limits.MaximumDepth() > defaults.MaximumDepth() ||
			request.limits.MaximumBytes() > defaults.MaximumBytes() {
			return fmt.Errorf("journal-authorized removal traversal limits exceed storage maximum")
		}
	}
	switch request.expected.kind {
	case entryKindRegular, entryKindDirectory, entryKindSymlink:
		return nil
	default:
		return fmt.Errorf("logical removal has unsupported entry kind")
	}
}

func validateRemovalEntry(anchor *anchoredParent, request LogicalRemoval) error {
	_, stat, err := anchor.requireExpected(anchor.base, request.path, request.expected)
	if err != nil {
		return err
	}
	if err := validateOwnedStat(request.path, &stat); err != nil {
		return err
	}
	if request.expected.kind == entryKindSymlink {
		return nil
	}
	fd, _, err := anchor.openExpected(anchor.base, request.path, request.expected)
	if fd >= 0 {
		_ = unix.Close(fd)
	}
	return err
}

func unusedSiblingName(parentFD int, prefix string) (string, error) {
	for range 64 {
		name, err := randomSiblingName(prefix)
		if err != nil {
			return "", err
		}
		exists, err := entryExists(parentFD, name)
		if err != nil {
			return "", err
		}
		if !exists {
			return name, nil
		}
	}
	return "", fmt.Errorf("could not allocate a collision-free tombstone name")
}

func removeEntryAtWithFaults(
	ctx context.Context,
	parentFD int,
	name string,
	path string,
	expected EntryIdentity,
	limits mutationfs.TreeTraversalLimits,
	faults faultPlan,
	validateNamespace func() error,
) error {
	_, err := removeEntryAtWithFaultsAndOutcome(
		ctx,
		parentFD,
		name,
		path,
		expected,
		limits,
		faults,
		validateNamespace,
	)
	return err
}

func removeEntryAtWithFaultsAndOutcome(
	ctx context.Context,
	parentFD int,
	name string,
	path string,
	expected EntryIdentity,
	limits mutationfs.TreeTraversalLimits,
	faults faultPlan,
	validateNamespace func() error,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	snapshot, authority, captureChanged, err := captureRemovalTreeSnapshot(
		ctx,
		parentFD,
		name,
		path,
		expected,
		limits,
		validateNamespace,
		faults,
	)
	if err != nil {
		return captureChanged, err
	}
	removeChanged, err := removeRemovalTreeSnapshot(
		ctx,
		parentFD,
		path,
		&snapshot,
		authority,
		limits,
		faults,
	)
	return captureChanged || removeChanged, err
}

func removeRemovalTreeSnapshot(
	ctx context.Context,
	parentFD int,
	path string,
	entry *removalTreeSnapshotEntry,
	authority removalEffectAuthority,
	limits mutationfs.TreeTraversalLimits,
	faults faultPlan,
) (bool, error) {
	if err := faults.check(ctx, phaseRevalidateCleanup); err != nil {
		return false, atPhase(phaseRevalidateCleanup, err)
	}
	if err := verifyRemovalTreeSnapshot(
		ctx,
		parentFD,
		path,
		*entry,
		authority,
		limits,
		faults,
	); err != nil {
		return false, atPhase(phaseRevalidateCleanup, err)
	}
	if err := authority.verify(nil); err != nil {
		return false, atPhase(phaseRevalidateCleanup, err)
	}
	budget, err := newTreeTraversalBudget(limits)
	if err != nil {
		return false, err
	}
	state := &removalTreeEffectState{}
	err = removeRemovalSnapshotEntry(
		ctx,
		parentFD,
		path,
		entry,
		authority,
		nil,
		0,
		budget,
		faults,
		state,
	)
	return state.changed, err
}

func removeRemovalSnapshotEntry(
	ctx context.Context,
	parentFD int,
	path string,
	entry *removalTreeSnapshotEntry,
	authority removalEffectAuthority,
	ancestors []removalDirectoryBinding,
	depth int,
	budget *treeTraversalBudget,
	faults faultPlan,
	state *removalTreeEffectState,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch entry.identity.kind {
	case entryKindRegular:
		return removeRemovalSnapshotFile(
			ctx,
			parentFD,
			path,
			*entry,
			authority,
			ancestors,
			budget,
			faults,
			state,
		)
	case entryKindSymlink:
		return removeRemovalSnapshotSymlink(
			ctx,
			parentFD,
			path,
			*entry,
			authority,
			ancestors,
			faults,
			state,
		)
	case entryKindDirectory:
		return removeRemovalSnapshotDirectory(
			ctx,
			parentFD,
			path,
			entry,
			authority,
			ancestors,
			depth,
			budget,
			faults,
			state,
		)
	default:
		return unsupported(fmt.Sprintf("cannot safely remove special entry %q", path), nil)
	}
}

func removeRemovalSnapshotFile(
	ctx context.Context,
	parentFD int,
	path string,
	entry removalTreeSnapshotEntry,
	authority removalEffectAuthority,
	ancestors []removalDirectoryBinding,
	budget *treeTraversalBudget,
	faults faultPlan,
	state *removalTreeEffectState,
) error {
	if err := budget.admitBytes(entry.size); err != nil {
		return err
	}
	if err := faults.check(ctx, phaseCleanupEntry); err != nil {
		return err
	}
	if err := authority.verify(ancestors); err != nil {
		return err
	}
	if err := verifyRemovalSnapshotEntryAt(parentFD, path, entry, authority); err != nil {
		return err
	}
	if err := unix.Unlinkat(parentFD, entry.name, 0); err != nil {
		return err
	}
	state.changed = true
	return nil
}

func removeRemovalSnapshotSymlink(
	ctx context.Context,
	parentFD int,
	path string,
	entry removalTreeSnapshotEntry,
	authority removalEffectAuthority,
	ancestors []removalDirectoryBinding,
	faults faultPlan,
	state *removalTreeEffectState,
) error {
	if err := faults.check(ctx, phaseCleanupEntry); err != nil {
		return err
	}
	if err := authority.verify(ancestors); err != nil {
		return err
	}
	if err := verifyRemovalSnapshotEntryAt(parentFD, path, entry, authority); err != nil {
		return err
	}
	if err := unix.Unlinkat(parentFD, entry.name, 0); err != nil {
		return err
	}
	state.changed = true
	return nil
}

func removeRemovalSnapshotDirectory(
	ctx context.Context,
	parentFD int,
	path string,
	entry *removalTreeSnapshotEntry,
	authority removalEffectAuthority,
	ancestors []removalDirectoryBinding,
	depth int,
	budget *treeTraversalBudget,
	faults faultPlan,
	state *removalTreeEffectState,
) error {
	if err := budget.admitDepth(depth); err != nil {
		return err
	}
	fd, err := openExpectedAt(parentFD, entry.name, path, entry.identity.entryAt(path))
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := authority.validateDirectoryHandle(fd); err != nil {
		return err
	}
	bindings := append(ancestors, removalDirectoryBinding{
		parentFD:    parentFD,
		directoryFD: fd,
		path:        path,
		entry:       entry,
	})
	if err := prepareOpenedRemovalDirectory(
		ctx,
		bindings,
		authority,
		faults,
		state,
	); err != nil {
		return err
	}
	if err := authority.verify(bindings); err != nil {
		return err
	}
	if err := budget.admitEntries(len(entry.children)); err != nil {
		return err
	}
	if err := verifyRemovalDirectorySnapshot(ctx, fd, path, *entry, authority, faults); err != nil {
		return err
	}
	for index := range entry.children {
		if err := faults.checkCleanupChild(ctx); err != nil {
			return err
		}
		child := &entry.children[index]
		childPath := filepath.Join(path, child.name)
		if err := removeRemovalSnapshotEntry(
			ctx,
			fd,
			childPath,
			child,
			authority,
			bindings,
			depth+1,
			budget,
			faults,
			state,
		); err != nil {
			return err
		}
		if err := refreshRemovalDirectoryBinding(parentFD, path, fd, entry, authority); err != nil {
			return err
		}
		if err := authority.verify(bindings); err != nil {
			return err
		}
	}
	if err := faults.check(ctx, phaseCleanupEntry); err != nil {
		return err
	}
	if err := authority.verify(bindings); err != nil {
		return err
	}
	names, err := readDirectoryNames(ctx, fd, path, 0)
	if err != nil {
		return err
	}
	faults.finishCleanupDirectoryRead()
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(names) != 0 {
		return fmt.Errorf("directory entries changed before cleanup at %q", path)
	}
	if err := authority.verify(bindings); err != nil {
		return err
	}
	if err := unix.Unlinkat(parentFD, entry.name, unix.AT_REMOVEDIR); err != nil {
		return err
	}
	state.changed = true
	return nil
}

func prepareOpenedRemovalDirectory(
	ctx context.Context,
	bindings []removalDirectoryBinding,
	authority removalEffectAuthority,
	faults faultPlan,
	state *removalTreeEffectState,
) error {
	if len(bindings) == 0 {
		return fmt.Errorf("removal directory binding is required")
	}
	binding := bindings[len(bindings)-1]
	if binding.entry == nil {
		return fmt.Errorf("removal directory binding is uninitialized")
	}
	if binding.entry.mode&0o700 == 0o700 {
		return authority.verify(bindings)
	}
	if state == nil {
		return fmt.Errorf("removal tree effect state is required")
	}
	if err := faults.check(ctx, phaseApplyMode); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	if err := authority.verify(bindings); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	mode := uint32(binding.entry.mode.Perm()) | 0o700
	if err := unix.Fchmod(binding.directoryFD, mode); err != nil {
		return atPhase(phaseApplyMode, fmt.Errorf("make retired directory removable at %q: %w", binding.path, err))
	}
	state.changed = true
	var stat unix.Stat_t
	if err := unix.Fstat(binding.directoryFD, &stat); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	current := identityFromStat(binding.path, &stat)
	if !binding.entry.identity.sameObject(current) || current.kind != entryKindDirectory {
		return atPhase(phaseApplyMode, fmt.Errorf("retired directory identity changed while preparing cleanup at %q", binding.path))
	}
	if err := validateOwnedStat(binding.path, &stat); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	if fs.FileMode(stat.Mode).Perm() != fs.FileMode(mode).Perm() {
		return atPhase(
			phaseApplyMode,
			unsupported(fmt.Sprintf("retired directory %q did not retain private cleanup mode", binding.path), nil),
		)
	}
	binding.entry.identity = newRemovalEntryIdentity(current)
	binding.entry.mode = fs.FileMode(stat.Mode).Perm()
	if err := authority.verify(bindings); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	return nil
}
