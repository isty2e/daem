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
	limits := defaultTreeTraversalLimits()
	if request.names != nil {
		limits = request.limits
	}
	preflightBudget, err := newTreeTraversalBudget(limits)
	if err != nil {
		return windowsFailureBeforeVisibility(phaseValidate, request.path, err)
	}
	if err := preflightWindowsEntryTree(
		ctx,
		anchor.parentDirectory(),
		anchor.name.String(),
		request.path,
		request.expected,
		0,
		preflightBudget,
		nil,
		faultPlan{},
		"",
	); err != nil {
		return windowsFailureBeforeVisibility(phaseCleanupTombstone, request.path, windowsUnsupportedCause(err))
	}

	residueName := ""
	cleanupName := ""
	if request.names != nil {
		residueName = request.names.Residue()
		cleanupName = request.names.Cleanup()
		for _, name := range []string{residueName, cleanupName} {
			if err := requireWindowsSiblingAbsent(anchor.parentDirectory(), name); err != nil {
				return windowsFailureBeforeVisibility(phaseCommitTombstone, request.path, err)
			}
		}
	} else {
		residueName, err = unusedWindowsSiblingName(anchor.parentDirectory(), tombstonePrefix)
		if err != nil {
			return windowsFailureBeforeVisibility(phaseCommitTombstone, request.path, err)
		}
	}
	residuePath := filepath.Join(filepath.Dir(request.path), residueName)
	if err := revalidateWindowsObservedEntry(ctx, anchor, existing); err != nil {
		return windowsFailureBeforeVisibility(phaseRevalidateEntry, request.path, err)
	}
	if err := ctx.Err(); err != nil {
		return windowsFailureBeforeVisibility(phaseCommitTombstone, request.path, err)
	}
	if _, err := renameWindowsByHandle(existing.handle.Handle(), anchor.parentHandle(), residueName, windowsRenameNoReplace); err != nil {
		moved, unchanged, observeErr := observeWindowsNamespaceTransition(
			anchor.parentDirectory(),
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
	postRenameFacts, factsErr := queryWindowsEntryFacts(existing.handle.Handle())
	moved, err := observeWindowsEntryAt(anchor.parentDirectory(), residueName)
	if factsErr != nil || err != nil || !moved.exists || !postRenameFacts.identity.equal(moved.identity) {
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, request.path, errors.Join(factsErr, err), residuePath)
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
		if err := ctx.Err(); err != nil {
			return newFailure(failureRetainedResidue, phasePromoteCleanup, request.path, err, residuePath)
		}
		if _, err := renameWindowsByHandle(existing.handle.Handle(), anchor.parentHandle(), cleanupName, windowsRenameNoReplace); err != nil {
			return newFailure(failureRetainedResidue, phasePromoteCleanup, request.path, err, residuePath)
		}
		cleanupPath = filepath.Join(filepath.Dir(request.path), cleanupName)
		postPromoteFacts, factsErr := queryWindowsEntryFacts(existing.handle.Handle())
		promoted, observeErr := observeWindowsEntryAt(anchor.parentDirectory(), cleanupName)
		if factsErr != nil || observeErr != nil || !promoted.exists || !postPromoteFacts.identity.equal(promoted.identity) {
			observeErr = errors.Join(factsErr, observeErr)
			return newFailure(
				failureIndeterminateCommit,
				phaseVerifyEntry,
				request.path,
				observeErr,
				residuePath,
				cleanupPath,
			)
		}
		if err := requireWindowsSiblingAbsent(anchor.parentDirectory(), residueName); err != nil {
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
	budget, err := newTreeTraversalBudget(limits)
	if err != nil {
		return newFailure(failureRetainedResidue, phaseCleanupTombstone, request.path, err, cleanupPath)
	}
	if _, err := removeWindowsEntryTree(
		ctx,
		anchor.parentDirectory(),
		cleanupEntryName,
		cleanupPath,
		cleanupIdentity,
		0,
		budget,
		nil,
		faultPlan{},
		"",
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

type windowsTreeWalkMode uint8

const (
	windowsTreeWalkPreflight windowsTreeWalkMode = iota
	windowsTreeWalkRemove
)

func preflightWindowsEntryTree(
	ctx context.Context,
	parent windowsDirectoryHandle,
	name string,
	path string,
	expected EntryIdentity,
	depth int,
	budget *treeTraversalBudget,
	plan *windowsPreparedTreeRemovalPlan,
	faults faultPlan,
	relativePath string,
) error {
	_, err := walkWindowsEntryTree(
		ctx, parent, name, path, expected, depth, budget, plan, faults, relativePath, windowsTreeWalkPreflight,
	)
	return err
}

func removeWindowsEntryTree(
	ctx context.Context,
	parent windowsDirectoryHandle,
	name string,
	path string,
	expected EntryIdentity,
	depth int,
	budget *treeTraversalBudget,
	plan *windowsPreparedTreeRemovalPlan,
	faults faultPlan,
	relativePath string,
) (bool, error) {
	return walkWindowsEntryTree(
		ctx, parent, name, path, expected, depth, budget, plan, faults, relativePath, windowsTreeWalkRemove,
	)
}

// walkWindowsEntryTree visits one expected entry tree. Removal mode reports
// whether any disposition succeeded so callers can classify later failures as
// retained residue instead of claiming no host effect started.
func walkWindowsEntryTree(
	ctx context.Context,
	parent windowsDirectoryHandle,
	name string,
	path string,
	expected EntryIdentity,
	depth int,
	budget *treeTraversalBudget,
	plan *windowsPreparedTreeRemovalPlan,
	faults faultPlan,
	relativePath string,
	mode windowsTreeWalkMode,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := budget.admitDepth(depth); err != nil {
		return false, err
	}
	access := uint32(windows.FILE_READ_ATTRIBUTES | windows.FILE_READ_EA | windows.READ_CONTROL |
		windows.DELETE | windows.SYNCHRONIZE)
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
		return false, err
	}
	defer opened.handle.Close()
	changed := false
	facts, err := queryWindowsEntryFacts(opened.handle.Handle())
	if err != nil {
		return false, err
	}
	kind := windowsEntryKindFromFacts(facts)
	actual := EntryIdentity{path: path, kind: kind, platform: platformIdentity{native: facts.identity}}
	if !actual.sameEntry(expected) {
		return false, fmt.Errorf("cleanup entry identity changed for %q", path)
	}
	if plan != nil && relativePath != "" {
		if err := plan.validate(relativePath, kind, facts); err != nil {
			return false, err
		}
	}
	metadata, err := queryWindowsMetadataFacts(opened.handle.Handle())
	if err != nil {
		return false, err
	}
	if _, err := windowsCanonicalModeFromSecurity(metadata.security); err != nil {
		return false, err
	}
	if err := validateWindowsObservedMetadata(metadata); err != nil {
		return false, err
	}
	if kind == entryKindRegular || kind == entryKindDirectory {
		if err := validateWindowsCanonicalEntryAttributes(facts.attribute.attributes, kind == entryKindDirectory); err != nil {
			return false, err
		}
	}
	if kind == entryKindRegular {
		if err := budget.admitBytes(facts.standard.endOfFile); err != nil {
			return false, err
		}
	}
	if kind == entryKindDirectory {
		first, err := enumerateWindowsDirectoryOnce(ctx, opened.handle.Handle(), budget.remainingEntries()+1)
		if err != nil {
			return false, err
		}
		if err := budget.admitEntries(len(first)); err != nil {
			return false, err
		}
		second, err := enumerateWindowsDirectoryOnce(ctx, opened.handle.Handle(), len(first)+1)
		if err != nil {
			return false, err
		}
		if !equalWindowsDirectoryEntries(first, second) {
			return false, fmt.Errorf("cleanup directory changed before traversal at %q", path)
		}
		if err := plan.validateChildren(relativePath, first); err != nil {
			return false, err
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
				return changed, windowsNativeUnsupported(windowsNativePhaseDisposition, "special cleanup entries are not supported", nil)
			}
			childPath := filepath.Join(path, child.name)
			childRelativePath := child.name
			if relativePath != "" {
				childRelativePath = relativePath + "/" + child.name
			}
			childIdentity := EntryIdentity{
				path:     childPath,
				kind:     childKind,
				platform: platformIdentity{native: child.identity},
			}
			childDirectory, err := opened.Directory()
			if err != nil {
				return changed, err
			}
			childChanged, err := walkWindowsEntryTree(
				ctx,
				childDirectory,
				child.name,
				childPath,
				childIdentity,
				depth+1,
				budget,
				plan,
				faults,
				childRelativePath,
				mode,
			)
			if childChanged {
				changed = true
			}
			if err != nil {
				return changed, err
			}
		}
		remainingLimit := 1
		if mode == windowsTreeWalkPreflight {
			remainingLimit = len(first) + 1
		}
		remaining, err := enumerateWindowsDirectoryOnce(ctx, opened.handle.Handle(), remainingLimit)
		if err != nil {
			return changed, err
		}
		if mode == windowsTreeWalkRemove && len(remaining) != 0 {
			return changed, fmt.Errorf("cleanup directory is not empty at %q", path)
		}
		if mode == windowsTreeWalkPreflight && !equalWindowsDirectoryEntries(first, remaining) {
			return changed, fmt.Errorf("cleanup directory changed during preflight at %q", path)
		}
		if mode == windowsTreeWalkRemove {
			if err := flushWindowsHandle(opened.handle.Handle(), windowsFlushPolicy{directory: true}); err != nil {
				return changed, err
			}
		}
	}
	if err := requireWindowsNameMatches(parent, name, facts.identity, false); err != nil {
		return changed, err
	}
	if mode == windowsTreeWalkPreflight {
		return false, nil
	}
	if err := faults.check(ctx, phaseCleanupEntry); err != nil {
		return changed, err
	}
	if err := ctx.Err(); err != nil {
		return changed, err
	}
	if _, err := disposeWindowsByHandle(opened.handle.Handle(), false); err != nil {
		return changed, err
	}
	changed = true
	if err := opened.handle.Close(); err != nil {
		return changed, err
	}
	opened.handle = nil
	if err := requireWindowsSiblingAbsent(parent, name); err != nil {
		return changed, err
	}
	return changed, nil
}

func observeWindowsEntryAt(parent windowsDirectoryHandle, name string) (windowsNamespaceObservation, error) {
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
	parent windowsDirectoryHandle,
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
	if !source.exists && destination.exists && expected.sameRetainedFile(destination.identity) {
		return true, false, nil
	}
	if source.exists && expected.equal(source.identity) && !destination.exists {
		return false, true, nil
	}
	return false, false, nil
}

func requireWindowsNameMatches(
	parent windowsDirectoryHandle,
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

func requireWindowsSiblingAbsent(parent windowsDirectoryHandle, name string) error {
	observed, err := observeWindowsEntryAt(parent, name)
	if err != nil {
		return err
	}
	if observed.exists {
		return fmt.Errorf("Windows sibling %q is already occupied", name)
	}
	return nil
}

func unusedWindowsSiblingName(parent windowsDirectoryHandle, prefix string) (string, error) {
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
