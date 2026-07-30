//go:build !darwin && !linux

package commit

import (
	"context"
	"fmt"
	"io/fs"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type platformIdentity struct{}

// PreparedRootedTree has no valid value on an unsupported platform.
type PreparedRootedTree struct {
	destination string
}

func (platformIdentity) valid() bool { return false }

func (platformIdentity) matches(platformIdentity) bool { return false }

func (platformIdentity) sameObject(platformIdentity) bool { return false }

// CaptureEntryIdentity returns unsupported_guarantee on platforms without a
// proven handle-relative adapter.
func CaptureEntryIdentity(_ context.Context, path string) (EntryIdentity, error) {
	if err := validateCommitPath(path); err != nil {
		return EntryIdentity{}, err
	}
	return EntryIdentity{}, newFailure(
		failureUnsupportedGuarantee,
		phaseUnsupported,
		path,
		unsupported("no proven storage commit adapter for this platform", nil),
	)
}

// CaptureRootedEntryIdentity returns unsupported_guarantee without effects.
func CaptureRootedEntryIdentity(_ context.Context, capability rootedpath.CommitCapability) (EntryIdentity, error) {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return EntryIdentity{}, err
	}
	return EntryIdentity{}, newUnsupportedPlatformFailure(path)
}

// PrepareCommitParent returns unsupported_guarantee without performing effects.
func PrepareCommitParent(_ context.Context, path string) error {
	if err := validateCommitPath(path); err != nil {
		return err
	}
	return newUnsupportedPlatformFailure(path)
}

// PrepareRootedTree fails closed before invoking populate.
func PrepareRootedTree(
	_ context.Context,
	capability rootedpath.CommitCapability,
	_ func(mutationfs.RootedTreeWriter) error,
) (*PreparedRootedTree, error) {
	if capability != nil {
		defer capability.Close()
	}
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return nil, err
	}
	return nil, newUnsupportedPlatformFailure(path)
}

// Commit fails closed for the invalid unsupported-platform value.
func (prepared *PreparedRootedTree) Commit(_ context.Context) error {
	path := ""
	if prepared != nil {
		path = prepared.destination
	}
	return newUnsupportedPlatformFailure(path)
}

// CommitWithOutcome returns unsupported_guarantee without effects.
func (prepared *PreparedRootedTree) CommitWithOutcome(
	_ context.Context,
) (mutationfs.CommitOutcome, error) {
	path := ""
	if prepared != nil {
		path = prepared.destination
	}
	err := newUnsupportedPlatformFailure(path)
	return outcomeFromError(err), err
}

// Abort has no resources to release on an unsupported platform.
func (*PreparedRootedTree) Abort(_ context.Context) error {
	return nil
}

// ReadRegularFile returns unsupported_guarantee without reading content.
func ReadRegularFile(_ context.Context, path string) ([]byte, fs.FileMode, error) {
	if err := validateCommitPath(path); err != nil {
		return nil, 0, err
	}
	return nil, 0, newUnsupportedPlatformFailure(path)
}

// ReadRegularFileSnapshot returns unsupported_guarantee without reading.
func ReadRegularFileSnapshot(_ context.Context, path string) (mutationfs.RegularFileSnapshot, error) {
	if err := validateCommitPath(path); err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	return mutationfs.RegularFileSnapshot{}, newUnsupportedPlatformFailure(path)
}

// ReadRegularFileSnapshotUpTo returns unsupported_guarantee without reading.
func ReadRegularFileSnapshotUpTo(
	_ context.Context,
	path string,
	maximumBytes int64,
) (mutationfs.RegularFileSnapshot, error) {
	if maximumBytes <= 0 {
		return mutationfs.RegularFileSnapshot{}, fmt.Errorf("regular file snapshot maximum bytes must be positive")
	}
	if err := validateCommitPath(path); err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	return mutationfs.RegularFileSnapshot{}, newUnsupportedPlatformFailure(path)
}

// SnapshotDirectory returns unsupported_guarantee without reading.
func SnapshotDirectory(
	_ context.Context,
	path string,
) (mutationfs.DirectorySnapshot, error) {
	if err := validateCommitPath(path); err != nil {
		return mutationfs.DirectorySnapshot{}, err
	}
	return mutationfs.DirectorySnapshot{}, newUnsupportedPlatformFailure(path)
}

// ReadRootedRegularFile returns unsupported_guarantee without reading.
func ReadRootedRegularFile(
	_ context.Context,
	capability rootedpath.CommitCapability,
) ([]byte, fs.FileMode, EntryIdentity, error) {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return nil, 0, EntryIdentity{}, err
	}
	return nil, 0, EntryIdentity{}, newUnsupportedPlatformFailure(path)
}

// ReadRootedRegularFileUpTo returns unsupported_guarantee without reading.
func ReadRootedRegularFileUpTo(
	_ context.Context,
	capability rootedpath.CommitCapability,
	maximumBytes int64,
) ([]byte, fs.FileMode, EntryIdentity, error) {
	if maximumBytes <= 0 {
		return nil, 0, EntryIdentity{}, fmt.Errorf("rooted regular file maximum bytes must be positive")
	}
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return nil, 0, EntryIdentity{}, err
	}
	return nil, 0, EntryIdentity{}, newUnsupportedPlatformFailure(path)
}

// SnapshotRootedDirectory returns unsupported_guarantee without reading.
func SnapshotRootedDirectory(
	_ context.Context,
	capability rootedpath.CommitCapability,
	_ mutationfs.RootedTreeSnapshotSink,
) (EntryIdentity, error) {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return EntryIdentity{}, err
	}
	return EntryIdentity{}, newUnsupportedPlatformFailure(path)
}

// CommitFile returns unsupported_guarantee without performing effects.
func CommitFile(_ context.Context, request FileCommit) error {
	if request.capability != nil {
		defer request.capability.Close()
	}
	return newUnsupportedPlatformFailure(request.path)
}

// CommitPreparedTree returns unsupported_guarantee without performing effects.
func CommitPreparedTree(_ context.Context, request PreparedTreeCommit) error {
	return newUnsupportedPlatformFailure(request.destination)
}

// CommitLogicalRemoval returns unsupported_guarantee without performing effects.
func CommitLogicalRemoval(_ context.Context, request LogicalRemoval) error {
	if request.capability != nil {
		defer request.capability.Close()
	}
	return newUnsupportedPlatformFailure(request.path)
}

// CommitRootedEntryRename returns unsupported_guarantee without effects.
func CommitRootedEntryRename(
	_ context.Context,
	request RootedEntryRename,
) (mutationfs.CommitOutcome, error) {
	if request.capability != nil {
		defer request.capability.Close()
	}
	err := newUnsupportedPlatformFailure(request.sourcePath)
	return outcomeFromError(err), err
}

// CommitRootedEntryCleanup returns unsupported_guarantee without effects.
func CommitRootedEntryCleanup(
	_ context.Context,
	request RootedEntryCleanup,
) (mutationfs.CommitOutcome, error) {
	if request.capability != nil {
		defer request.capability.Close()
	}
	err := newUnsupportedPlatformFailure(request.path)
	return outcomeFromError(err), err
}

func newUnsupportedPlatformFailure(path string) error {
	return newFailure(
		failureUnsupportedGuarantee,
		phaseUnsupported,
		path,
		unsupported("no proven storage commit adapter for this platform", nil),
	)
}
