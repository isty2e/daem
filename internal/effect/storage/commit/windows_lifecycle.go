//go:build windows

package commit

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func CommitRootedEntryRename(
	ctx context.Context,
	request RootedEntryRename,
) (mutationfs.CommitOutcome, error) {
	if request.capability != nil {
		defer request.capability.Close()
	}
	fail := func(err error) (mutationfs.CommitOutcome, error) { return outcomeFromError(err), err }
	if ctx == nil {
		return fail(windowsFailureBeforeVisibility(phaseValidate, request.sourcePath, fmt.Errorf("rooted entry rename context is required")))
	}
	if err := validateWindowsRootedEntryRename(request); err != nil {
		return fail(windowsFailureBeforeVisibility(phaseValidate, request.sourcePath, err))
	}
	anchor, err := openWindowsDestinationAnchor(ctx, request.capability, true)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return fail(windowsFailureBeforeVisibility(phaseCaptureIdentity, request.sourcePath, windowsUnsupportedCause(err)))
	}
	if err := requireWindowsSiblingAbsent(anchor.parentDirectory(), request.destinationName); err != nil {
		return fail(windowsFailureBeforeVisibility(phaseValidate, request.sourcePath, err))
	}
	source, err := openWindowsObservedEntry(ctx, anchor, true, false, true)
	if source != nil {
		defer source.close()
	}
	if err != nil {
		return fail(windowsFailureBeforeVisibility(phaseCaptureIdentity, request.sourcePath, windowsUnsupportedCause(err)))
	}
	if !source.identity.sameEntry(request.expected) {
		return fail(windowsFailureBeforeVisibility(phaseRevalidateEntry, request.sourcePath, fmt.Errorf("rename source no longer matches expected identity")))
	}
	if err := revalidateWindowsObservedEntry(ctx, anchor, source); err != nil {
		return fail(windowsFailureBeforeVisibility(phaseRevalidateEntry, request.sourcePath, err))
	}
	if err := ctx.Err(); err != nil {
		return fail(windowsFailureBeforeVisibility(phaseCommitEntry, request.sourcePath, err))
	}
	if _, err := renameWindowsByHandle(source.handle.Handle(), anchor.parentHandle(), request.destinationName, windowsRenameNoReplace); err != nil {
		moved, unchanged, observeErr := observeWindowsNamespaceTransition(
			anchor.parentDirectory(),
			anchor.name.String(),
			request.destinationName,
			source.facts.identity,
		)
		if unchanged && observeErr == nil {
			return fail(windowsFailureBeforeVisibility(phaseCommitEntry, request.sourcePath, err))
		}
		destinationPath := filepath.Join(filepath.Dir(request.sourcePath), request.destinationName)
		residue := []string(nil)
		if moved {
			residue = []string{destinationPath}
		}
		return fail(newFailure(failureIndeterminateCommit, phaseCommitEntry, request.sourcePath, errors.Join(err, observeErr), residue...))
	}
	destinationPath := filepath.Join(filepath.Dir(request.sourcePath), request.destinationName)
	postRenameFacts, factsErr := queryWindowsEntryFacts(source.handle.Handle())
	moved, err := observeWindowsEntryAt(anchor.parentDirectory(), request.destinationName)
	if factsErr != nil || err != nil || !moved.exists || !postRenameFacts.identity.equal(moved.identity) {
		err = errors.Join(factsErr, err)
		return fail(newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.sourcePath, err, destinationPath))
	}
	if err := requireWindowsDestinationAbsent(anchor); err != nil {
		return fail(newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.sourcePath, err, destinationPath))
	}
	if err := flushWindowsHandle(anchor.parentHandle(), windowsFlushPolicy{directory: true}); err != nil {
		return fail(newFailure(failureIndeterminateCommit, phaseSyncParent, request.sourcePath, err, destinationPath))
	}
	if err := anchor.revalidate(context.WithoutCancel(ctx)); err != nil {
		return fail(newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.sourcePath, err, destinationPath))
	}
	if err := source.close(); err != nil {
		return fail(newFailure(
			failureIndeterminateCommit,
			phaseClosePayload,
			request.sourcePath,
			err,
			destinationPath,
		))
	}
	if request.moved != nil {
		*request.moved = EntryIdentity{
			path:     destinationPath,
			kind:     source.identity.kind,
			platform: platformIdentity{native: moved.identity},
		}
	}
	return outcomeFromError(nil), nil
}

func CommitRootedEntryCleanup(
	ctx context.Context,
	request RootedEntryCleanup,
) (mutationfs.CommitOutcome, error) {
	if request.capability != nil {
		defer request.capability.Close()
	}
	fail := func(err error) (mutationfs.CommitOutcome, error) { return outcomeFromError(err), err }
	if ctx == nil {
		return fail(windowsFailureBeforeVisibility(phaseValidate, request.path, fmt.Errorf("rooted entry cleanup context is required")))
	}
	if err := validateWindowsRootedEntryCleanup(request); err != nil {
		return fail(windowsFailureBeforeVisibility(phaseValidate, request.path, err))
	}
	if err := admitWindowsRootedCleanupWork(request); err != nil {
		return fail(windowsFailureBeforeVisibility(phaseValidate, request.path, err))
	}
	anchor, err := openWindowsDestinationAnchor(ctx, request.capability, true)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return fail(windowsFailureBeforeVisibility(phaseCaptureIdentity, request.path, windowsUnsupportedCause(err)))
	}
	observed, err := openWindowsObservedEntry(ctx, anchor, true, false, true)
	if observed != nil {
		defer observed.close()
	}
	if err != nil {
		return fail(windowsFailureBeforeVisibility(phaseCaptureIdentity, request.path, windowsUnsupportedCause(err)))
	}
	if !observed.identity.sameEntry(request.expected) {
		return fail(windowsFailureBeforeVisibility(phaseRevalidateEntry, request.path, fmt.Errorf("cleanup entry no longer matches expected identity")))
	}
	if err := observed.close(); err != nil {
		return fail(windowsFailureBeforeVisibility(phaseClosePayload, request.path, err))
	}
	budget, err := newTreeTraversalBudget(request.limits)
	if err != nil {
		return fail(windowsFailureBeforeVisibility(phaseValidate, request.path, err))
	}
	err = removeWindowsEntryTree(
		ctx,
		anchor.parentDirectory(),
		anchor.name.String(),
		request.path,
		request.expected,
		0,
		budget,
		nil,
		"",
	)
	if err != nil {
		return fail(classifyWindowsExactCleanupFailure(anchor, request, err))
	}
	if err := flushWindowsHandle(anchor.parentHandle(), windowsFlushPolicy{directory: true}); err != nil {
		return fail(newFailure(failureIndeterminateCommit, phaseSyncCleanupParent, request.path, err, request.path))
	}
	if err := anchor.revalidate(context.WithoutCancel(ctx)); err != nil {
		return fail(newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err, request.path))
	}
	if err := requireWindowsDestinationAbsent(anchor); err != nil {
		return fail(newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err, request.path))
	}
	return outcomeFromError(nil), nil
}

func validateWindowsRootedEntryRename(request RootedEntryRename) error {
	if err := validateRootedCapability(request.sourcePath, request.capability); err != nil {
		return err
	}
	if err := validateSiblingName(request.destinationName); err != nil {
		return err
	}
	if filepath.Base(request.sourcePath) == request.destinationName {
		return fmt.Errorf("source and destination names must differ")
	}
	return validateWindowsLifecycleIdentity(request.sourcePath, request.expected)
}

func validateWindowsRootedEntryCleanup(request RootedEntryCleanup) error {
	if request.removalNames == nil {
		if err := validateCommitPath(request.path); err != nil {
			return err
		}
	} else {
		if err := validateRootedPath(request.path); err != nil {
			return err
		}
		if !request.removalNames.Valid() || filepath.Base(request.path) != request.removalNames.Cleanup() {
			return fmt.Errorf("rooted removal cleanup path is not the authorized cleanup stage")
		}
	}
	if err := validateRootedCapability(request.path, request.capability); err != nil {
		return err
	}
	if err := request.limits.Validate(); err != nil {
		return err
	}
	return validateWindowsLifecycleIdentity(request.path, request.expected)
}

func validateWindowsLifecycleIdentity(path string, expected EntryIdentity) error {
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

func admitWindowsRootedCleanupWork(request RootedEntryCleanup) error {
	kind := mutationfs.EntryKindFile
	if request.expected.kind == entryKindDirectory {
		kind = mutationfs.EntryKindDirectory
	}
	envelope, err := mutationfs.NewRootedCleanupWorkEnvelope(kind, request.limits)
	if err != nil {
		return err
	}
	parentValidationWork, err := request.capability.Destination().ParentChainValidationWork()
	if err != nil {
		return err
	}
	pathWork, err := envelope.PathWork(parentValidationWork)
	if err != nil {
		return err
	}
	return request.capability.AdmitPhysicalWork(pathWork, envelope.EntryWork(), envelope.ByteWork())
}

func classifyWindowsExactCleanupFailure(
	anchor *windowsDestinationAnchor,
	request RootedEntryCleanup,
	cause error,
) error {
	observed, observeErr := observeWindowsEntryAt(anchor.parentDirectory(), anchor.name.String())
	switch {
	case observeErr == nil && observed.exists && request.expected.platform.native.equal(observed.identity):
		return windowsFailureBeforeVisibility(phaseCleanupEntry, request.path, cause)
	case observeErr == nil && observed.exists && request.expected.platform.native.sameObject(observed.identity):
		return newFailure(failureRetainedResidue, phaseCleanupEntry, request.path, cause, request.path)
	case observeErr == nil && !observed.exists:
		return newFailure(failureIndeterminateCommit, phaseCleanupEntry, request.path, cause, request.path)
	default:
		return newFailure(failureIndeterminateCommit, phaseCleanupEntry, request.path, errors.Join(cause, observeErr), request.path)
	}
}
