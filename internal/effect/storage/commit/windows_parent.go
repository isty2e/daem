//go:build windows

package commit

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/windows"
)

type windowsCreatedDirectory struct {
	path                     string
	name                     string
	identity                 EntryIdentity
	parent                   *windowsOwnedHandle
	parentCaseSensitive      bool
	handle                   *windowsOwnedHandle
	cleanupState             windowsCreatedDirectoryCleanupState
	pendingDurabilityFailure error
}

type windowsCreatedDirectoryCleanupState uint8

const (
	windowsCreatedDirectoryCleanupActive windowsCreatedDirectoryCleanupState = iota
	windowsCreatedDirectoryCleanupPendingDurability
	windowsCreatedDirectoryCleanupRetired
)

type windowsAncestorCleanupState struct {
	directories []windowsCreatedDirectory
	closed      bool
}

// AncestorCleanup retains exact Windows handles for directories created by
// operations run through it. It can remove only those objects while empty.
type AncestorCleanup struct {
	state *windowsAncestorCleanupState
}

func (cleanup *AncestorCleanup) PrepareParent(ctx context.Context, path string) error {
	state, err := cleanup.requireOpen()
	if err != nil {
		return windowsFailureBeforeVisibility(phaseValidate, path, err)
	}
	return prepareWindowsCommitParent(ctx, path, state)
}

func (cleanup *AncestorCleanup) CommitFile(ctx context.Context, request FileCommit) error {
	state, err := cleanup.requireOpen()
	if err != nil {
		return windowsFailureBeforeVisibility(phaseValidate, request.path, err)
	}
	if err := prepareWindowsCommitParent(ctx, request.path, state); err != nil {
		return err
	}
	return CommitFile(ctx, request)
}

// CreatedDirectoryIdentity returns the exact identity retained for a directory
// created by this cleanup authority. Existing or externally created
// directories are never reported as created by this invocation.
func (cleanup *AncestorCleanup) CreatedDirectoryIdentity(path string) (EntryIdentity, bool, error) {
	state, err := cleanup.requireOpen()
	if err != nil {
		return EntryIdentity{}, false, err
	}
	for index := range state.directories {
		directory := state.directories[index]
		if directory.path == path &&
			directory.cleanupState == windowsCreatedDirectoryCleanupActive &&
			directory.identity.valid() {
			return directory.identity, true, nil
		}
	}
	return EntryIdentity{}, false, nil
}

func (cleanup *AncestorCleanup) RemoveEmpty(ctx context.Context) error {
	state, err := cleanup.requireOpen()
	if err != nil {
		return err
	}
	var failures []error
	for index := len(state.directories) - 1; index >= 0; index-- {
		directory := &state.directories[index]
		if directory.cleanupState == windowsCreatedDirectoryCleanupRetired {
			continue
		}
		if directory.cleanupState == windowsCreatedDirectoryCleanupPendingDurability {
			failures = append(failures, directory.pendingDurabilityFailure)
			continue
		}
		if err := removeWindowsCreatedDirectory(ctx, directory); err != nil {
			failures = append(failures, fmt.Errorf("remove created ancestor %q: %w", directory.path, err))
		}
	}
	return errors.Join(failures...)
}

func (cleanup *AncestorCleanup) Close() {
	if cleanup == nil {
		return
	}
	if cleanup.state == nil {
		cleanup.state = &windowsAncestorCleanupState{closed: true}
		return
	}
	if cleanup.state.closed {
		return
	}
	for index := len(cleanup.state.directories) - 1; index >= 0; index-- {
		_ = cleanup.state.directories[index].handle.Close()
		_ = cleanup.state.directories[index].parent.Close()
	}
	cleanup.state.closed = true
}

func (cleanup *AncestorCleanup) requireOpen() (*windowsAncestorCleanupState, error) {
	if cleanup == nil {
		return nil, fmt.Errorf("ancestor cleanup authority is required")
	}
	if cleanup.state == nil {
		cleanup.state = &windowsAncestorCleanupState{}
	}
	if cleanup.state.closed {
		return nil, fmt.Errorf("ancestor cleanup authority is closed")
	}
	return cleanup.state, nil
}

func PrepareCommitParent(ctx context.Context, path string) error {
	var cleanup AncestorCleanup
	state, _ := cleanup.requireOpen()
	err := prepareWindowsCommitParent(ctx, path, state)
	if err != nil {
		cleanupErr := cleanup.RemoveEmpty(context.Background())
		cleanup.Close()
		return errors.Join(err, cleanupErr)
	}
	cleanup.Close()
	return nil
}

func prepareWindowsCommitParent(
	ctx context.Context,
	path string,
	state *windowsAncestorCleanupState,
) error {
	return prepareWindowsCommitParentWithFaults(ctx, path, state, faultPlan{})
}

func prepareWindowsCommitParentWithFaults(
	ctx context.Context,
	path string,
	state *windowsAncestorCleanupState,
	faults faultPlan,
) error {
	if ctx == nil {
		return windowsFailureBeforeVisibility(phaseValidate, path, fmt.Errorf("commit parent context is required"))
	}
	if err := validateCommitPath(path); err != nil {
		return windowsFailureBeforeVisibility(phaseValidate, path, err)
	}
	if err := ctx.Err(); err != nil {
		return windowsFailureBeforeVisibility(phaseValidate, path, err)
	}
	root, destination, err := rootedpath.CaptureDestinationNoFollow(path)
	if err != nil {
		return windowsFailureBeforeVisibility(phaseCreateAncestors, path, err)
	}
	capability, err := root.Acquire(destination)
	rootCloseErr := root.Close()
	if err != nil || rootCloseErr != nil {
		return windowsFailureBeforeVisibility(phaseCreateAncestors, path, errors.Join(err, rootCloseErr))
	}
	defer capability.Close()
	components := strings.Split(destination.Relative().Path(), "/")
	if len(components) < 1 {
		return windowsFailureBeforeVisibility(phaseCreateAncestors, path, fmt.Errorf("commit destination has no components"))
	}
	rootFile, err := capability.OpenRootDirectoryForMutation()
	if err != nil {
		return windowsFailureBeforeVisibility(phaseCreateAncestors, path, windowsUnsupportedCause(err))
	}
	defer rootFile.Close()
	parentDirectory, err := captureWindowsDirectoryHandle(windows.Handle(rootFile.Fd()))
	if err != nil {
		return windowsFailureBeforeVisibility(phaseCreateAncestors, path, windowsUnsupportedCause(err))
	}
	if err := capability.ValidateDirectoryHandle(rootFile.Fd()); err != nil {
		return windowsFailureBeforeVisibility(phaseCreateAncestors, path, err)
	}
	var traversal []*windowsOwnedHandle
	defer func() { _ = closeWindowsOwnedHandles(traversal...) }()
	privateSecurity, err := buildWindowsCanonicalSecurity(preparedTreePrivateDirectoryMode)
	if err != nil {
		return windowsFailureBeforeVisibility(phaseCreateAncestors, path, windowsUnsupportedCause(err))
	}
	currentPath := path
	for range components {
		currentPath = filepath.Dir(currentPath)
	}
	for _, component := range components[:len(components)-1] {
		if err := ctx.Err(); err != nil {
			return windowsFailureBeforeVisibility(phaseCreateAncestors, path, err)
		}
		currentPath = filepath.Join(currentPath, component)
		opened, openErr := openWindowsRelativeDirectory(
			parentDirectory,
			component,
			windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.FILE_TRAVERSE|windows.FILE_READ_EA|
				windows.READ_CONTROL|windows.WRITE_DAC|windows.DELETE,
			windowsParentShareMode,
			windows.FILE_OPEN,
			false,
		)
		created := false
		attemptedCreate := false
		if openErr != nil && windowsNativeErrorClassOf(openErr) == windowsNativeErrorNotFound {
			attemptedCreate = true
			if err := faults.check(ctx, phaseCreateAncestors); err != nil {
				return windowsFailureBeforeVisibility(phaseCreateAncestors, path, err)
			}
			if err := ctx.Err(); err != nil {
				return windowsFailureBeforeVisibility(phaseCreateAncestors, path, err)
			}
			opened, openErr = createWindowsRelativeDirectory(
				parentDirectory,
				component,
				windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.FILE_TRAVERSE|windows.FILE_READ_EA|
					windows.READ_CONTROL|windows.WRITE_DAC|windows.DELETE,
				windowsParentShareMode,
				true,
				privateSecurity.descriptor,
			)
			created = openErr == nil
		}
		if openErr != nil {
			if attemptedCreate {
				observed, observeErr := observeWindowsEntryAt(parentDirectory, component)
				if observed.exists || observeErr != nil {
					return newFailure(
						failureIndeterminateCommit,
						phaseCreateAncestors,
						path,
						errors.Join(openErr, observeErr),
						currentPath,
					)
				}
			}
			return windowsFailureBeforeVisibility(phaseCreateAncestors, path, openErr)
		}
		facts, factsErr := queryWindowsEntryFacts(opened.handle.Handle())
		if factsErr != nil || !facts.standard.directory || facts.attribute.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			cause := errors.Join(factsErr, fmt.Errorf("commit ancestor is not a non-reparse directory"))
			if created {
				return windowsCleanupFreshCreatedAncestor(parentDirectory, opened, component, cause, path, currentPath)
			}
			_ = opened.handle.Close()
			return windowsFailureBeforeVisibility(phaseCreateAncestors, path, cause)
		}
		if created {
			parentCopy, copyErr := duplicateWindowsOwnedHandle(parentDirectory.Handle())
			handleCopy, handleErr := duplicateWindowsOwnedHandle(opened.handle.Handle())
			if copyErr != nil || handleErr != nil {
				_ = parentCopy.Close()
				_ = handleCopy.Close()
				return windowsCleanupFreshCreatedAncestor(
					parentDirectory,
					opened,
					component,
					errors.Join(copyErr, handleErr),
					path,
					currentPath,
				)
			}
			state.directories = append(state.directories, windowsCreatedDirectory{
				path: currentPath,
				name: component,
				identity: EntryIdentity{
					path:     currentPath,
					kind:     entryKindDirectory,
					platform: platformIdentity{native: facts.identity},
				},
				parent:              parentCopy,
				parentCaseSensitive: parentDirectory.caseSensitive,
				handle:              handleCopy,
			})
			if err := validateWindowsCanonicalEntryAttributes(facts.attribute.attributes, true); err != nil {
				_ = opened.handle.Close()
				return newFailure(failureIndeterminateCommit, phaseApplyMetadata, path, err, currentPath)
			}
			metadata, metadataErr := queryWindowsMetadataFacts(opened.handle.Handle())
			if metadataErr != nil {
				_ = opened.handle.Close()
				return newFailure(failureRetainedResidue, phaseApplyMetadata, path, metadataErr, currentPath)
			}
			if err := ensureWindowsCanonicalMetadataSupported(metadata, privateSecurity.facts); err != nil {
				_ = opened.handle.Close()
				return newFailure(failureRetainedResidue, phaseApplyMetadata, path, err, currentPath)
			}
			if err := flushWindowsHandle(opened.handle.Handle(), windowsFlushPolicy{directory: true}); err != nil {
				_ = opened.handle.Close()
				return newFailure(failureRetainedResidue, phaseSyncAncestors, path, err, currentPath)
			}
			if err := flushWindowsHandle(parentDirectory.Handle(), windowsFlushPolicy{directory: true}); err != nil {
				_ = opened.handle.Close()
				return newFailure(failureIndeterminateCommit, phaseSyncAncestors, path, err, currentPath)
			}
		}
		traversal = append(traversal, opened.handle)
		parentDirectory = opened.directory
	}
	if err := faults.check(ctx, phaseCreateAncestors); err != nil {
		return windowsFailureBeforeVisibility(phaseCreateAncestors, path, err)
	}
	if err := ctx.Err(); err != nil {
		return windowsFailureBeforeVisibility(phaseCreateAncestors, path, err)
	}
	if err := capability.ValidateRetainedDirectoryHandle(rootFile.Fd()); err != nil {
		return windowsFailureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	return nil
}

func removeWindowsCreatedDirectory(ctx context.Context, directory *windowsCreatedDirectory) error {
	return removeWindowsCreatedDirectoryWithFaults(ctx, directory, faultPlan{})
}

// windowsCleanupFreshCreatedAncestor removes a just-created, still-empty
// ancestor through its retained handle when cleanup authority could not be
// registered. Confirmed absence keeps the failure uncommitted; any remaining
// visibility returns indeterminate evidence instead of an untracked residue.
func windowsCleanupFreshCreatedAncestor(
	parentDirectory windowsDirectoryHandle,
	opened *windowsRelativeOpen,
	component string,
	cause error,
	path string,
	currentPath string,
) error {
	_, disposeErr := disposeWindowsByHandle(opened.handle.Handle(), false)
	closeErr := opened.handle.Close()
	if disposeErr != nil || closeErr != nil {
		return newFailure(
			failureIndeterminateCommit,
			phaseCreateAncestors,
			path,
			errors.Join(cause, disposeErr, closeErr),
			currentPath,
		)
	}
	reopen, observeErr := openWindowsRelativeDirectory(
		parentDirectory,
		component,
		windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE,
		windowsParentShareMode,
		windows.FILE_OPEN,
		false,
	)
	if observeErr != nil {
		if windowsNativeErrorClassOf(observeErr) == windowsNativeErrorNotFound {
			return windowsFailureBeforeVisibility(phaseCreateAncestors, path, cause)
		}
		return newFailure(
			failureIndeterminateCommit,
			phaseCreateAncestors,
			path,
			errors.Join(cause, observeErr),
			currentPath,
		)
	}
	_ = reopen.handle.Close()
	return newFailure(failureIndeterminateCommit, phaseCreateAncestors, path, cause, currentPath)
}

func removeWindowsCreatedDirectoryWithFaults(
	ctx context.Context,
	directory *windowsCreatedDirectory,
	faults faultPlan,
) error {
	if directory == nil || directory.cleanupState == windowsCreatedDirectoryCleanupRetired {
		return nil
	}
	if directory.cleanupState == windowsCreatedDirectoryCleanupPendingDurability {
		return directory.pendingDurabilityFailure
	}
	if directory.parent == nil || directory.handle == nil {
		return fmt.Errorf("created ancestor cleanup authority is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	facts, err := queryWindowsEntryFacts(directory.handle.Handle())
	if err != nil {
		return err
	}
	if !directory.identity.platform.native.sameObject(facts.identity) || !facts.standard.directory {
		return fmt.Errorf("created ancestor identity changed")
	}
	entries, err := enumerateWindowsDirectoryOnce(ctx, directory.handle.Handle(), 1)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("created ancestor is not empty")
	}
	parentDirectory := windowsDirectoryHandle{
		handle:        directory.parent.Handle(),
		caseSensitive: directory.parentCaseSensitive,
	}
	if err := requireWindowsNameMatches(
		parentDirectory,
		directory.name,
		directory.identity.platform.native,
		false,
	); err != nil {
		return err
	}
	if err := faults.check(ctx, phaseCleanupEntry); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := disposeWindowsByHandle(directory.handle.Handle(), false); err != nil {
		return err
	}
	directory.cleanupState = windowsCreatedDirectoryCleanupPendingDurability
	if err := directory.handle.Close(); err != nil {
		directory.handle = nil
		return retainWindowsCreatedDirectoryDurabilityFailure(directory, newFailure(
			failureIndeterminateCommit,
			phaseClosePayload,
			directory.path,
			err,
			directory.path,
		))
	}
	directory.handle = nil
	if err := faults.check(ctx, phaseSyncCleanupParent); err != nil {
		return retainWindowsCreatedDirectoryDurabilityFailure(directory, newFailure(
			failureIndeterminateCommit,
			phaseSyncCleanupParent,
			directory.path,
			err,
			directory.path,
		))
	}
	if err := flushWindowsHandle(directory.parent.Handle(), windowsFlushPolicy{directory: true}); err != nil {
		return retainWindowsCreatedDirectoryDurabilityFailure(directory, newFailure(
			failureIndeterminateCommit,
			phaseSyncCleanupParent,
			directory.path,
			err,
			directory.path,
		))
	}
	if err := requireWindowsSiblingAbsent(parentDirectory, directory.name); err != nil {
		return retainWindowsCreatedDirectoryDurabilityFailure(directory, newFailure(
			failureIndeterminateCommit,
			phaseVerifyEntry,
			directory.path,
			err,
			directory.path,
		))
	}
	if err := directory.parent.Close(); err != nil {
		directory.parent = nil
		return retainWindowsCreatedDirectoryDurabilityFailure(directory, newFailure(
			failureIndeterminateCommit,
			phaseClosePayload,
			directory.path,
			err,
			directory.path,
		))
	}
	directory.parent = nil
	directory.cleanupState = windowsCreatedDirectoryCleanupRetired
	directory.pendingDurabilityFailure = nil
	return nil
}

func retainWindowsCreatedDirectoryDurabilityFailure(
	directory *windowsCreatedDirectory,
	err error,
) error {
	directory.cleanupState = windowsCreatedDirectoryCleanupPendingDurability
	directory.pendingDurabilityFailure = err
	return err
}
