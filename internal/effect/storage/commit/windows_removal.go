//go:build windows

package commit

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"golang.org/x/sys/windows"
)

type windowsNamespaceObservation struct {
	exists   bool
	identity windowsEntryIdentityNative
	kind     entryKind
}

func CommitLogicalRemoval(ctx context.Context, request LogicalRemoval) error {
	_, err := CommitLogicalRemovalWithOutcome(ctx, request)
	return err
}

func CommitLogicalRemovalWithOutcome(
	ctx context.Context,
	request LogicalRemoval,
) (mutationfs.CommitOutcome, error) {
	err := commitWindowsLogicalRemoval(ctx, request)
	return outcomeFromError(err), err
}

func commitWindowsLogicalRemoval(ctx context.Context, request LogicalRemoval) error {
	capability := request.capability
	if capability != nil {
		defer capability.Close()
	}
	if ctx == nil {
		return fmt.Errorf("logical removal context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateWindowsRemovalRequest(request); err != nil {
		return windowsFailureBeforeVisibility(phaseValidate, request.path, err)
	}
	var err error
	if capability == nil {
		capability, err = acquireWindowsPathCapability(request.path)
		if err != nil {
			return windowsFailureBeforeVisibility(phaseCaptureIdentity, request.path, err)
		}
		defer capability.Close()
	}
	anchor, err := openWindowsDestinationAnchor(ctx, capability, true)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return windowsFailureBeforeVisibility(phaseCaptureIdentity, request.path, windowsUnsupportedCause(err))
	}
	existing, err := openWindowsObservedEntry(ctx, anchor, true, false, true)
	if existing != nil {
		defer existing.close()
	}
	if err != nil {
		return windowsFailureBeforeVisibility(phaseCaptureIdentity, request.path, windowsUnsupportedCause(err))
	}
	if !existing.identity.sameEntry(request.expected) {
		return windowsFailureBeforeVisibility(phaseRevalidateEntry, request.path, fmt.Errorf("removal entry no longer matches expected identity"))
	}

	residueName := ""
	cleanupName := ""
	if request.names != nil {
		residueName = request.names.Residue()
		cleanupName = request.names.Cleanup()
		for _, name := range []string{residueName, cleanupName} {
			if err := requireWindowsSiblingAbsent(anchor.parentHandle(), name); err != nil {
				return windowsFailureBeforeVisibility(phaseCommitTombstone, request.path, err)
			}
		}
	} else {
		residueName, err = unusedWindowsSiblingName(anchor.parentHandle(), tombstonePrefix)
		if err != nil {
			return windowsFailureBeforeVisibility(phaseCommitTombstone, request.path, err)
		}
	}
	residuePath := filepath.Join(filepath.Dir(request.path), residueName)
	if err := revalidateWindowsObservedEntry(ctx, anchor, existing); err != nil {
		return windowsFailureBeforeVisibility(phaseRevalidateEntry, request.path, err)
	}
	if _, err := renameWindowsByHandle(existing.handle.Handle(), anchor.parentHandle(), residueName, windowsRenameNoReplace); err != nil {
		moved, unchanged, observeErr := observeWindowsNamespaceTransition(
			anchor.parentHandle(),
			anchor.name.String(),
			residueName,
			existing.facts.identity,
		)
		switch {
		case unchanged && observeErr == nil:
			return windowsFailureBeforeVisibility(phaseCommitTombstone, request.path, err)
		case moved:
			return newFailure(failureIndeterminateCommit, phaseCommitTombstone, request.path, errors.Join(err, observeErr), residuePath)
		default:
			return newFailure(failureIndeterminateCommit, phaseCommitTombstone, request.path, errors.Join(err, observeErr), residuePath)
		}
	}
	moved, err := observeWindowsEntryAt(anchor.parentHandle(), residueName)
	if err != nil || !moved.exists || !existing.facts.identity.sameObject(moved.identity) {
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err, residuePath)
	}
	if err := requireWindowsDestinationAbsent(anchor); err != nil {
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err, residuePath)
	}
	if err := flushWindowsHandle(anchor.parentHandle(), windowsFlushPolicy{directory: true}); err != nil {
		return newFailure(failureIndeterminateCommit, phaseSyncParent, request.path, err, residuePath)
	}

	cleanupEntryName := residueName
	cleanupPath := residuePath
	cleanupIdentity := EntryIdentity{
		path:     residuePath,
		kind:     existing.identity.kind,
		platform: platformIdentity{native: moved.identity},
	}
	if cleanupName != "" {
		if _, err := renameWindowsByHandle(existing.handle.Handle(), anchor.parentHandle(), cleanupName, windowsRenameNoReplace); err != nil {
			return newFailure(failureRetainedResidue, phasePromoteCleanup, request.path, err, residuePath)
		}
		cleanupPath = filepath.Join(filepath.Dir(request.path), cleanupName)
		promoted, observeErr := observeWindowsEntryAt(anchor.parentHandle(), cleanupName)
		if observeErr != nil || !promoted.exists || !moved.identity.sameObject(promoted.identity) {
			return newFailure(
				failureIndeterminateCommit,
				phaseVerifyEntry,
				request.path,
				observeErr,
				residuePath,
				cleanupPath,
			)
		}
		if err := requireWindowsSiblingAbsent(anchor.parentHandle(), residueName); err != nil {
			return newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err, residuePath, cleanupPath)
		}
		if err := flushWindowsHandle(anchor.parentHandle(), windowsFlushPolicy{directory: true}); err != nil {
			return newFailure(failureIndeterminateCommit, phaseSyncParent, request.path, err, cleanupPath)
		}
		cleanupEntryName = cleanupName
		cleanupIdentity.path = cleanupPath
		cleanupIdentity.platform = platformIdentity{native: promoted.identity}
	}

	if err := existing.close(); err != nil {
		return newFailure(failureRetainedResidue, phaseClosePayload, request.path, err, cleanupPath)
	}
	limits := defaultTreeTraversalLimits()
	if request.names != nil {
		limits = request.limits
	}
	budget, err := newTreeTraversalBudget(limits)
	if err != nil {
		return newFailure(failureRetainedResidue, phaseCleanupTombstone, request.path, err, cleanupPath)
	}
	if err := removeWindowsEntryTree(
		ctx,
		anchor.parentHandle(),
		cleanupEntryName,
		cleanupPath,
		cleanupIdentity,
		0,
		budget,
	); err != nil {
		return newFailure(failureRetainedResidue, phaseCleanupTombstone, request.path, err, cleanupPath)
	}
	if err := flushWindowsHandle(anchor.parentHandle(), windowsFlushPolicy{directory: true}); err != nil {
		return newFailure(failureRetainedResidue, phaseSyncCleanupParent, request.path, err, cleanupPath)
	}
	if err := anchor.revalidate(context.WithoutCancel(ctx)); err != nil {
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, err)
	}
	return nil
}

func validateWindowsRemovalRequest(request LogicalRemoval) error {
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
			return err
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

func removeWindowsEntryTree(
	ctx context.Context,
	parent windows.Handle,
	name string,
	path string,
	expected EntryIdentity,
	depth int,
	budget *treeTraversalBudget,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := budget.admitDepth(depth); err != nil {
		return err
	}
	access := uint32(windows.FILE_READ_ATTRIBUTES | windows.FILE_READ_EA | windows.READ_CONTROL |
		windows.WRITE_DAC | windows.DELETE | windows.SYNCHRONIZE)
	if expected.kind == entryKindDirectory {
		access |= windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.FILE_TRAVERSE
	}
	opened, err := openWindowsRelativeEntry(
		parent,
		name,
		access,
		windowsParentShareMode,
		windows.FILE_OPEN,
		false,
	)
	if err != nil {
		return err
	}
	defer opened.handle.Close()
	facts, err := queryWindowsEntryFacts(opened.handle.Handle())
	if err != nil {
		return err
	}
	kind := windowsEntryKindFromFacts(facts)
	actual := EntryIdentity{path: path, kind: kind, platform: platformIdentity{native: facts.identity}}
	if !actual.sameEntry(expected) {
		return fmt.Errorf("cleanup entry identity changed for %q", path)
	}
	metadata, err := queryWindowsMetadataFacts(opened.handle.Handle())
	if err != nil {
		return err
	}
	if _, err := windowsCanonicalModeFromSecurity(metadata.security); err != nil {
		return err
	}
	if err := validateWindowsObservedMetadata(metadata); err != nil {
		return err
	}
	if err := validateWindowsCanonicalEntryAttributes(facts.attribute.attributes, kind == entryKindDirectory); err != nil {
		return err
	}
	if kind == entryKindRegular {
		if err := budget.admitBytes(facts.standard.endOfFile); err != nil {
			return err
		}
	}
	if kind == entryKindDirectory {
		first, err := enumerateWindowsDirectoryOnce(ctx, opened.handle.Handle(), budget.remainingEntries()+1)
		if err != nil {
			return err
		}
		if err := budget.admitEntries(len(first)); err != nil {
			return err
		}
		second, err := enumerateWindowsDirectoryOnce(ctx, opened.handle.Handle(), len(first)+1)
		if err != nil {
			return err
		}
		if !equalWindowsDirectoryEntries(first, second) {
			return fmt.Errorf("cleanup directory changed before traversal at %q", path)
		}
		for _, child := range first {
			childKind := entryKindRegular
			if child.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
				switch child.reparseTag {
				case windows.IO_REPARSE_TAG_SYMLINK, windows.IO_REPARSE_TAG_MOUNT_POINT:
					childKind = entryKindSymlink
				default:
					childKind = entryKindSpecial
				}
			} else if child.attributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
				childKind = entryKindDirectory
			}
			if childKind == entryKindSpecial {
				return windowsNativeUnsupported(windowsNativePhaseDisposition, "special cleanup entries are not supported", nil)
			}
			childPath := filepath.Join(path, child.name)
			childIdentity := EntryIdentity{
				path:     childPath,
				kind:     childKind,
				platform: platformIdentity{native: child.identity},
			}
			if err := removeWindowsEntryTree(ctx, opened.handle.Handle(), child.name, childPath, childIdentity, depth+1, budget); err != nil {
				return err
			}
		}
		remaining, err := enumerateWindowsDirectoryOnce(ctx, opened.handle.Handle(), 1)
		if err != nil {
			return err
		}
		if len(remaining) != 0 {
			return fmt.Errorf("cleanup directory is not empty at %q", path)
		}
		if err := flushWindowsHandle(opened.handle.Handle(), windowsFlushPolicy{directory: true}); err != nil {
			return err
		}
	}
	if err := requireWindowsNameMatches(parent, name, facts.identity, false); err != nil {
		return err
	}
	if _, err := disposeWindowsByHandle(opened.handle.Handle(), false); err != nil {
		return err
	}
	if err := opened.handle.Close(); err != nil {
		return err
	}
	opened.handle = nil
	if err := requireWindowsSiblingAbsent(parent, name); err != nil {
		return err
	}
	return nil
}

func observeWindowsEntryAt(parent windows.Handle, name string) (windowsNamespaceObservation, error) {
	opened, err := openWindowsRelativeEntry(
		parent,
		name,
		windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL|windows.SYNCHRONIZE,
		windowsParentShareMode,
		windows.FILE_OPEN,
		false,
	)
	if err != nil {
		if windowsNativeErrorClassOf(err) == windowsNativeErrorNotFound {
			return windowsNamespaceObservation{}, nil
		}
		return windowsNamespaceObservation{}, err
	}
	facts, factsErr := queryWindowsEntryFacts(opened.handle.Handle())
	closeErr := opened.handle.Close()
	if factsErr != nil || closeErr != nil {
		return windowsNamespaceObservation{}, errors.Join(factsErr, closeErr)
	}
	return windowsNamespaceObservation{
		exists:   true,
		identity: facts.identity,
		kind:     windowsEntryKindFromFacts(facts),
	}, nil
}

func observeWindowsNamespaceTransition(
	parent windows.Handle,
	sourceName string,
	destinationName string,
	expected windowsEntryIdentityNative,
) (moved bool, unchanged bool, err error) {
	source, sourceErr := observeWindowsEntryAt(parent, sourceName)
	if sourceErr != nil {
		return false, false, sourceErr
	}
	destination, destinationErr := observeWindowsEntryAt(parent, destinationName)
	if destinationErr != nil {
		return false, false, destinationErr
	}
	if !source.exists && destination.exists && expected.sameObject(destination.identity) {
		return true, false, nil
	}
	if source.exists && expected.equal(source.identity) && !destination.exists {
		return false, true, nil
	}
	return false, false, nil
}

func requireWindowsNameMatches(
	parent windows.Handle,
	name string,
	expected windowsEntryIdentityNative,
	exact bool,
) error {
	observed, err := observeWindowsEntryAt(parent, name)
	if err != nil {
		return err
	}
	if !observed.exists {
		return fmt.Errorf("Windows entry %q is no longer present", name)
	}
	matches := expected.sameObject(observed.identity)
	if exact {
		matches = expected.equal(observed.identity)
	}
	if !matches {
		return fmt.Errorf("Windows entry %q no longer matches its retained handle", name)
	}
	return nil
}

func requireWindowsSiblingAbsent(parent windows.Handle, name string) error {
	observed, err := observeWindowsEntryAt(parent, name)
	if err != nil {
		return err
	}
	if observed.exists {
		return fmt.Errorf("Windows sibling %q is already occupied", name)
	}
	return nil
}

func unusedWindowsSiblingName(parent windows.Handle, prefix string) (string, error) {
	for range maximumWindowsTemporaryNameAttempts {
		name, err := newWindowsPrivateName(prefix)
		if err != nil {
			return "", err
		}
		observed, err := observeWindowsEntryAt(parent, name)
		if err != nil {
			return "", err
		}
		if !observed.exists {
			return name, nil
		}
	}
	return "", fmt.Errorf("cannot allocate a unique Windows private sibling name")
}
