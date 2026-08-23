//go:build windows

package commit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

const maximumWindowsTemporaryNameAttempts = 16

func commitWindowsFile(ctx context.Context, request FileCommit) (EntryIdentity, error) {
	return commitWindowsFileWithFaults(ctx, request, faultPlan{})
}

func commitWindowsFileWithFaults(
	ctx context.Context,
	request FileCommit,
	faults faultPlan,
) (EntryIdentity, error) {
	capability := request.capability
	rootedRequest := capability != nil
	if capability != nil {
		defer capability.Close()
	}
	if ctx == nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(
			phaseValidate,
			request.path,
			fmt.Errorf("file commit context is required"),
		)
	}
	if err := ctx.Err(); err != nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseValidate, request.path, err)
	}
	if err := validateCommitPath(request.path); err != nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseValidate, request.path, err)
	}
	if request.policy != filePolicyMustBeAbsent && request.policy != filePolicyReplaceExpected {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseValidate, request.path, fmt.Errorf("file commit policy is invalid"))
	}
	if err := validateWindowsCanonicalFileMode(request.mode); err != nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseValidate, request.path, windowsUnsupportedCause(err))
	}
	canonical, err := buildWindowsCanonicalSecurity(request.mode)
	if err != nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseValidate, request.path, windowsUnsupportedCause(err))
	}

	if capability == nil {
		capability, err = acquireWindowsPathCapability(request.path)
		if err != nil {
			return EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, request.path, err)
		}
		defer capability.Close()
	}
	if rootedRequest {
		if err := validateRootedCapability(request.path, capability); err != nil {
			return EntryIdentity{}, windowsFailureBeforeVisibility(phaseValidate, request.path, err)
		}
	}
	anchor, err := openWindowsDestinationAnchor(ctx, capability, true)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, request.path, windowsUnsupportedCause(err))
	}
	if request.expectedParent.valid() {
		if request.expectedParent.path != filepath.Dir(request.path) || request.expectedParent.kind != entryKindDirectory ||
			!request.expectedParent.platform.native.equal(anchor.parentFacts.identity) {
			return EntryIdentity{}, windowsFailureBeforeVisibility(
				phaseRevalidateEntry,
				request.path,
				fmt.Errorf("destination parent identity changed before file commit"),
			)
		}
	}

	existing, err := observeWindowsFileCommitDestination(ctx, anchor, request)
	if existing != nil {
		defer existing.close()
	}
	if err != nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseRevalidateEntry, request.path, windowsUnsupportedCause(err))
	}
	if err := faults.check(ctx, phaseCreateTemporary); err != nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseCreateTemporary, request.path, err)
	}
	stageName, stage, err := createWindowsFileStage(anchor, canonical)
	if err != nil {
		if stageName != "" {
			observed, observeErr := observeWindowsEntryAt(anchor.parentHandle(), stageName)
			if observed.exists || observeErr != nil {
				return EntryIdentity{}, newFailure(
					failureIndeterminateCommit,
					phaseCreateTemporary,
					request.path,
					errors.Join(err, observeErr),
				)
			}
		}
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseCreateTemporary, request.path, err)
	}
	stagePath := filepath.Join(filepath.Dir(request.path), stageName)
	defer func() {
		if stage != nil {
			_ = stage.handle.Close()
		}
	}()

	cleanupStage := func(primary error, failedPhase phase) (EntryIdentity, error) {
		stageIdentity := windowsEntryIdentityNative{}
		if stage != nil && stage.handle != nil {
			if facts, factsErr := queryWindowsEntryFacts(stage.handle.Handle()); factsErr == nil {
				stageIdentity = facts.identity
			}
		}
		cleanupErr := faults.check(context.WithoutCancel(ctx), phaseCleanupTemporary)
		if cleanupErr == nil {
			cleanupErr = cleanupWindowsFileStage(anchor, stageName, stage)
		}
		stage = nil
		if cleanupErr != nil {
			observed, observeErr := observeWindowsEntryAt(anchor.parentHandle(), stageName)
			if observeErr == nil && observed.exists && stageIdentity.sameObject(observed.identity) {
				return EntryIdentity{}, newFailure(
					failureRetainedResidue,
					failedPhase,
					request.path,
					errors.Join(primary, cleanupErr),
					stagePath,
				)
			}
			return EntryIdentity{}, newFailure(
				failureIndeterminateCommit,
				failedPhase,
				request.path,
				errors.Join(primary, cleanupErr, observeErr),
			)
		}
		return EntryIdentity{}, windowsFailureBeforeVisibility(failedPhase, request.path, windowsUnsupportedCause(primary))
	}

	if err := faults.check(ctx, phaseWritePayload); err != nil {
		return cleanupStage(err, phaseWritePayload)
	}
	if err := writeWindowsPayload(ctx, stage.handle.Handle(), request.payload); err != nil {
		return cleanupStage(err, phaseWritePayload)
	}
	if err := faults.check(ctx, phaseSyncPayload); err != nil {
		return cleanupStage(err, phaseSyncPayload)
	}
	if err := flushWindowsHandle(stage.handle.Handle(), windowsFlushPolicy{}); err != nil {
		return cleanupStage(err, phaseSyncPayload)
	}
	stageFacts, err := queryWindowsEntryFacts(stage.handle.Handle())
	if err != nil {
		return cleanupStage(err, phaseRevalidateEntry)
	}
	if err := faults.check(ctx, phaseCaptureMetadata); err != nil {
		return cleanupStage(err, phaseCaptureMetadata)
	}
	stageMetadata, err := queryWindowsMetadataFacts(stage.handle.Handle())
	if err != nil {
		return cleanupStage(err, phaseCaptureMetadata)
	}
	if stageFacts.standard.directory || stageFacts.standard.endOfFile != int64(len(request.payload)) {
		return cleanupStage(fmt.Errorf("Windows file stage payload facts are inconsistent"), phaseRevalidateEntry)
	}
	if err := validateWindowsCanonicalEntryAttributes(stageFacts.attribute.attributes, false); err != nil {
		return cleanupStage(err, phaseApplyMetadata)
	}
	if err := ensureWindowsCanonicalMetadataSupported(stageMetadata, canonical.facts); err != nil {
		return cleanupStage(err, phaseApplyMetadata)
	}
	if err := faults.check(ctx, phaseClosePayload); err != nil {
		return cleanupStage(err, phaseClosePayload)
	}
	if err := stage.handle.Close(); err != nil {
		stage = nil
		return EntryIdentity{}, classifyWindowsStageReopenFailure(anchor, stageName, stageFacts.identity, request.path, stagePath, err)
	}
	stage = nil
	stage, err = openWindowsRelativeFile(
		anchor.parentHandle(),
		stageName,
		windows.FILE_GENERIC_READ|windows.FILE_READ_EA|windows.READ_CONTROL|windows.DELETE|windows.WRITE_DAC,
		windowsPublicationShareMode,
		windows.FILE_OPEN,
		true,
	)
	if err != nil {
		return EntryIdentity{}, classifyWindowsStageReopenFailure(anchor, stageName, stageFacts.identity, request.path, stagePath, err)
	}
	reopenedFacts, factsErr := queryWindowsEntryFacts(stage.handle.Handle())
	reopenedMetadata, metadataErr := queryWindowsMetadataFacts(stage.handle.Handle())
	if factsErr != nil || metadataErr != nil || !stageFacts.identity.equal(reopenedFacts.identity) ||
		reopenedFacts.standard.directory || reopenedFacts.standard.endOfFile != int64(len(request.payload)) {
		return cleanupStage(
			errors.Join(factsErr, metadataErr, fmt.Errorf("reopened Windows file stage changed after payload close")),
			phaseRevalidateEntry,
		)
	}
	if err := validateWindowsCanonicalEntryAttributes(reopenedFacts.attribute.attributes, false); err != nil {
		return cleanupStage(err, phaseApplyMetadata)
	}
	if err := ensureWindowsCanonicalMetadataSupported(reopenedMetadata, canonical.facts); err != nil {
		return cleanupStage(err, phaseApplyMetadata)
	}
	stageFacts = reopenedFacts
	if err := faults.check(ctx, phaseRevalidateEntry); err != nil {
		return cleanupStage(err, phaseRevalidateEntry)
	}
	if err := anchor.revalidate(ctx); err != nil {
		return cleanupStage(err, phaseRevalidateEntry)
	}
	if err := requireWindowsNameMatches(anchor.parentHandle(), stageName, stageFacts.identity, true); err != nil {
		return cleanupStage(err, phaseRevalidateEntry)
	}
	if existing != nil {
		if err := revalidateWindowsObservedEntry(ctx, anchor, existing); err != nil {
			return cleanupStage(err, phaseRevalidateEntry)
		}
	} else if err := requireWindowsDestinationAbsent(anchor); err != nil {
		return cleanupStage(err, phaseRevalidateEntry)
	}

	renameMode := windowsRenameNoReplace
	if request.policy == filePolicyReplaceExpected {
		renameMode = windowsRenameReplace
	}
	if err := faults.check(ctx, phaseCommitEntry); err != nil {
		return cleanupStage(err, phaseCommitEntry)
	}
	if err := anchor.revalidate(ctx); err != nil {
		return cleanupStage(err, phaseCommitEntry)
	}
	if existing != nil {
		if err := revalidateWindowsObservedEntry(ctx, anchor, existing); err != nil {
			return cleanupStage(err, phaseCommitEntry)
		}
	} else if err := requireWindowsDestinationAbsent(anchor); err != nil {
		return cleanupStage(err, phaseCommitEntry)
	}
	if _, err := renameWindowsByHandle(stage.handle.Handle(), anchor.parentHandle(), anchor.name.String(), renameMode); err != nil {
		visible, stable, observationErr := observeWindowsRenameFailure(anchor, stageName, stageFacts.identity, existing)
		if visible || !stable || observationErr != nil {
			return EntryIdentity{}, newFailure(
				failureIndeterminateCommit,
				phaseCommitEntry,
				request.path,
				errors.Join(err, observationErr),
			)
		}
		return cleanupStage(err, phaseCommitEntry)
	}
	if err := faults.check(ctx, phaseVerifyEntry); err != nil {
		return EntryIdentity{}, newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err)
	}
	postRenameStageFacts, err := queryWindowsEntryFacts(stage.handle.Handle())
	if err != nil {
		return EntryIdentity{}, newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err)
	}
	published, err := openWindowsRelativeFile(
		anchor.parentHandle(),
		anchor.name.String(),
		windows.FILE_GENERIC_READ|windows.FILE_READ_EA|windows.READ_CONTROL|windows.DELETE|windows.WRITE_DAC,
		windowsParentShareMode,
		windows.FILE_OPEN,
		false,
	)
	if err != nil {
		return EntryIdentity{}, newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err)
	}
	publishedFacts, factsErr := queryWindowsEntryFacts(published.handle.Handle())
	publishedMetadata, metadataErr := queryWindowsMetadataFacts(published.handle.Handle())
	closePublishedErr := published.handle.Close()
	if factsErr != nil || metadataErr != nil || closePublishedErr != nil {
		return EntryIdentity{}, newFailure(
			failureIndeterminateCommit,
			phaseVerifyEntry,
			request.path,
			joinWindowsErrors(factsErr, metadataErr, closePublishedErr),
		)
	}
	if !postRenameStageFacts.identity.equal(publishedFacts.identity) || publishedFacts.standard.directory ||
		publishedFacts.standard.endOfFile != int64(len(request.payload)) {
		return EntryIdentity{}, newFailure(
			failureIndeterminateCommit,
			phaseVerifyEntry,
			request.path,
			fmt.Errorf("published Windows file does not match its private stage"),
		)
	}
	if err := validateWindowsCanonicalEntryAttributes(publishedFacts.attribute.attributes, false); err != nil {
		return EntryIdentity{}, newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err)
	}
	if err := ensureWindowsCanonicalMetadataSupported(publishedMetadata, canonical.facts); err != nil {
		return EntryIdentity{}, newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err)
	}
	if err := requireWindowsSiblingAbsent(anchor.parentHandle(), stageName); err != nil {
		return EntryIdentity{}, newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err)
	}
	if err := anchor.revalidate(ctx); err != nil {
		return EntryIdentity{}, newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err)
	}
	if err := faults.check(ctx, phaseSyncParent); err != nil {
		return EntryIdentity{}, newFailure(failureIndeterminateCommit, phaseSyncParent, request.path, err)
	}
	if err := flushWindowsHandle(anchor.parentHandle(), windowsFlushPolicy{directory: true}); err != nil {
		return EntryIdentity{}, newFailure(failureIndeterminateCommit, phaseSyncParent, request.path, err)
	}
	if err := stage.handle.Close(); err != nil {
		stage = nil
		return EntryIdentity{}, newFailure(failureIndeterminateCommit, phaseClosePayload, request.path, err)
	}
	stage = nil
	return EntryIdentity{
		path:     request.path,
		kind:     entryKindRegular,
		platform: platformIdentity{native: publishedFacts.identity},
	}, nil
}

func observeWindowsFileCommitDestination(
	ctx context.Context,
	anchor *windowsDestinationAnchor,
	request FileCommit,
) (*windowsObservedEntry, error) {
	if request.policy == filePolicyMustBeAbsent {
		if err := requireWindowsDestinationAbsent(anchor); err != nil {
			return nil, err
		}
		return nil, nil
	}
	observed, err := openWindowsObservedEntry(ctx, anchor, true, false, true)
	if err != nil {
		return nil, err
	}
	if observed.identity.kind != entryKindRegular || !observed.identity.sameEntry(request.expected) {
		_ = observed.close()
		return nil, fmt.Errorf("destination no longer matches the expected regular file")
	}
	return observed, nil
}

func requireWindowsDestinationAbsent(anchor *windowsDestinationAnchor) error {
	opened, err := openWindowsRelativeEntry(
		anchor.parentHandle(),
		anchor.name.String(),
		windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL|windows.SYNCHRONIZE,
		windowsParentShareMode,
		windows.FILE_OPEN,
		false,
	)
	if err != nil {
		if windowsNativeErrorClassOf(err) == windowsNativeErrorNotFound {
			return nil
		}
		return err
	}
	_ = opened.handle.Close()
	return fmt.Errorf("destination already exists")
}

func createWindowsFileStage(
	anchor *windowsDestinationAnchor,
	canonical *windowsCanonicalSecurity,
) (string, *windowsRelativeOpen, error) {
	for range maximumWindowsTemporaryNameAttempts {
		name, err := newWindowsPrivateName(temporaryPrefix)
		if err != nil {
			return "", nil, err
		}
		opened, err := createWindowsRelativeFile(
			anchor.parentHandle(),
			name,
			windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.FILE_GENERIC_EXECUTE|windows.DELETE|windows.WRITE_DAC|windows.READ_CONTROL,
			windowsPublicationShareMode,
			true,
			canonical.descriptor,
		)
		if err == nil {
			return name, opened, nil
		}
		if windowsNativeErrorClassOf(err) != windowsNativeErrorCollision {
			return name, nil, err
		}
	}
	return "", nil, fmt.Errorf("cannot allocate a unique Windows file stage name")
}

func cleanupWindowsFileStage(
	anchor *windowsDestinationAnchor,
	stageName string,
	stage *windowsRelativeOpen,
) error {
	if stage == nil || stage.handle == nil {
		return nil
	}
	var failures []error
	facts, factsErr := queryWindowsEntryFacts(stage.handle.Handle())
	if factsErr != nil {
		failures = append(failures, factsErr)
	} else if err := requireWindowsNameMatches(anchor.parentHandle(), stageName, facts.identity, false); err != nil {
		failures = append(failures, err)
	} else if _, err := disposeWindowsByHandle(stage.handle.Handle(), false); err != nil {
		failures = append(failures, err)
	}
	if err := stage.handle.Close(); err != nil {
		failures = append(failures, err)
	}
	if err := flushWindowsHandle(anchor.parentHandle(), windowsFlushPolicy{directory: true}); err != nil {
		failures = append(failures, err)
	}
	opened, err := openWindowsRelativeEntry(
		anchor.parentHandle(),
		stageName,
		windows.FILE_READ_ATTRIBUTES,
		windowsParentShareMode,
		windows.FILE_OPEN,
		false,
	)
	if err == nil {
		_ = opened.handle.Close()
		failures = append(failures, fmt.Errorf("Windows file stage remains visible after cleanup"))
	} else if windowsNativeErrorClassOf(err) != windowsNativeErrorNotFound {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func classifyWindowsStageReopenFailure(
	anchor *windowsDestinationAnchor,
	stageName string,
	expected windowsEntryIdentityNative,
	path string,
	stagePath string,
	cause error,
) error {
	observed, observeErr := observeWindowsEntryAt(anchor.parentHandle(), stageName)
	if observeErr == nil && observed.exists && expected.sameObject(observed.identity) {
		return newFailure(
			failureRetainedResidue,
			phaseClosePayload,
			path,
			errors.Join(cause, observeErr),
			stagePath,
		)
	}
	return newFailure(
		failureIndeterminateCommit,
		phaseClosePayload,
		path,
		errors.Join(cause, observeErr),
	)
}

func observeWindowsRenameFailure(
	anchor *windowsDestinationAnchor,
	stageName string,
	stageIdentity windowsEntryIdentityNative,
	existing *windowsObservedEntry,
) (visible bool, stable bool, err error) {
	destination, destinationErr := openWindowsRelativeEntry(
		anchor.parentHandle(),
		anchor.name.String(),
		windows.FILE_READ_ATTRIBUTES,
		windowsParentShareMode,
		windows.FILE_OPEN,
		false,
	)
	destinationStable := false
	if destinationErr == nil {
		facts, factsErr := queryWindowsEntryFacts(destination.handle.Handle())
		closeErr := destination.handle.Close()
		if factsErr != nil || closeErr != nil {
			return false, false, joinWindowsErrors(factsErr, closeErr)
		}
		if stageIdentity.sameRetainedFile(facts.identity) {
			return true, false, nil
		}
		destinationStable = existing == nil || existing.facts.identity.equal(facts.identity)
	} else if windowsNativeErrorClassOf(destinationErr) != windowsNativeErrorNotFound {
		return false, false, destinationErr
	} else {
		destinationStable = existing == nil
	}
	stage, stageErr := openWindowsRelativeEntry(
		anchor.parentHandle(),
		stageName,
		windows.FILE_READ_ATTRIBUTES,
		windowsParentShareMode,
		windows.FILE_OPEN,
		false,
	)
	if stageErr != nil {
		return false, false, stageErr
	}
	facts, factsErr := queryWindowsEntryFacts(stage.handle.Handle())
	closeErr := stage.handle.Close()
	if factsErr != nil || closeErr != nil {
		return false, false, joinWindowsErrors(factsErr, closeErr)
	}
	return false, destinationStable && stageIdentity.equal(facts.identity), nil
}

func newWindowsPrivateName(prefix string) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(random[:]), nil
}

func windowsFailureBeforeVisibility(current phase, path string, err error) error {
	return failureBeforeVisibility(current, path, windowsUnsupportedCause(err))
}

func windowsUnsupportedCause(err error) error {
	if err == nil || isUnsupported(err) {
		return err
	}
	if errors.Is(err, errWindowsNativeUnsupported) {
		return unsupported("Windows storage guarantee is unavailable", err)
	}
	return err
}
