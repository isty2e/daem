//go:build darwin || linux

package commit

import (
	"context"
	"fmt"
	"path/filepath"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
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
			request.capability,
			limits,
			faults,
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
		if request.limits.MaximumEntries() > defaultTreeTraversalMaximumEntries ||
			request.limits.MaximumDepth() > defaultTreeTraversalMaximumDepth ||
			request.limits.MaximumBytes() > defaultTreeTraversalMaximumBytes {
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

func removeEntryAt(
	ctx context.Context,
	parentFD int,
	name string,
	path string,
	expected EntryIdentity,
	capability rootedpath.CommitCapability,
) error {
	return removeEntryAtWithFaults(
		ctx,
		parentFD,
		name,
		path,
		expected,
		capability,
		defaultTreeTraversalLimits(),
		faultPlan{},
	)
}

func removeEntryAtWithFaults(
	ctx context.Context,
	parentFD int,
	name string,
	path string,
	expected EntryIdentity,
	capability rootedpath.CommitCapability,
	limits mutationfs.TreeTraversalLimits,
	faults faultPlan,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	observed, stat, err := observeAt(parentFD, name, path)
	if err != nil {
		return err
	}
	if !expected.sameEntry(observed) {
		return fmt.Errorf("entry identity changed before cleanup at %q", path)
	}
	if err := validateOwnedStat(path, &stat); err != nil {
		return err
	}
	if observed.kind == entryKindRegular {
		budget, err := newTreeTraversalBudget(limits)
		if err != nil {
			return err
		}
		if err := budget.admitBytes(stat.Size); err != nil {
			return err
		}
	}
	if observed.kind == entryKindDirectory {
		observed, err = prepareRemovalDirectory(parentFD, name, path, observed, stat)
		if err != nil {
			return err
		}
		fd, err := openExpectedAt(parentFD, name, path, observed)
		if err != nil {
			return err
		}
		validateMount, err := removalMountValidator(capability, fd)
		if err != nil {
			_ = unix.Close(fd)
			return err
		}
		preflightBudget, err := newTreeTraversalBudget(limits)
		if err == nil {
			err = validateDirectoryEntries(
				ctx,
				validateMount,
				fd,
				path,
				0,
				preflightBudget,
			)
		}
		if err == nil {
			cleanupBudget, budgetErr := newTreeTraversalBudget(limits)
			if budgetErr != nil {
				err = budgetErr
			} else {
				err = removeDirectoryContentsWithFaults(
					ctx,
					validateMount,
					fd,
					path,
					0,
					cleanupBudget,
					faults,
				)
			}
		}
		_ = unix.Close(fd)
		if err != nil {
			return err
		}
		current, _, err := observeAt(parentFD, name, path)
		if err != nil {
			return err
		}
		if !observed.sameObject(current) {
			return fmt.Errorf("directory identity changed before cleanup at %q", path)
		}
	}
	if err := faults.check(ctx, phaseCleanupEntry); err != nil {
		return err
	}
	return unix.Unlinkat(parentFD, name, removalFlags(observed.kind))
}

func removeDirectoryContentsWithFaults(
	ctx context.Context,
	validateMount func(uintptr) error,
	directoryFD int,
	path string,
	depth int,
	budget *treeTraversalBudget,
	faults faultPlan,
) error {
	if validateMount == nil {
		return fmt.Errorf("directory mount validator is required")
	}
	if err := validateMount(uintptr(directoryFD)); err != nil {
		return err
	}
	if err := budget.admitDepth(depth); err != nil {
		return err
	}
	names, err := readDirectoryNames(
		ctx,
		directoryFD,
		path,
		budget.remainingEntries(),
	)
	if err != nil {
		return err
	}
	if err := budget.admitEntries(len(names)); err != nil {
		return err
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		entryPath := filepath.Join(path, name)
		identity, stat, err := observeAt(directoryFD, name, entryPath)
		if err != nil {
			return err
		}
		if err := validateOwnedStat(entryPath, &stat); err != nil {
			return err
		}
		switch identity.kind {
		case entryKindDirectory:
			identity, err = prepareRemovalDirectory(directoryFD, name, entryPath, identity, stat)
			if err != nil {
				return err
			}
			fd, err := openExpectedAt(directoryFD, name, entryPath, identity)
			if err != nil {
				return err
			}
			if err := validateMount(uintptr(fd)); err != nil {
				_ = unix.Close(fd)
				return err
			}
			err = removeDirectoryContentsWithFaults(
				ctx,
				validateMount,
				fd,
				entryPath,
				depth+1,
				budget,
				faults,
			)
			_ = unix.Close(fd)
			if err != nil {
				return err
			}
			current, _, err := observeAt(directoryFD, name, entryPath)
			if err != nil {
				return err
			}
			if !identity.sameObject(current) {
				return fmt.Errorf("directory identity changed before cleanup at %q", entryPath)
			}
			if err := faults.check(ctx, phaseCleanupEntry); err != nil {
				return err
			}
			if err := unix.Unlinkat(directoryFD, name, unix.AT_REMOVEDIR); err != nil {
				return err
			}
		case entryKindRegular:
			if err := budget.admitBytes(stat.Size); err != nil {
				return err
			}
			fd, err := openExpectedAt(directoryFD, name, entryPath, identity)
			if err != nil {
				return err
			}
			if err := validateMount(uintptr(fd)); err != nil {
				_ = unix.Close(fd)
				return err
			}
			if err := verifySnapshotEntry(
				directoryFD,
				name,
				fd,
				entryPath,
				identity,
			); err != nil {
				_ = unix.Close(fd)
				return err
			}
			if err := unix.Close(fd); err != nil {
				return err
			}
			current, _, err := observeAt(directoryFD, name, entryPath)
			if err != nil {
				return err
			}
			if !identity.sameEntry(current) {
				return fmt.Errorf("entry identity changed before cleanup at %q", entryPath)
			}
			if err := faults.check(ctx, phaseCleanupEntry); err != nil {
				return err
			}
			if err := unix.Unlinkat(directoryFD, name, 0); err != nil {
				return err
			}
		case entryKindSymlink:
			current, _, err := observeAt(directoryFD, name, entryPath)
			if err != nil {
				return err
			}
			if !identity.sameEntry(current) {
				return fmt.Errorf("entry identity changed before cleanup at %q", entryPath)
			}
			if err := faults.check(ctx, phaseCleanupEntry); err != nil {
				return err
			}
			if err := unix.Unlinkat(directoryFD, name, 0); err != nil {
				return err
			}
		default:
			return unsupported(fmt.Sprintf("cannot safely remove special entry %q", entryPath), nil)
		}
	}
	return nil
}

func removalMountValidator(
	capability rootedpath.CommitCapability,
	rootFD int,
) (func(uintptr) error, error) {
	if capability != nil {
		if err := capability.ValidateDirectoryHandle(uintptr(rootFD)); err != nil {
			return nil, err
		}
		return capability.ValidateDirectoryHandle, nil
	}
	boundary, err := rootedpath.CaptureDirectoryMountBoundary(uintptr(rootFD))
	if err != nil {
		return nil, err
	}
	if err := boundary.ValidateDirectoryHandle(uintptr(rootFD)); err != nil {
		return nil, err
	}
	return boundary.ValidateDirectoryHandle, nil
}

func prepareRemovalDirectory(
	parentFD int,
	name string,
	path string,
	expected EntryIdentity,
	stat unix.Stat_t,
) (EntryIdentity, error) {
	if expected.kind != entryKindDirectory {
		return EntryIdentity{}, fmt.Errorf("removal entry %q is not a directory", path)
	}
	if stat.Mode&0o700 == 0o700 {
		return expected, nil
	}
	mode := uint32(stat.Mode&0o777) | 0o700
	if err := unix.Fchmodat(parentFD, name, mode, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return EntryIdentity{}, fmt.Errorf("make retired directory removable at %q: %w", path, err)
	}
	current, currentStat, err := observeAt(parentFD, name, path)
	if err != nil {
		return EntryIdentity{}, err
	}
	if current.kind != entryKindDirectory || !expected.sameObject(current) {
		return EntryIdentity{}, fmt.Errorf("retired directory identity changed while preparing cleanup at %q", path)
	}
	if err := validateOwnedStat(path, &currentStat); err != nil {
		return EntryIdentity{}, err
	}
	if currentStat.Mode&0o700 != 0o700 {
		return EntryIdentity{}, unsupported(fmt.Sprintf("retired directory %q did not retain private cleanup mode", path), nil)
	}
	return current, nil
}
