//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"golang.org/x/sys/unix"
)

// CommitRootedEntryRename performs one exact, no-replace same-parent rename and
// persists the parent namespace.
func CommitRootedEntryRename(
	ctx context.Context,
	request RootedEntryRename,
) (mutationfs.CommitOutcome, error) {
	return commitRootedEntryRenameWithFaults(ctx, request, faultPlan{})
}

func commitRootedEntryRenameWithFaults(
	ctx context.Context,
	request RootedEntryRename,
	faults faultPlan,
) (mutationfs.CommitOutcome, error) {
	if request.capability != nil {
		defer request.capability.Close()
	}
	fail := func(err error) (mutationfs.CommitOutcome, error) {
		return outcomeFromError(err), err
	}
	if ctx == nil {
		return fail(failureBeforeVisibility(
			phaseValidate,
			request.sourcePath,
			fmt.Errorf("rooted entry rename context is required"),
		))
	}
	if err := validateRootedEntryRename(request); err != nil {
		return fail(failureBeforeVisibility(phaseValidate, request.sourcePath, err))
	}
	if err := faults.check(ctx, phaseValidate); err != nil {
		return fail(failureBeforeVisibility(phaseValidate, request.sourcePath, err))
	}

	anchor, err := openCommitParent(request.sourcePath, request.capability, false)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return fail(failureBeforeVisibility(phaseValidate, request.sourcePath, err))
	}
	destinationPath := filepath.Join(filepath.Dir(request.sourcePath), request.destinationName)
	exists, err := entryExists(anchor.parentFD(), request.destinationName)
	if err != nil {
		return fail(failureBeforeVisibility(phaseValidate, request.sourcePath, err))
	}
	if exists {
		return fail(failureBeforeVisibility(
			phaseValidate,
			request.sourcePath,
			fs.ErrExist,
		))
	}

	sourceFD := -1
	var sourceMode fs.FileMode
	err = faults.run(ctx, phaseRevalidateEntry, func() error {
		fd, stat, observeErr := anchor.openExpected(
			anchor.base,
			request.sourcePath,
			request.expected,
		)
		if observeErr != nil {
			return observeErr
		}
		sourceFD = fd
		sourceMode = fs.FileMode(stat.Mode).Perm()
		return nil
	})
	if err != nil {
		return fail(failureBeforeVisibility(phaseRevalidateEntry, request.sourcePath, err))
	}
	defer unix.Close(sourceFD)
	if err := anchor.verifyChain(); err != nil {
		return fail(failureBeforeVisibility(phaseValidate, request.sourcePath, err))
	}

	err = faults.run(ctx, phaseCommitEntry, func() error {
		return renameNoReplace(
			anchor.parentFD(),
			anchor.base,
			anchor.parentFD(),
			request.destinationName,
		)
	})
	if err != nil {
		return fail(failureBeforeVisibility(phaseCommitEntry, request.sourcePath, err))
	}
	verifyMoved := func() error {
		if err := anchor.verifyChain(); err != nil {
			return err
		}
		moved, stat, err := anchor.observe(request.destinationName, destinationPath)
		if err != nil {
			return err
		}
		if !request.expected.sameObject(moved) {
			return fmt.Errorf("renamed entry identity does not match source")
		}
		if err := validateOwnedStat(destinationPath, &stat); err != nil {
			return err
		}
		if mode := fs.FileMode(stat.Mode).Perm(); mode != sourceMode {
			return fmt.Errorf(
				"renamed entry mode is %04o, want %04o",
				mode,
				sourceMode,
			)
		}
		sourceExists, err := entryExists(anchor.parentFD(), anchor.base)
		if err != nil {
			return err
		}
		if sourceExists {
			return fmt.Errorf("source name reappeared during rename")
		}
		return nil
	}
	if err := faults.run(ctx, phaseVerifyEntry, verifyMoved); err != nil {
		return fail(newFailure(
			failureIndeterminateCommit,
			phaseVerifyEntry,
			request.sourcePath,
			err,
			destinationPath,
		))
	}
	if err := faults.run(ctx, phaseSyncParent, func() error {
		return syncDirectory(anchor.parentFD())
	}); err != nil {
		return fail(newFailure(
			failureIndeterminateCommit,
			phaseSyncParent,
			request.sourcePath,
			err,
			destinationPath,
		))
	}
	if err := verifyMoved(); err != nil {
		return fail(newFailure(
			failureIndeterminateCommit,
			phaseVerifyEntry,
			request.sourcePath,
			err,
			destinationPath,
		))
	}
	return outcomeFromError(nil), nil
}

// CommitRootedEntryCleanup removes only the exact expected entry and persists
// its absence. It does not rename the entry or infer cleanup authority.
func CommitRootedEntryCleanup(
	ctx context.Context,
	request RootedEntryCleanup,
) (mutationfs.CommitOutcome, error) {
	return commitRootedEntryCleanupWithFaults(ctx, request, faultPlan{})
}

func commitRootedEntryCleanupWithFaults(
	ctx context.Context,
	request RootedEntryCleanup,
	faults faultPlan,
) (mutationfs.CommitOutcome, error) {
	if request.capability != nil {
		defer request.capability.Close()
	}
	fail := func(err error) (mutationfs.CommitOutcome, error) {
		return outcomeFromError(err), err
	}
	if ctx == nil {
		return fail(failureBeforeVisibility(
			phaseValidate,
			request.path,
			fmt.Errorf("rooted entry cleanup context is required"),
		))
	}
	if err := validateRootedEntryCleanup(request); err != nil {
		return fail(failureBeforeVisibility(phaseValidate, request.path, err))
	}
	if err := faults.check(ctx, phaseValidate); err != nil {
		return fail(failureBeforeVisibility(phaseValidate, request.path, err))
	}

	anchor, err := openCommitParent(request.path, request.capability, false)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return fail(failureBeforeVisibility(phaseValidate, request.path, err))
	}
	if _, _, err := requireOwnedExpectedEntry(
		anchor,
		anchor.base,
		request.path,
		request.expected,
	); err != nil {
		return fail(failureBeforeVisibility(phaseValidate, request.path, err))
	}
	if err := anchor.verifyChain(); err != nil {
		return fail(failureBeforeVisibility(phaseValidate, request.path, err))
	}
	err = faults.run(ctx, phaseRevalidateEntry, func() error {
		_, _, err := requireOwnedExpectedEntry(
			anchor,
			anchor.base,
			request.path,
			request.expected,
		)
		return err
	})
	if err != nil {
		return fail(failureBeforeVisibility(phaseRevalidateEntry, request.path, err))
	}
	if err := anchor.verifyChain(); err != nil {
		return fail(failureBeforeVisibility(phaseValidate, request.path, err))
	}

	err = removeEntryAtWithFaults(
		ctx,
		anchor.parentFD(),
		anchor.base,
		request.path,
		request.expected,
		request.capability,
		faults,
	)
	if err != nil {
		return fail(classifyExactCleanupFailure(anchor, request, err))
	}
	if err := faults.run(ctx, phaseSyncCleanupParent, func() error {
		return syncDirectory(anchor.parentFD())
	}); err != nil {
		return fail(newFailure(
			failureIndeterminateCommit,
			phaseSyncCleanupParent,
			request.path,
			err,
			request.path,
		))
	}
	if err := anchor.verifyChain(); err != nil {
		return fail(newFailure(
			failureIndeterminateCommit,
			phaseVerifyEntry,
			request.path,
			err,
			request.path,
		))
	}
	exists, err := entryExists(anchor.parentFD(), anchor.base)
	if err != nil {
		return fail(newFailure(
			failureIndeterminateCommit,
			phaseVerifyEntry,
			request.path,
			err,
			request.path,
		))
	}
	if exists {
		return fail(newFailure(
			failureIndeterminateCommit,
			phaseVerifyEntry,
			request.path,
			fmt.Errorf("cleaned entry name reappeared"),
			request.path,
		))
	}
	return outcomeFromError(nil), nil
}

func validateRootedEntryRename(request RootedEntryRename) error {
	if err := validateCommitPath(request.sourcePath); err != nil {
		return err
	}
	if err := validateRootedCapability(request.sourcePath, request.capability); err != nil {
		return err
	}
	if err := validateSiblingName(request.destinationName); err != nil {
		return err
	}
	if filepath.Base(request.sourcePath) == request.destinationName {
		return fmt.Errorf("source and destination names must differ")
	}
	return validateLifecycleIdentity(request.sourcePath, request.expected)
}

func validateRootedEntryCleanup(request RootedEntryCleanup) error {
	if err := validateCommitPath(request.path); err != nil {
		return err
	}
	if err := validateRootedCapability(request.path, request.capability); err != nil {
		return err
	}
	return validateLifecycleIdentity(request.path, request.expected)
}

func validateLifecycleIdentity(path string, expected EntryIdentity) error {
	if !expected.valid() || expected.path != path {
		return fmt.Errorf("expected identity must describe %q", path)
	}
	switch expected.kind {
	case entryKindRegular, entryKindDirectory:
		return nil
	case entryKindSymlink:
		return rootedFinalSymlinkFailure(path)
	default:
		return fmt.Errorf("expected identity has unsupported entry kind")
	}
}

func requireOwnedExpectedEntry(
	anchor *anchoredParent,
	name string,
	path string,
	expected EntryIdentity,
) (EntryIdentity, unix.Stat_t, error) {
	observed, stat, err := anchor.requireExpected(name, path, expected)
	if err != nil {
		return EntryIdentity{}, unix.Stat_t{}, err
	}
	if err := validateOwnedStat(path, &stat); err != nil {
		return EntryIdentity{}, unix.Stat_t{}, err
	}
	if expected.kind != entryKindSymlink {
		fd, _, err := anchor.openExpected(name, path, expected)
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		if err != nil {
			return EntryIdentity{}, unix.Stat_t{}, err
		}
	}
	return observed, stat, nil
}

func classifyExactCleanupFailure(
	anchor *anchoredParent,
	request RootedEntryCleanup,
	cause error,
) error {
	observed, _, observeErr := anchor.observe(anchor.base, request.path)
	switch {
	case observeErr == nil && request.expected.sameEntry(observed):
		return failureBeforeVisibility(phaseCleanupEntry, request.path, cause)
	case observeErr == nil && request.expected.sameObject(observed):
		return newFailure(
			failureRetainedResidue,
			phaseCleanupEntry,
			request.path,
			cause,
			request.path,
		)
	case errors.Is(observeErr, unix.ENOENT):
		return newFailure(
			failureIndeterminateCommit,
			phaseCleanupEntry,
			request.path,
			cause,
			request.path,
		)
	default:
		return newFailure(
			failureIndeterminateCommit,
			phaseCleanupEntry,
			request.path,
			errors.Join(cause, observeErr),
			request.path,
		)
	}
}
