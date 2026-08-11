//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

// CaptureEntryIdentity captures ephemeral no-follow identity evidence for path.
func CaptureEntryIdentity(ctx context.Context, path string) (EntryIdentity, error) {
	return captureEntryIdentity(ctx, path, nil)
}

// CaptureRootedEntryIdentity captures no-follow final-entry identity through
// one rooted-path capability.
func CaptureRootedEntryIdentity(
	ctx context.Context,
	capability rootedpath.CommitCapability,
) (EntryIdentity, error) {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return EntryIdentity{}, err
	}
	return captureEntryIdentity(ctx, path, capability)
}

// CaptureWorkingDirectoryIdentity observes the exact retained directory object
// without re-resolving its diagnostic path.
func CaptureWorkingDirectoryIdentity(
	ctx context.Context,
	capability rootedpath.WorkingDirectoryCapability,
	budget rootedpath.PhysicalTraversalBudget,
) (EntryIdentity, error) {
	if ctx == nil {
		return EntryIdentity{}, fmt.Errorf("working-directory identity context is required")
	}
	if err := ctx.Err(); err != nil {
		return EntryIdentity{}, err
	}
	if capability == nil {
		return EntryIdentity{}, fmt.Errorf("working-directory identity capability is required")
	}
	file, err := capability.OpenDirectoryBounded(budget)
	if err != nil {
		return EntryIdentity{}, err
	}
	if err := ctx.Err(); err != nil {
		return EntryIdentity{}, errors.Join(err, file.Close())
	}
	identity, identityErr := refreshOpenedIdentity(int(file.Fd()), file.Name())
	closeErr := file.Close()
	if identityErr != nil || closeErr != nil {
		return EntryIdentity{}, errors.Join(identityErr, closeErr)
	}
	if identity.kind != entryKindDirectory {
		return EntryIdentity{}, fmt.Errorf("working-directory capability does not identify a directory")
	}
	return identity, nil
}

func captureEntryIdentity(
	ctx context.Context,
	path string,
	capability rootedpath.CommitCapability,
) (EntryIdentity, error) {
	if capability == nil {
		if err := validateCommitPath(path); err != nil {
			return EntryIdentity{}, err
		}
	} else if err := validateRootedCapability(path, capability); err != nil {
		return EntryIdentity{}, err
	}
	if err := (faultPlan{}).check(ctx, phaseCaptureIdentity); err != nil {
		return EntryIdentity{}, newFailure(failureUncommitted, phaseCaptureIdentity, path, err)
	}
	anchor, err := openCommitParent(path, capability, false)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	identity, stat, err := anchor.observe(anchor.base, path)
	if err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	if capability != nil && identity.kind == entryKindSymlink {
		return EntryIdentity{}, failureBeforeVisibility(
			phaseCaptureIdentity,
			path,
			rootedFinalSymlinkFailure(path),
		)
	}
	if err := validateOwnedStat(path, &stat); err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	return identity, nil
}

// CommitFile publishes a complete regular file and persists its namespace.
func CommitFile(ctx context.Context, request FileCommit) error {
	return commitFileWithFaultsAndParentRefresh(ctx, request, faultPlan{}, nil, nil)
}

func commitFileWithFaults(ctx context.Context, request FileCommit, faults faultPlan) error {
	return commitFileWithFaultsAndParentRefresh(ctx, request, faults, nil, nil)
}

func commitFileAndRefreshParent(
	ctx context.Context,
	request FileCommit,
) (EntryIdentity, error) {
	var refreshed EntryIdentity
	err := commitFileWithFaultsAndParentRefresh(ctx, request, faultPlan{}, &refreshed, nil)
	return refreshed, err
}

func commitFileWithFaultsAndParentRefresh(
	ctx context.Context,
	request FileCommit,
	faults faultPlan,
	refreshedParent *EntryIdentity,
	cleanup *AncestorCleanup,
) (returnErr error) {
	if request.capability != nil {
		defer request.capability.Close()
	}
	if err := validateFileRequest(request); err != nil {
		return newFailure(failureUncommitted, phaseValidate, request.path, err)
	}
	if cleanup != nil {
		if _, err := cleanup.requireOpen(); err != nil {
			return newFailure(failureUncommitted, phaseValidate, request.path, err)
		}
	}
	if err := faults.check(ctx, phaseValidate); err != nil {
		return newFailure(failureUncommitted, phaseValidate, request.path, err)
	}

	anchor, err := openCommitParent(request.path, request.capability, true)
	if anchor != nil {
		defer anchor.close()
		defer cleanup.capture(anchor)
	}
	if err != nil {
		return failFileBeforeVisibility(request.path, phaseCreateAncestors, err, anchor, "", EntryIdentity{}, faults)
	}
	if err := faults.check(ctx, phaseCreateAncestors); err != nil {
		return failFileBeforeVisibility(request.path, phaseCreateAncestors, err, anchor, "", EntryIdentity{}, faults)
	}
	if err := anchor.verifyChain(); err != nil {
		return failFileBeforeVisibility(request.path, phaseValidate, err, anchor, "", EntryIdentity{}, faults)
	}
	if request.expectedParent.valid() {
		observedParent, err := refreshOpenedIdentity(anchor.parentFD(), filepath.Dir(request.path))
		if err != nil {
			return failFileBeforeVisibility(request.path, phaseValidate, err, anchor, "", EntryIdentity{}, faults)
		}
		if !request.expectedParent.sameEntry(observedParent) {
			return failFileBeforeVisibility(
				request.path,
				phaseValidate,
				fmt.Errorf("parent directory identity changed before file replacement"),
				anchor,
				"",
				EntryIdentity{},
				faults,
			)
		}
		defer refreshCommittedParentIdentity(anchor, request, refreshedParent, &returnErr)
	}
	if request.policy == filePolicyReplaceExpected {
		if err := faults.check(ctx, phaseCaptureMetadata); err != nil {
			return failFileBeforeVisibility(
				request.path,
				phaseCaptureMetadata,
				err,
				anchor,
				"",
				EntryIdentity{},
				faults,
			)
		}
	}
	metadata, err := validateFileDestination(anchor, request)
	if err != nil {
		return failFileBeforeVisibility(
			request.path,
			errorPhase(err, phaseValidate),
			err,
			anchor,
			"",
			EntryIdentity{},
			faults,
		)
	}

	if err := faults.check(ctx, phaseCreateTemporary); err != nil {
		return failFileBeforeVisibility(request.path, phaseCreateTemporary, err, anchor, "", EntryIdentity{}, faults)
	}
	temporaryName, temporaryFD, temporaryIdentity, err := createTemporaryFile(anchor)
	if err != nil {
		return failFileBeforeVisibility(
			request.path,
			phaseCreateTemporary,
			err,
			anchor,
			temporaryName,
			temporaryIdentity,
			faults,
		)
	}
	closed := false
	defer func() {
		if !closed {
			_ = unix.Close(temporaryFD)
		}
	}()

	err = faults.writePayload(ctx, fdWriter{fd: temporaryFD}, request.payload)
	temporaryIdentity, err = refreshedTemporaryIdentity(temporaryFD, temporaryIdentity, err)
	if err != nil {
		_ = unix.Close(temporaryFD)
		closed = true
		return failFileBeforeVisibility(request.path, phaseWritePayload, err, anchor, temporaryName, temporaryIdentity, faults)
	}
	err = faults.run(ctx, phaseApplyMode, func() error {
		return unix.Fchmod(temporaryFD, uint32(request.mode.Perm()))
	})
	temporaryIdentity, err = refreshedTemporaryIdentity(temporaryFD, temporaryIdentity, err)
	if err != nil {
		_ = unix.Close(temporaryFD)
		closed = true
		return failFileBeforeVisibility(request.path, phaseApplyMode, err, anchor, temporaryName, temporaryIdentity, faults)
	}
	err = faults.run(ctx, phaseApplyMetadata, func() error {
		if err := applyPreservedMetadata(temporaryFD, metadata); err != nil {
			return err
		}
		var stat unix.Stat_t
		if err := unix.Fstat(temporaryFD, &stat); err != nil {
			return err
		}
		if fs.FileMode(stat.Mode).Perm() != request.mode.Perm() {
			return unsupported("preserved metadata changed the requested file mode", nil)
		}
		return nil
	})
	temporaryIdentity, err = refreshedTemporaryIdentity(temporaryFD, temporaryIdentity, err)
	if err != nil {
		_ = unix.Close(temporaryFD)
		closed = true
		return failFileBeforeVisibility(request.path, phaseApplyMetadata, err, anchor, temporaryName, temporaryIdentity, faults)
	}
	err = faults.run(ctx, phaseSyncPayload, func() error { return syncPayload(temporaryFD) })
	if err != nil {
		_ = unix.Close(temporaryFD)
		closed = true
		return failFileBeforeVisibility(request.path, phaseSyncPayload, err, anchor, temporaryName, temporaryIdentity, faults)
	}
	err = faults.run(ctx, phaseClosePayload, func() error { return unix.Close(temporaryFD) })
	if err != nil {
		_ = unix.Close(temporaryFD)
	}
	closed = true
	if err != nil {
		return failFileBeforeVisibility(request.path, phaseClosePayload, err, anchor, temporaryName, temporaryIdentity, faults)
	}
	if err := anchor.verifyChain(); err != nil {
		return failFileBeforeVisibility(request.path, phaseValidate, err, anchor, temporaryName, temporaryIdentity, faults)
	}
	if request.policy == filePolicyReplaceExpected {
		err = faults.run(ctx, phaseRevalidateEntry, func() error {
			fd, _, err := anchor.openExpected(anchor.base, request.path, request.expected)
			if err != nil {
				return err
			}
			defer unix.Close(fd)
			return verifyPreservedMetadata(fd, metadata)
		})
		if err != nil {
			return failFileBeforeVisibility(
				request.path,
				phaseRevalidateEntry,
				err,
				anchor,
				temporaryName,
				temporaryIdentity,
				faults,
			)
		}
	}
	if err := anchor.verifyChain(); err != nil {
		return failFileBeforeVisibility(
			request.path,
			phaseValidate,
			err,
			anchor,
			temporaryName,
			temporaryIdentity,
			faults,
		)
	}

	err = faults.run(ctx, phaseCommitEntry, func() error {
		switch request.policy {
		case filePolicyMustBeAbsent:
			return renameNoReplace(anchor.parentFD(), temporaryName, anchor.parentFD(), anchor.base)
		case filePolicyReplaceExpected:
			return unix.Renameat(anchor.parentFD(), temporaryName, anchor.parentFD(), anchor.base)
		default:
			return fmt.Errorf("invalid file commit policy")
		}
	})
	if err != nil {
		return failFileBeforeVisibility(request.path, phaseCommitEntry, err, anchor, temporaryName, temporaryIdentity, faults)
	}

	err = faults.run(ctx, phaseVerifyEntry, func() error {
		if err := anchor.verifyChain(); err != nil {
			return err
		}
		return verifyCommittedFile(anchor, request, temporaryIdentity, metadata)
	})
	if err != nil {
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err)
	}
	err = faults.run(ctx, phaseSyncParent, func() error { return syncDirectory(anchor.parentFD()) })
	if err != nil {
		return newFailure(failureIndeterminateCommit, phaseSyncParent, request.path, err)
	}
	if hasCreatedAncestors(anchor) {
		err = faults.run(ctx, phaseSyncAncestors, func() error { return syncCreatedAncestors(anchor) })
		if err != nil {
			return newFailure(failureIndeterminateCommit, phaseSyncAncestors, request.path, err)
		}
	}
	if err := anchor.verifyChain(); err != nil {
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err)
	}
	return nil
}

func refreshCommittedParentIdentity(
	anchor *anchoredParent,
	request FileCommit,
	refreshedParent *EntryIdentity,
	returnErr *error,
) {
	if anchor == nil || refreshedParent == nil || returnErr == nil {
		return
	}
	path := filepath.Dir(request.path)
	refreshed, err := refreshOpenedIdentity(anchor.parentFD(), path)
	if err == nil && !request.expectedParent.sameObject(refreshed) {
		err = fmt.Errorf("parent directory object changed during file replacement")
	}
	if err == nil {
		err = anchor.verifyChain()
	}
	if err != nil {
		if *returnErr == nil {
			*returnErr = newFailure(
				failureIndeterminateCommit,
				phaseVerifyEntry,
				request.path,
				fmt.Errorf("refresh parent directory identity: %w", err),
			)
		}
		return
	}
	*refreshedParent = refreshed
}

func refreshedTemporaryIdentity(fd int, current EntryIdentity, operationErr error) (EntryIdentity, error) {
	refreshed, refreshErr := refreshOpenedIdentity(fd, current.path)
	if refreshErr != nil {
		if operationErr != nil {
			return current, errors.Join(operationErr, fmt.Errorf("refresh temporary identity: %w", refreshErr))
		}
		return current, fmt.Errorf("refresh temporary identity: %w", refreshErr)
	}
	return refreshed, operationErr
}

func validateFileRequest(request FileCommit) error {
	if err := validateCommitPath(request.path); err != nil {
		return err
	}
	if err := validateFileMode(request.mode); err != nil {
		return err
	}
	if err := validateRootedCapability(request.path, request.capability); err != nil {
		return err
	}
	if request.capability != nil && request.expected.kind == entryKindSymlink {
		return rootedFinalSymlinkFailure(request.path)
	}
	switch request.policy {
	case filePolicyMustBeAbsent:
		if request.expected.valid() {
			return fmt.Errorf("exclusive creation must not carry expected identity")
		}
	case filePolicyReplaceExpected:
		if err := validateExpectedIdentity(request.path, request.expected, entryKindRegular); err != nil {
			return err
		}
		if request.expectedParent.valid() {
			if request.capability == nil {
				return fmt.Errorf("parent identity refresh requires rooted file authority")
			}
			return validateExpectedIdentity(
				filepath.Dir(request.path),
				request.expectedParent,
				entryKindDirectory,
			)
		}
		return nil
	default:
		return fmt.Errorf("invalid file commit policy")
	}
	return nil
}

func validateFileDestination(anchor *anchoredParent, request FileCommit) (preservedMetadata, error) {
	switch request.policy {
	case filePolicyMustBeAbsent:
		exists, err := entryExists(anchor.parentFD(), anchor.base)
		if err != nil {
			return preservedMetadata{}, err
		}
		if exists {
			if anchor.capability != nil {
				identity, _, observeErr := anchor.observe(anchor.base, request.path)
				if observeErr != nil {
					return preservedMetadata{}, observeErr
				}
				if identity.kind == entryKindSymlink {
					return preservedMetadata{}, rootedFinalSymlinkFailure(request.path)
				}
			}
			return preservedMetadata{}, fs.ErrExist
		}
		return preservedMetadata{xattrs: make(map[string][]byte)}, nil
	case filePolicyReplaceExpected:
		fd, stat, err := anchor.openExpected(anchor.base, request.path, request.expected)
		if fd >= 0 {
			defer unix.Close(fd)
		}
		if err != nil {
			return preservedMetadata{}, err
		}
		metadata, err := capturePreservedMetadata(fd, &stat)
		return metadata, atPhase(phaseCaptureMetadata, err)
	default:
		return preservedMetadata{}, fmt.Errorf("invalid file commit policy")
	}
}

func createTemporaryFile(anchor *anchoredParent) (string, int, EntryIdentity, error) {
	for range 64 {
		name, err := randomSiblingName(temporaryPrefix)
		if err != nil {
			return "", -1, EntryIdentity{}, err
		}
		fd, err := unix.Openat(
			anchor.parentFD(),
			name,
			unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			0o600,
		)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return "", -1, EntryIdentity{}, err
		}
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil {
			_ = unix.Close(fd)
			return name, -1, EntryIdentity{}, err
		}
		path := filepath.Join(filepath.Dir(anchor.path), name)
		return name, fd, identityFromStat(path, &stat), nil
	}
	return "", -1, EntryIdentity{}, fmt.Errorf("could not allocate a collision-free temporary name")
}

func verifyCommittedFile(
	anchor *anchoredParent,
	request FileCommit,
	temporary EntryIdentity,
	metadata preservedMetadata,
) error {
	observed, stat, err := anchor.observe(anchor.base, request.path)
	if err != nil {
		return err
	}
	if !temporary.sameObject(observed) {
		return fmt.Errorf("committed entry does not identify the prepared payload")
	}
	if observed.kind != entryKindRegular {
		return fmt.Errorf("committed entry is not a regular file")
	}
	if fs.FileMode(stat.Mode).Perm() != request.mode.Perm() {
		return fmt.Errorf("committed mode is %04o, want %04o", fs.FileMode(stat.Mode).Perm(), request.mode.Perm())
	}
	fd, _, err := anchor.openExpected(anchor.base, request.path, observed)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return verifyPreservedMetadata(fd, metadata)
}

type fdWriter struct{ fd int }

func (writer fdWriter) Write(payload []byte) (int, error) {
	return unix.Write(writer.fd, payload)
}
