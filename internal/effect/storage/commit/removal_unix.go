//go:build darwin || linux

package commit

import (
	"context"
	"fmt"
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
