//go:build darwin || linux

package commit

import (
	"context"
	"fmt"

	"golang.org/x/sys/unix"
)

// ConfirmFilePersistence syncs exactly the observed regular file and its parent
// without rewriting bytes or creating ancestors. Success relies on the existing
// platform storage contract; it does not exclude concurrent external writers.
func ConfirmFilePersistence(ctx context.Context, path string, expected EntryIdentity) error {
	return confirmFilePersistenceWithFaults(ctx, path, expected, faultPlan{})
}

func confirmFilePersistenceWithFaults(ctx context.Context, path string, expected EntryIdentity, faults faultPlan) error {
	if ctx == nil {
		return failureBeforeVisibility(phaseValidate, path, fmt.Errorf("file persistence context is required"))
	}
	if err := faults.check(ctx, phaseValidate); err != nil {
		return failureBeforeVisibility(phaseValidate, path, err)
	}
	if err := validateCommitPath(path); err != nil {
		return failureBeforeVisibility(phaseValidate, path, err)
	}
	if err := validateExpectedIdentity(path, expected, entryKindRegular); err != nil {
		return failureBeforeVisibility(phaseValidate, path, err)
	}
	anchor, err := openAnchoredParent(path, false)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return failureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	fd, _, err := anchor.openExpected(anchor.base, path, expected)
	if err != nil {
		return failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	defer unix.Close(fd)
	if err := anchor.verifyChain(); err != nil {
		return failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	if err := faults.run(ctx, phaseSyncPayload, func() error { return syncPayload(fd) }); err != nil {
		return newFailure(failureRetainedResidue, phaseSyncPayload, path, err, path)
	}
	if err := faults.check(ctx, phaseRevalidateEntry); err != nil {
		return newFailure(failureRetainedResidue, phaseRevalidateEntry, path, err, path)
	}
	if err := requirePersistenceIdentity(anchor, fd, path, expected); err != nil {
		return newFailure(failureRetainedResidue, phaseRevalidateEntry, path, err, path)
	}
	if err := faults.run(ctx, phaseSyncParent, func() error { return syncDirectory(anchor.parentFD()) }); err != nil {
		return newFailure(failureRetainedResidue, phaseSyncParent, path, err, path)
	}
	if err := faults.check(ctx, phaseVerifyEntry); err != nil {
		return newFailure(failureRetainedResidue, phaseVerifyEntry, path, err, path)
	}
	if err := requirePersistenceIdentity(anchor, fd, path, expected); err != nil {
		return newFailure(failureRetainedResidue, phaseVerifyEntry, path, err, path)
	}
	if err := anchor.verifyChain(); err != nil {
		return newFailure(failureRetainedResidue, phaseVerifyEntry, path, err, path)
	}
	return nil
}

func requirePersistenceIdentity(anchor *anchoredParent, fd int, path string, expected EntryIdentity) error {
	opened, err := refreshOpenedIdentity(fd, path)
	if err != nil {
		return err
	}
	if !expected.sameEntry(opened) {
		return fmt.Errorf("file identity changed while confirming persistence at %q", path)
	}
	_, _, err = anchor.requireExpected(anchor.base, path, expected)
	return err
}
