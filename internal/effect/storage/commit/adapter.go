package commit

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

// Adapter implements the Effect-owned guarded-filesystem ports with this
// package's platform-specific stable commit machinery.
type Adapter struct{}

func (Adapter) CaptureEntryIdentity(
	ctx context.Context,
	path string,
) (mutationfs.EntryIdentity, error) {
	return CaptureEntryIdentity(ctx, path)
}

func (Adapter) ReadRegularFileSnapshotUpTo(
	ctx context.Context,
	path string,
	maximumBytes int64,
) (mutationfs.RegularFileSnapshot, error) {
	snapshot, err := ReadRegularFileSnapshotUpTo(ctx, path, maximumBytes)
	if err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	return snapshot, nil
}

func (Adapter) SnapshotDirectory(
	ctx context.Context,
	path string,
	maximumEntries int,
) (mutationfs.DirectorySnapshot, error) {
	return SnapshotDirectory(ctx, path, maximumEntries)
}

func (Adapter) PrepareCommitParent(ctx context.Context, path string) error {
	return PrepareCommitParent(ctx, path)
}

func (Adapter) CreateFile(
	ctx context.Context,
	path string,
	content []byte,
	mode fs.FileMode,
) error {
	request, err := NewFileCreate(path, content, mode)
	if err != nil {
		return failureBeforeVisibility(phaseValidate, path, err)
	}
	return CommitFile(ctx, request)
}

func (Adapter) ReplaceFile(
	ctx context.Context,
	path string,
	content []byte,
	mode fs.FileMode,
	expected mutationfs.EntryIdentity,
) error {
	identity, err := concreteEntryIdentity(expected)
	if err != nil {
		return failureBeforeVisibility(phaseValidate, path, err)
	}
	request, err := NewFileReplacement(path, content, mode, identity)
	if err != nil {
		return failureBeforeVisibility(phaseValidate, path, err)
	}
	return CommitFile(ctx, request)
}

func (Adapter) RemoveEntry(
	ctx context.Context,
	path string,
	expected mutationfs.EntryIdentity,
) error {
	identity, err := concreteEntryIdentity(expected)
	if err != nil {
		return failureBeforeVisibility(phaseValidate, path, err)
	}
	request, err := NewLogicalRemoval(path, identity)
	if err != nil {
		return failureBeforeVisibility(phaseValidate, path, err)
	}
	return CommitLogicalRemoval(ctx, request)
}

func (Adapter) PublishPreparedTree(
	ctx context.Context,
	stagedRoot string,
	destination string,
	expected mutationfs.EntryIdentity,
) error {
	identity, err := concreteEntryIdentity(expected)
	if err != nil {
		return failureBeforeVisibility(phaseValidate, destination, err)
	}
	request, err := NewPreparedTreeCommit(stagedRoot, destination, identity)
	if err != nil {
		return failureBeforeVisibility(phaseValidate, destination, err)
	}
	return CommitPreparedTree(ctx, request)
}

func (Adapter) CaptureRootedEntryIdentity(
	ctx context.Context,
	capability rootedpath.CommitCapability,
) (mutationfs.EntryIdentity, error) {
	return CaptureRootedEntryIdentity(ctx, capability)
}

func (Adapter) ReadRootedRegularFile(
	ctx context.Context,
	capability rootedpath.CommitCapability,
) ([]byte, fs.FileMode, mutationfs.EntryIdentity, error) {
	content, mode, identity, err := ReadRootedRegularFile(ctx, capability)
	return content, mode, identity, err
}

func (Adapter) ReadRootedRegularFileUpTo(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	maximumBytes int64,
) ([]byte, fs.FileMode, mutationfs.EntryIdentity, error) {
	content, mode, identity, err := ReadRootedRegularFileUpTo(ctx, capability, maximumBytes)
	return content, mode, identity, err
}

func (Adapter) SnapshotRootedDirectoryEntries(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	maximumEntries int,
) (mutationfs.DirectorySnapshot, error) {
	return SnapshotRootedDirectoryEntries(ctx, capability, maximumEntries)
}

func (Adapter) SnapshotRootedDirectory(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	limits mutationfs.TreeTraversalLimits,
	sink mutationfs.RootedTreeSnapshotSink,
) (mutationfs.EntryIdentity, error) {
	return SnapshotRootedDirectory(ctx, capability, limits, sink)
}

func (Adapter) ValidateRootedDirectoryTree(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	limits mutationfs.TreeTraversalLimits,
) (mutationfs.EntryIdentity, error) {
	return ValidateRootedDirectoryTree(ctx, capability, limits)
}

func (Adapter) CreateRootedFile(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	content []byte,
	mode fs.FileMode,
) error {
	request, err := NewRootedFileCreate(capability, content, mode)
	if err != nil {
		closeErr := closeRootedCapability(capability)
		return errors.Join(rootedAdapterValidationFailure(capability, err), closeErr)
	}
	return CommitFile(ctx, request)
}

func (Adapter) ReplaceRootedFile(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	content []byte,
	mode fs.FileMode,
	expected mutationfs.EntryIdentity,
) error {
	identity, err := concreteEntryIdentity(expected)
	if err != nil {
		return errors.Join(rootedAdapterValidationFailure(capability, err), closeRootedCapability(capability))
	}
	request, err := NewRootedFileReplacement(capability, content, mode, identity)
	if err != nil {
		return errors.Join(rootedAdapterValidationFailure(capability, err), closeRootedCapability(capability))
	}
	return CommitFile(ctx, request)
}

func (adapter Adapter) ReplaceRootedFileWithOutcome(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	content []byte,
	mode fs.FileMode,
	expected mutationfs.EntryIdentity,
) (mutationfs.CommitOutcome, error) {
	err := adapter.ReplaceRootedFile(ctx, capability, content, mode, expected)
	return outcomeFromError(err), err
}

func (Adapter) RemoveRootedEntry(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	expected mutationfs.EntryIdentity,
) error {
	identity, err := concreteEntryIdentity(expected)
	if err != nil {
		return errors.Join(rootedAdapterValidationFailure(capability, err), closeRootedCapability(capability))
	}
	request, err := NewRootedLogicalRemoval(capability, identity)
	if err != nil {
		return errors.Join(rootedAdapterValidationFailure(capability, err), closeRootedCapability(capability))
	}
	return CommitLogicalRemoval(ctx, request)
}

func (Adapter) RenameRootedEntry(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	destinationName string,
	expected mutationfs.EntryIdentity,
) (mutationfs.CommitOutcome, error) {
	identity, err := concreteEntryIdentity(expected)
	if err != nil {
		failure := errors.Join(
			rootedAdapterValidationFailure(capability, err),
			closeRootedCapability(capability),
		)
		return outcomeFromError(failure), failure
	}
	request, err := NewRootedEntryRename(capability, destinationName, identity)
	if err != nil {
		failure := errors.Join(
			rootedAdapterValidationFailure(capability, err),
			closeRootedCapability(capability),
		)
		return outcomeFromError(failure), failure
	}
	return CommitRootedEntryRename(ctx, request)
}

func (Adapter) CleanupRootedEntry(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	expected mutationfs.EntryIdentity,
) (mutationfs.CommitOutcome, error) {
	identity, err := concreteEntryIdentity(expected)
	if err != nil {
		failure := errors.Join(
			rootedAdapterValidationFailure(capability, err),
			closeRootedCapability(capability),
		)
		return outcomeFromError(failure), failure
	}
	request, err := NewRootedEntryCleanup(capability, identity)
	if err != nil {
		failure := errors.Join(
			rootedAdapterValidationFailure(capability, err),
			closeRootedCapability(capability),
		)
		return outcomeFromError(failure), failure
	}
	return CommitRootedEntryCleanup(ctx, request)
}

func (Adapter) PrepareRootedTree(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	populate func(mutationfs.RootedTreeWriter) error,
) (mutationfs.PreparedRootedTree, error) {
	return PrepareRootedTree(ctx, capability, populate)
}

func concreteEntryIdentity(identity mutationfs.EntryIdentity) (EntryIdentity, error) {
	concrete, ok := identity.(EntryIdentity)
	if !ok || !concrete.valid() {
		return EntryIdentity{}, fmt.Errorf("entry identity was not issued by storage commit adapter")
	}
	return concrete, nil
}

func rootedAdapterValidationFailure(capability rootedpath.CommitCapability, cause error) error {
	path, pathErr := rootedCapabilityPath(capability)
	if pathErr != nil {
		return failureBeforeVisibility(phaseValidate, "", errors.Join(cause, pathErr))
	}
	return failureBeforeVisibility(phaseValidate, path, cause)
}

func closeRootedCapability(capability rootedpath.CommitCapability) error {
	if capability == nil {
		return nil
	}
	return capability.Close()
}

var _ mutationfs.Store = Adapter{}
