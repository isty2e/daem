//go:build windows

package commit

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func CommitPreparedTree(ctx context.Context, request PreparedTreeCommit) error {
	if ctx == nil {
		return windowsFailureBeforeVisibility(phaseValidate, request.destination, fmt.Errorf("prepared tree context is required"))
	}
	if err := ctx.Err(); err != nil {
		return windowsFailureBeforeVisibility(phaseValidate, request.destination, err)
	}
	if err := validateWindowsPreparedTreeRequest(request); err != nil {
		return windowsFailureBeforeVisibility(phaseValidate, request.destination, err)
	}
	capability, err := acquireWindowsPathCapability(request.destination)
	if err != nil {
		return windowsFailureBeforeVisibility(phaseValidate, request.destination, err)
	}
	defer capability.Close()
	anchor, err := openWindowsDestinationAnchor(ctx, capability, true)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return windowsFailureBeforeVisibility(phaseValidate, request.destination, windowsUnsupportedCause(err))
	}
	if err := requireWindowsDestinationAbsent(anchor); err != nil {
		return windowsFailureBeforeVisibility(phaseValidate, request.destination, err)
	}
	stageName, err := parseWindowsComponent(filepath.Base(request.stagedRoot))
	if err != nil {
		return windowsFailureBeforeVisibility(phaseValidate, request.destination, err)
	}
	stageAnchor := *anchor
	stageAnchor.name = stageName
	stageAnchor.path = request.stagedRoot
	stage, err := openWindowsObservedEntry(ctx, &stageAnchor, true, false, true)
	if stage != nil {
		defer stage.close()
	}
	if err != nil {
		return windowsFailureBeforeVisibility(phaseValidate, request.destination, windowsUnsupportedCause(err))
	}
	if stage.identity.kind != entryKindDirectory || !stage.identity.sameEntry(request.expected) {
		return windowsFailureBeforeVisibility(phaseValidate, request.destination, fmt.Errorf("prepared tree no longer matches its expected identity"))
	}
	if err := stage.close(); err != nil {
		return windowsFailureBeforeVisibility(phaseClosePayload, request.destination, err)
	}
	writableStage, err := openWindowsRelativeDirectory(
		anchor.parentDirectory(),
		stageName.String(),
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.FILE_TRAVERSE|windows.FILE_READ_EA|
			windows.READ_CONTROL|windows.WRITE_DAC|windows.DELETE,
		windowsPublicationShareMode,
		windows.FILE_OPEN,
		false,
	)
	if err != nil {
		return windowsFailureBeforeVisibility(phaseRevalidateEntry, request.destination, err)
	}
	defer writableStage.handle.Close()
	writableFacts, err := queryWindowsEntryFacts(writableStage.handle.Handle())
	if err != nil || !stage.facts.identity.equal(writableFacts.identity) {
		return windowsFailureBeforeVisibility(
			phaseRevalidateEntry,
			request.destination,
			errors.Join(err, fmt.Errorf("prepared tree changed while acquiring flush authority")),
		)
	}
	budget, err := newTreeTraversalBudget(defaultTreeTraversalLimits())
	if err != nil {
		return windowsFailureBeforeVisibility(phaseValidate, request.destination, err)
	}
	writableDirectory, err := writableStage.Directory()
	if err != nil {
		return windowsFailureBeforeVisibility(phaseValidate, request.destination, err)
	}
	if err := syncValidateWindowsDirectoryTree(ctx, writableDirectory, request.stagedRoot, 0, budget); err != nil {
		return windowsFailureBeforeVisibility(errorPhase(err, phaseSyncTreeDirectory), request.destination, windowsUnsupportedCause(err))
	}
	if err := requireWindowsNameMatches(
		anchor.parentDirectory(),
		stageName.String(),
		writableFacts.identity,
		true,
	); err != nil {
		return windowsFailureBeforeVisibility(phaseRevalidateEntry, request.destination, err)
	}
	if err := requireWindowsDestinationAbsent(anchor); err != nil {
		return windowsFailureBeforeVisibility(phaseRevalidateEntry, request.destination, err)
	}
	if err := ctx.Err(); err != nil {
		return windowsFailureBeforeVisibility(phaseCommitEntry, request.destination, err)
	}
	if _, err := renameWindowsByHandle(writableStage.handle.Handle(), anchor.parentHandle(), anchor.name.String(), windowsRenameNoReplace); err != nil {
		moved, unchanged, observeErr := observeWindowsNamespaceTransition(
			anchor.parentDirectory(), stageName.String(), anchor.name.String(), writableFacts.identity,
		)
		if unchanged && observeErr == nil {
			return windowsFailureBeforeVisibility(phaseCommitEntry, request.destination, err)
		}
		residue := []string(nil)
		if moved {
			residue = []string{request.destination}
		}
		return newFailure(failureIndeterminateCommit, phaseCommitEntry, request.destination, errors.Join(err, observeErr), residue...)
	}
	postRenameFacts, factsErr := queryWindowsEntryFacts(writableStage.handle.Handle())
	published, observeErr := observeWindowsEntryAt(anchor.parentDirectory(), anchor.name.String())
	if factsErr != nil || observeErr != nil || !published.exists || !postRenameFacts.identity.equal(published.identity) {
		observeErr = errors.Join(factsErr, observeErr)
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.destination, observeErr, request.destination)
	}
	if err := requireWindowsSiblingAbsent(anchor.parentDirectory(), stageName.String()); err != nil {
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.destination, err, request.destination)
	}
	if err := flushWindowsHandle(anchor.parentHandle(), windowsFlushPolicy{directory: true}); err != nil {
		return newFailure(failureIndeterminateCommit, phaseSyncParent, request.destination, err, request.destination)
	}
	if err := anchor.revalidate(context.WithoutCancel(ctx)); err != nil {
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.destination, err, request.destination)
	}
	if err := writableStage.handle.Close(); err != nil {
		return newFailure(
			failureIndeterminateCommit,
			phaseClosePayload,
			request.destination,
			err,
			request.destination,
		)
	}
	return nil
}

func validateWindowsPreparedTreeRequest(request PreparedTreeCommit) error {
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

func syncValidateWindowsDirectoryTree(
	ctx context.Context,
	directory windowsDirectoryHandle,
	path string,
	depth int,
	budget *treeTraversalBudget,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := budget.admitDepth(depth); err != nil {
		return err
	}
	facts, err := queryWindowsEntryFacts(directory.Handle())
	if err != nil {
		return atPhase(phaseValidate, err)
	}
	if !facts.standard.directory || facts.attribute.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return atPhase(phaseValidate, fmt.Errorf("prepared Windows tree entry %q is not a non-reparse directory", path))
	}
	metadata, err := queryWindowsMetadataFacts(directory.Handle())
	if err != nil {
		return atPhase(phaseCaptureMetadata, err)
	}
	if err := validateWindowsObservedMetadata(metadata); err != nil {
		return atPhase(phaseCaptureMetadata, err)
	}
	if err := validateWindowsCanonicalEntryAttributes(facts.attribute.attributes, true); err != nil {
		return atPhase(phaseCaptureMetadata, err)
	}
	mode, err := windowsCanonicalModeFromSecurity(metadata.security)
	if err != nil {
		return atPhase(phaseCaptureMetadata, err)
	}
	if err := validateWindowsCanonicalDirectoryMode(mode); err != nil {
		return atPhase(phaseCaptureMetadata, err)
	}
	entries, err := enumerateWindowsDirectoryOnce(ctx, directory.Handle(), budget.remainingEntries()+1)
	if err != nil {
		return atPhase(phaseValidate, err)
	}
	if err := budget.admitEntries(len(entries)); err != nil {
		return atPhase(phaseValidate, err)
	}
	for _, entry := range entries {
		entryPath := filepath.Join(path, entry.name)
		if entry.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return atPhase(phaseValidate, windowsNativeUnsupported(windowsNativePhaseMetadata, fmt.Sprintf("prepared tree contains reparse entry %q", entryPath), nil))
		}
		kind := windowsRelativeFile
		access := uint32(windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.FILE_READ_EA |
			windows.READ_CONTROL | windows.SYNCHRONIZE)
		if entry.attributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
			kind = windowsRelativeDirectory
			access |= windows.FILE_TRAVERSE
		}
		opened, err := openWindowsRelativeChild(
			directory,
			entry.name,
			access,
			windowsPublicationShareMode,
			windows.FILE_OPEN,
			kind,
			false,
		)
		if err != nil {
			return atPhase(phaseValidate, err)
		}
		childFacts, factsErr := queryWindowsEntryFacts(opened.handle.Handle())
		childMetadata, metadataErr := queryWindowsMetadataFacts(opened.handle.Handle())
		if factsErr != nil || metadataErr != nil || !entry.identity.equal(childFacts.identity) {
			_ = opened.handle.Close()
			return atPhase(phaseValidate, errors.Join(factsErr, metadataErr, fmt.Errorf("prepared tree entry changed at %q", entryPath)))
		}
		childMode, modeErr := windowsCanonicalModeFromSecurity(childMetadata.security)
		observedMetadataErr := validateWindowsObservedMetadata(childMetadata)
		attributesErr := validateWindowsCanonicalEntryAttributes(
			childFacts.attribute.attributes,
			kind == windowsRelativeDirectory,
		)
		if modeErr != nil || observedMetadataErr != nil || attributesErr != nil {
			_ = opened.handle.Close()
			return atPhase(phaseCaptureMetadata, errors.Join(modeErr, observedMetadataErr, attributesErr))
		}
		if kind == windowsRelativeDirectory {
			if err := validateWindowsCanonicalDirectoryMode(childMode); err != nil {
				_ = opened.handle.Close()
				return atPhase(phaseCaptureMetadata, err)
			}
			childDirectory, directoryErr := opened.Directory()
			if directoryErr != nil {
				_ = opened.handle.Close()
				return atPhase(phaseValidate, directoryErr)
			}
			err = syncValidateWindowsDirectoryTree(ctx, childDirectory, entryPath, depth+1, budget)
		} else {
			if childFacts.standard.numberOfLinks != 1 {
				_ = opened.handle.Close()
				return atPhase(
					phaseValidate,
					windowsNativeUnsupported(
						windowsNativePhaseIdentity,
						fmt.Sprintf("prepared tree file %q has %d hard links", entryPath, childFacts.standard.numberOfLinks),
						nil,
					),
				)
			}
			if err := validateWindowsCanonicalFileMode(childMode); err != nil {
				_ = opened.handle.Close()
				return atPhase(phaseCaptureMetadata, err)
			}
			if err := budget.admitBytes(childFacts.standard.endOfFile); err != nil {
				_ = opened.handle.Close()
				return atPhase(phaseValidate, err)
			}
			err = flushWindowsHandle(opened.handle.Handle(), windowsFlushPolicy{})
		}
		closeErr := opened.handle.Close()
		if err != nil || closeErr != nil {
			return atPhase(phaseSyncTreeFile, errors.Join(err, closeErr))
		}
	}
	second, err := enumerateWindowsDirectoryOnce(ctx, directory.Handle(), len(entries)+1)
	if err != nil || !equalWindowsDirectoryEntries(entries, second) {
		return atPhase(phaseRevalidateEntry, errors.Join(err, fmt.Errorf("prepared tree changed while syncing %q", path)))
	}
	if err := flushWindowsHandle(directory.Handle(), windowsFlushPolicy{directory: true}); err != nil {
		return atPhase(phaseSyncTreeDirectory, err)
	}
	return nil
}
