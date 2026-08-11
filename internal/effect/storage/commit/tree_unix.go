//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// CommitPreparedTree publishes an already prepared same-parent tree.
func CommitPreparedTree(ctx context.Context, request PreparedTreeCommit) error {
	return commitPreparedTreeWithFaults(ctx, request, faultPlan{})
}

func commitPreparedTreeWithFaults(ctx context.Context, request PreparedTreeCommit, faults faultPlan) error {
	if err := validatePreparedTreeRequest(request); err != nil {
		return newFailure(failureUncommitted, phaseValidate, request.destination, err)
	}
	if err := faults.check(ctx, phaseValidate); err != nil {
		return newFailure(failureUncommitted, phaseValidate, request.destination, err)
	}

	anchor, err := openAnchoredParent(request.destination, false)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return failureBeforeVisibility(phaseValidate, request.destination, err)
	}
	exists, err := entryExists(anchor.parentFD(), filepath.Base(request.destination))
	if err != nil {
		return failureBeforeVisibility(phaseValidate, request.destination, err)
	}
	if exists {
		return failureBeforeVisibility(phaseValidate, request.destination, errors.New("destination already exists"))
	}

	stagedName := filepath.Base(request.stagedRoot)
	stagedFD, _, err := anchor.openExpected(stagedName, request.stagedRoot, request.expected)
	if err != nil {
		return failureBeforeVisibility(phaseValidate, request.destination, err)
	}
	defer unix.Close(stagedFD)
	if err := syncPreparedDirectory(ctx, stagedFD, request.stagedRoot, faults); err != nil {
		return failureBeforeVisibility(errorPhase(err, phaseSyncTreeDirectory), request.destination, err)
	}
	err = faults.run(ctx, phaseRevalidateEntry, func() error {
		_, _, err := anchor.requireExpected(stagedName, request.stagedRoot, request.expected)
		return err
	})
	if err != nil {
		return failureBeforeVisibility(phaseRevalidateEntry, request.destination, err)
	}
	if err := anchor.verifyChain(); err != nil {
		return failureBeforeVisibility(phaseValidate, request.destination, err)
	}

	err = faults.run(ctx, phaseCommitEntry, func() error {
		return renameNoReplace(anchor.parentFD(), stagedName, anchor.parentFD(), filepath.Base(request.destination))
	})
	if err != nil {
		return failureBeforeVisibility(phaseCommitEntry, request.destination, err)
	}
	err = faults.run(ctx, phaseVerifyEntry, func() error {
		if err := anchor.verifyChain(); err != nil {
			return err
		}
		var observed EntryIdentity
		observed, _, observeErr := anchor.observe(filepath.Base(request.destination), request.destination)
		if observeErr != nil {
			return observeErr
		}
		if !request.expected.sameObject(observed) {
			return fmt.Errorf("published tree identity does not match staged tree")
		}
		return nil
	})
	if err != nil {
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.destination, err)
	}
	err = faults.run(ctx, phaseSyncParent, func() error { return syncDirectory(anchor.parentFD()) })
	if err != nil {
		return newFailure(failureIndeterminateCommit, phaseSyncParent, request.destination, err)
	}
	if err := anchor.verifyChain(); err != nil {
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.destination, err)
	}
	return nil
}

func validatePreparedTreeRequest(request PreparedTreeCommit) error {
	if err := validateCommitPath(request.stagedRoot); err != nil {
		return fmt.Errorf("staged root: %w", err)
	}
	if err := validateCommitPath(request.destination); err != nil {
		return fmt.Errorf("destination: %w", err)
	}
	if request.stagedRoot == request.destination || filepath.Dir(request.stagedRoot) != filepath.Dir(request.destination) {
		return fmt.Errorf("prepared tree requires distinct same-parent paths")
	}
	return validateExpectedIdentity(request.stagedRoot, request.expected, entryKindDirectory)
}

func syncPreparedDirectory(ctx context.Context, directoryFD int, path string, faults faultPlan) error {
	names, err := readDirectoryNames(
		ctx,
		directoryFD,
		path,
		defaultTreeTraversalMaximumEntries,
	)
	if err != nil {
		return atPhase(phaseValidate, err)
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return atPhase(phaseSyncTreeDirectory, err)
		}
		entryPath := filepath.Join(path, name)
		identity, stat, err := observeAt(directoryFD, name, entryPath)
		if err != nil {
			return atPhase(phaseValidate, err)
		}
		if err := validateOwnedStat(entryPath, &stat); err != nil {
			return atPhase(phaseValidate, err)
		}
		switch identity.kind {
		case entryKindRegular:
			fd, err := openExpectedAt(directoryFD, name, entryPath, identity)
			if err != nil {
				return atPhase(phaseValidate, err)
			}
			err = faults.run(ctx, phaseSyncTreeFile, func() error { return syncPayload(fd) })
			_ = unix.Close(fd)
			if err != nil {
				return atPhase(phaseSyncTreeFile, err)
			}
		case entryKindDirectory:
			fd, err := openExpectedAt(directoryFD, name, entryPath, identity)
			if err != nil {
				return atPhase(phaseValidate, err)
			}
			err = syncPreparedDirectory(ctx, fd, entryPath, faults)
			_ = unix.Close(fd)
			if err != nil {
				return err
			}
		default:
			return atPhase(
				phaseValidate,
				unsupported(fmt.Sprintf("prepared tree contains unsupported entry %q", entryPath), nil),
			)
		}
	}
	return atPhase(
		phaseSyncTreeDirectory,
		faults.run(ctx, phaseSyncTreeDirectory, func() error { return syncDirectory(directoryFD) }),
	)
}

func openExpectedAt(parentFD int, name string, path string, expected EntryIdentity) (int, error) {
	observed, before, err := observeAt(parentFD, name, path)
	if err != nil {
		return -1, err
	}
	if !expected.sameEntry(observed) {
		return -1, fmt.Errorf("entry identity changed at %q", path)
	}
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if observed.kind == entryKindDirectory {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Openat(parentFD, name, flags, 0)
	if err != nil {
		return -1, err
	}
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	if !expected.sameEntry(identityFromStat(path, &opened)) {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("entry identity changed while opening %q", path)
	}
	if err := validateOwnedStat(path, &before); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	return fd, nil
}
