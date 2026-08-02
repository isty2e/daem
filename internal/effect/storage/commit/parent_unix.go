//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// PrepareCommitParent creates and persists missing parent directories for a
// later file or same-parent prepared-tree commit. The returned evidence names
// only directories created by this invocation, shallowest first.
func PrepareCommitParent(ctx context.Context, path string) ([]CreatedDirectory, error) {
	return prepareCommitParentWithFaults(ctx, path, faultPlan{})
}

func prepareCommitParentWithFaults(
	ctx context.Context,
	path string,
	faults faultPlan,
) ([]CreatedDirectory, error) {
	if err := validateCommitPath(path); err != nil {
		return nil, newFailure(failureUncommitted, phaseValidate, path, err)
	}
	if err := faults.check(ctx, phaseValidate); err != nil {
		return nil, newFailure(failureUncommitted, phaseValidate, path, err)
	}
	if err := faults.check(ctx, phaseCreateAncestors); err != nil {
		return nil, newFailure(failureUncommitted, phaseCreateAncestors, path, err)
	}
	anchor, err := openCommitParent(path, nil, true)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		created := anchor.createdDirectories()
		return created, failFileBeforeVisibility(
			path,
			phaseCreateAncestors,
			err,
			anchor,
			"",
			EntryIdentity{},
			faults,
		)
	}
	created := anchor.createdDirectories()
	if err := anchor.verifyChain(); err != nil {
		return created, failFileBeforeVisibility(path, phaseValidate, err, anchor, "", EntryIdentity{}, faults)
	}
	if err := faults.check(ctx, phaseSyncAncestors); err != nil {
		return created, failFileBeforeVisibility(
			path,
			phaseSyncAncestors,
			err,
			anchor,
			"",
			EntryIdentity{},
			faults,
		)
	}
	if err := syncCreatedAncestors(anchor); err != nil {
		return created, failFileBeforeVisibility(
			path,
			phaseSyncAncestors,
			err,
			anchor,
			"",
			EntryIdentity{},
			faults,
		)
	}
	if err := anchor.verifyChain(); err != nil {
		return created, newFailure(failureIndeterminateCommit, phaseVerifyEntry, path, err)
	}
	return created, nil
}

// RemoveCreatedDirectoryIfEmpty removes only the exact directory represented
// by creation evidence and only while it remains empty.
func RemoveCreatedDirectoryIfEmpty(ctx context.Context, directory CreatedDirectory) error {
	return removeCreatedDirectoryIfEmptyWithFaults(ctx, directory, faultPlan{})
}

func removeCreatedDirectoryIfEmptyWithFaults(
	ctx context.Context,
	directory CreatedDirectory,
	faults faultPlan,
) error {
	path := directory.path
	if ctx == nil {
		return failureBeforeVisibility(
			phaseValidate,
			path,
			fmt.Errorf("created directory cleanup context is required"),
		)
	}
	if !directory.valid() {
		return failureBeforeVisibility(
			phaseValidate,
			path,
			fmt.Errorf("valid created directory evidence is required"),
		)
	}
	if err := faults.check(ctx, phaseValidate); err != nil {
		return failureBeforeVisibility(phaseValidate, path, err)
	}

	anchor, err := openCommitParent(path, nil, false)
	if anchor != nil {
		defer anchor.close()
	}
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return failureBeforeVisibility(phaseValidate, path, err)
	}
	if err := requireEmptyCreatedDirectory(ctx, anchor, directory); errors.Is(err, unix.ENOENT) {
		return nil
	} else if err != nil {
		return failureBeforeVisibility(phaseValidate, path, err)
	}
	if err := anchor.verifyChain(); err != nil {
		return failureBeforeVisibility(phaseValidate, path, err)
	}

	err = faults.run(ctx, phaseRevalidateEntry, func() error {
		return requireEmptyCreatedDirectory(ctx, anchor, directory)
	})
	if err != nil {
		return failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	if err := anchor.verifyChain(); err != nil {
		return failureBeforeVisibility(phaseValidate, path, err)
	}
	if err := faults.check(ctx, phaseCleanupEntry); err != nil {
		return failureBeforeVisibility(phaseCleanupEntry, path, err)
	}
	if err := requireEmptyCreatedDirectory(ctx, anchor, directory); err != nil {
		return failureBeforeVisibility(phaseCleanupEntry, path, err)
	}
	if err := anchor.verifyChain(); err != nil {
		return failureBeforeVisibility(phaseCleanupEntry, path, err)
	}
	if err := unix.Unlinkat(anchor.parentFD(), anchor.base, unix.AT_REMOVEDIR); err != nil {
		return classifyCreatedDirectoryCleanupFailure(anchor, directory, err)
	}
	if err := faults.run(ctx, phaseSyncCleanupParent, func() error {
		return syncDirectory(anchor.parentFD())
	}); err != nil {
		return newFailure(
			failureIndeterminateCommit,
			phaseSyncCleanupParent,
			path,
			err,
			path,
		)
	}
	if err := anchor.verifyChain(); err != nil {
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, path, err, path)
	}
	observed, _, err := anchor.observe(anchor.base, path)
	switch {
	case errors.Is(err, unix.ENOENT):
		return nil
	case err != nil:
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, path, err, path)
	case directory.identity.sameObject(observed):
		return newFailure(
			failureIndeterminateCommit,
			phaseVerifyEntry,
			path,
			fmt.Errorf("cleaned directory identity reappeared"),
			path,
		)
	default:
		return nil
	}
}

func requireEmptyCreatedDirectory(
	ctx context.Context,
	anchor *anchoredParent,
	directory CreatedDirectory,
) error {
	observed, stat, err := anchor.observe(anchor.base, directory.path)
	if err != nil {
		return err
	}
	if !directory.identity.sameObject(observed) || observed.kind != entryKindDirectory {
		return fmt.Errorf("created directory identity changed at %q", directory.path)
	}
	if err := validateOwnedStat(directory.path, &stat); err != nil {
		return err
	}
	directoryFD, _, err := anchor.openExpected(anchor.base, directory.path, observed)
	if err != nil {
		return err
	}
	names, readErr := readDirectoryNames(ctx, directoryFD, directory.path, 1)
	closeErr := unix.Close(directoryFD)
	if readErr != nil || closeErr != nil {
		return fmt.Errorf(
			"inspect created directory %q before cleanup: %w",
			directory.path,
			errors.Join(readErr, closeErr),
		)
	}
	if len(names) != 0 {
		return fmt.Errorf("created directory %q is not empty", directory.path)
	}
	return nil
}

func classifyCreatedDirectoryCleanupFailure(
	anchor *anchoredParent,
	directory CreatedDirectory,
	cause error,
) error {
	observed, _, observeErr := anchor.observe(anchor.base, directory.path)
	switch {
	case observeErr == nil && directory.identity.sameObject(observed):
		return failureBeforeVisibility(phaseCleanupEntry, directory.path, cause)
	case errors.Is(observeErr, unix.ENOENT):
		return newFailure(
			failureIndeterminateCommit,
			phaseCleanupEntry,
			directory.path,
			cause,
			directory.path,
		)
	default:
		return newFailure(
			failureIndeterminateCommit,
			phaseCleanupEntry,
			directory.path,
			errors.Join(cause, observeErr),
			directory.path,
		)
	}
}
