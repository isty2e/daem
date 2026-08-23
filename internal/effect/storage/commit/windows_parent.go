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
	path     string
	name     string
	identity EntryIdentity
	parent   *windowsOwnedHandle
	handle   *windowsOwnedHandle
	retired  bool
}

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

func (cleanup *AncestorCleanup) RemoveEmpty(ctx context.Context) error {
	state, err := cleanup.requireOpen()
	if err != nil {
		return err
	}
	var failures []error
	for index := len(state.directories) - 1; index >= 0; index-- {
		directory := &state.directories[index]
		if directory.retired {
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
	if ctx == nil {
		return windowsFailureBeforeVisibility(phaseValidate, path, fmt.Errorf("commit parent context is required"))
	}
	if err := validateCommitPath(path); err != nil {
		return windowsFailureBeforeVisibility(phaseValidate, path, err)
	}
	if err := ctx.Err(); err != nil {
		return windowsFailureBeforeVisibility(phaseValidate, path, err)
	}
	root, destination, err := rootedpath.CaptureDestination(path)
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
	rootFile, err := capability.OpenRootDirectory()
	if err != nil {
		return windowsFailureBeforeVisibility(phaseCreateAncestors, path, windowsUnsupportedCause(err))
	}
	defer rootFile.Close()
	parentHandle := windows.Handle(rootFile.Fd())
	var traversal []*windowsOwnedHandle
	defer func() { _ = closeWindowsOwnedHandles(traversal...) }()
	privateSecurity, err := buildWindowsCanonicalSecurity(preparedTreePrivateDirectoryMode)
	if err != nil {
		return windowsFailureBeforeVisibility(phaseCreateAncestors, path, windowsUnsupportedCause(err))
	}
	currentPath := destination.Root().PhysicalRoot()
	for _, component := range components[:len(components)-1] {
		if err := ctx.Err(); err != nil {
			return windowsFailureBeforeVisibility(phaseCreateAncestors, path, err)
		}
		currentPath = filepath.Join(currentPath, component)
		opened, openErr := openWindowsRelativeDirectory(
			parentHandle,
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
			opened, openErr = createWindowsRelativeDirectory(
				parentHandle,
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
				observed, observeErr := observeWindowsEntryAt(parentHandle, component)
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
			_ = opened.handle.Close()
			cause := errors.Join(factsErr, fmt.Errorf("commit ancestor is not a non-reparse directory"))
			if created {
				return newFailure(failureIndeterminateCommit, phaseCreateAncestors, path, cause, currentPath)
			}
			return windowsFailureBeforeVisibility(phaseCreateAncestors, path, cause)
		}
		if created {
			if err := validateWindowsCanonicalEntryAttributes(facts.attribute.attributes, true); err != nil {
				_ = opened.handle.Close()
				return newFailure(failureIndeterminateCommit, phaseApplyMetadata, path, err, currentPath)
			}
			parentCopy, copyErr := duplicateWindowsOwnedHandle(parentHandle)
			handleCopy, handleErr := duplicateWindowsOwnedHandle(opened.handle.Handle())
			if copyErr != nil || handleErr != nil {
				_ = parentCopy.Close()
				_ = handleCopy.Close()
				_ = opened.handle.Close()
				return newFailure(
					failureIndeterminateCommit,
					phaseCreateAncestors,
					path,
					errors.Join(copyErr, handleErr),
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
				parent: parentCopy,
				handle: handleCopy,
			})
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
			if err := flushWindowsHandle(parentHandle, windowsFlushPolicy{directory: true}); err != nil {
				_ = opened.handle.Close()
				return newFailure(failureIndeterminateCommit, phaseSyncAncestors, path, err, currentPath)
			}
		}
		traversal = append(traversal, opened.handle)
		parentHandle = opened.handle.Handle()
	}
	if err := capability.ValidateRetainedDirectoryHandle(rootFile.Fd()); err != nil {
		return windowsFailureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	return nil
}

func removeWindowsCreatedDirectory(ctx context.Context, directory *windowsCreatedDirectory) error {
	if directory == nil || directory.retired || directory.parent == nil || directory.handle == nil {
		return nil
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
	if err := requireWindowsNameMatches(
		directory.parent.Handle(),
		directory.name,
		directory.identity.platform.native,
		false,
	); err != nil {
		return err
	}
	if _, err := disposeWindowsByHandle(directory.handle.Handle(), false); err != nil {
		return err
	}
	if err := directory.handle.Close(); err != nil {
		return err
	}
	if err := flushWindowsHandle(directory.parent.Handle(), windowsFlushPolicy{directory: true}); err != nil {
		return err
	}
	if err := requireWindowsSiblingAbsent(directory.parent.Handle(), directory.name); err != nil {
		return err
	}
	directory.retired = true
	return nil
}
