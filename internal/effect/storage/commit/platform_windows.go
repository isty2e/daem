//go:build windows

package commit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/windows"
)

type platformIdentity struct {
	native windowsEntryIdentityNative
}

func (identity platformIdentity) valid() bool { return identity.native.valid() }

func (identity platformIdentity) matches(other platformIdentity) bool {
	return identity.native.equal(other.native)
}

func (identity platformIdentity) sameObject(other platformIdentity) bool {
	return identity.native.sameObject(other.native)
}

// CaptureEntryIdentity captures ephemeral no-follow identity evidence through
// a retained Windows root and parent handle.
func CaptureEntryIdentity(ctx context.Context, path string) (EntryIdentity, error) {
	if err := validateCommitPath(path); err != nil {
		return EntryIdentity{}, err
	}
	if ctx == nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, fmt.Errorf("identity capture context is required"))
	}
	if err := ctx.Err(); err != nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	capability, err := acquireWindowsPathCapability(path)
	if err != nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	defer capability.Close()
	identity, err := captureWindowsEntryIdentity(ctx, capability, false)
	if err == nil {
		identity.path = path
	}
	return identity, err
}

// CaptureRootedEntryIdentity captures a no-follow final entry without consuming
// the supplied capability.
func CaptureRootedEntryIdentity(ctx context.Context, capability rootedpath.CommitCapability) (EntryIdentity, error) {
	return captureWindowsEntryIdentity(ctx, capability, true)
}

func captureWindowsEntryIdentity(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	rejectFinalLink bool,
) (EntryIdentity, error) {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return EntryIdentity{}, err
	}
	anchor, err := openWindowsDestinationAnchor(ctx, capability, false)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	observed, err := openWindowsObservedEntry(ctx, anchor, false, false, false)
	if observed != nil {
		defer observed.close()
	}
	if err != nil {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	if rejectFinalLink && observed.identity.kind == entryKindSymlink {
		return EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, rootedFinalSymlinkFailure(path))
	}
	return observed.identity, nil
}

// CaptureWorkingDirectoryIdentity observes the retained directory object
// without reopening its diagnostic path.
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
	if budget == nil {
		return EntryIdentity{}, fmt.Errorf("working-directory identity budget is required")
	}
	file, err := capability.OpenDirectoryBounded(budget)
	if err != nil {
		return EntryIdentity{}, err
	}
	if err := ctx.Err(); err != nil {
		_ = file.Close()
		return EntryIdentity{}, err
	}
	facts, factsErr := queryWindowsEntryFacts(windows.Handle(file.Fd()))
	closeErr := file.Close()
	if factsErr != nil || closeErr != nil {
		return EntryIdentity{}, errors.Join(factsErr, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return EntryIdentity{}, err
	}
	if !facts.standard.directory || facts.attribute.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return EntryIdentity{}, fmt.Errorf("working-directory capability does not identify a non-reparse directory")
	}
	return EntryIdentity{
		path:     file.Name(),
		kind:     entryKindDirectory,
		platform: platformIdentity{native: facts.identity},
	}, nil
}

// ReadRegularFileSnapshotUpTo returns one bounded, identity-stable Windows
// regular-file snapshot.
func ReadRegularFileSnapshotUpTo(
	ctx context.Context,
	path string,
	maximumBytes int64,
) (mutationfs.RegularFileSnapshot, error) {
	if maximumBytes <= 0 {
		return mutationfs.RegularFileSnapshot{}, fmt.Errorf("regular file snapshot maximum bytes must be positive")
	}
	if err := validateCommitPath(path); err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	if ctx == nil {
		return mutationfs.RegularFileSnapshot{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, fmt.Errorf("file snapshot context is required"))
	}
	if err := ctx.Err(); err != nil {
		return mutationfs.RegularFileSnapshot{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	capability, err := acquireWindowsPathCapability(path)
	if err != nil {
		return mutationfs.RegularFileSnapshot{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	defer capability.Close()
	content, mode, identity, err := readWindowsRegularFile(ctx, capability, maximumBytes)
	if err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	identity.path = path
	return mutationfs.NewRegularFileSnapshot(content, mode, identity)
}

// SnapshotDirectory returns one stable immediate-child inventory through a
// retained Windows path authority.
func SnapshotDirectory(
	ctx context.Context,
	path string,
	maximumEntries int,
) (mutationfs.DirectorySnapshot, error) {
	if maximumEntries <= 0 {
		return mutationfs.DirectorySnapshot{}, fmt.Errorf("directory snapshot maximum entries must be positive")
	}
	if err := validateCommitPath(path); err != nil {
		return mutationfs.DirectorySnapshot{}, err
	}
	if ctx == nil {
		return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, fmt.Errorf("directory snapshot context is required"))
	}
	if err := ctx.Err(); err != nil {
		return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	capability, err := acquireWindowsPathCapability(path)
	if err != nil {
		return mutationfs.DirectorySnapshot{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	defer capability.Close()
	return snapshotWindowsDirectoryEntries(ctx, capability, path, maximumEntries)
}

// ReadRootedRegularFile returns content, canonical mode, and identity without
// consuming the supplied capability.
func ReadRootedRegularFile(
	ctx context.Context,
	capability rootedpath.CommitCapability,
) ([]byte, fs.FileMode, EntryIdentity, error) {
	return readWindowsRegularFile(ctx, capability, int64(^uint64(0)>>1))
}

// ReadRootedRegularFileUpTo returns one bounded rooted regular-file snapshot
// without consuming the supplied capability.
func ReadRootedRegularFileUpTo(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	maximumBytes int64,
) ([]byte, fs.FileMode, EntryIdentity, error) {
	if maximumBytes <= 0 {
		return nil, 0, EntryIdentity{}, fmt.Errorf("rooted regular file maximum bytes must be positive")
	}
	return readWindowsRegularFile(ctx, capability, maximumBytes)
}

func readWindowsRegularFile(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	maximumBytes int64,
) ([]byte, fs.FileMode, EntryIdentity, error) {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return nil, 0, EntryIdentity{}, err
	}
	anchor, err := openWindowsDestinationAnchor(ctx, capability, false)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return nil, 0, EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	observed, err := openWindowsObservedEntry(ctx, anchor, true, true, false)
	if observed != nil {
		defer observed.close()
	}
	if err != nil {
		return nil, 0, EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	if observed.identity.kind == entryKindSymlink {
		return nil, 0, EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, rootedFinalSymlinkFailure(path))
	}
	if observed.identity.kind != entryKindRegular {
		return nil, 0, EntryIdentity{}, windowsFailureBeforeVisibility(phaseValidate, path, fmt.Errorf("entry is not a regular file"))
	}
	if err := validateWindowsCanonicalEntryAttributes(observed.facts.attribute.attributes, false); err != nil {
		return nil, 0, EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureMetadata, path, err)
	}
	content, err := readWindowsPayloadUpTo(ctx, observed.handle.Handle(), maximumBytes)
	if err != nil {
		return nil, 0, EntryIdentity{}, windowsFailureBeforeVisibility(phaseReadPayload, path, err)
	}
	if err := revalidateWindowsObservedEntry(ctx, anchor, observed); err != nil {
		return nil, 0, EntryIdentity{}, windowsFailureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	return content, observed.mode, observed.identity, nil
}

// SnapshotRootedDirectoryEntries returns one stable immediate-child inventory
// without consuming the supplied capability.
func SnapshotRootedDirectoryEntries(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	maximumEntries int,
) (mutationfs.DirectorySnapshot, error) {
	return snapshotWindowsDirectoryEntries(ctx, capability, "", maximumEntries)
}

// CommitFile publishes one private, flushed Windows file stage through an
// identity-guarded same-parent rename.
func CommitFile(ctx context.Context, request FileCommit) error {
	_, err := commitWindowsFile(ctx, request)
	return err
}

func commitFileAndRefreshParent(
	ctx context.Context,
	request FileCommit,
) (EntryIdentity, error) {
	var refreshedParent EntryIdentity
	_, err := commitWindowsFileWithFaultsAndParent(ctx, request, faultPlan{}, &refreshedParent)
	return refreshedParent, err
}
