package filesystem

import (
	"context"
	"io/fs"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

// PathReader performs bounded, no-follow reads of operation-selected paths.
type PathReader interface {
	CaptureEntryIdentity(ctx context.Context, path string) (EntryIdentity, error)
	ReadRegularFileSnapshotUpTo(ctx context.Context, path string, maximumBytes int64) (RegularFileSnapshot, error)
}

// DirectoryReader captures one stable immediate-child inventory without
// interpreting entry names or traversing children.
type DirectoryReader interface {
	SnapshotDirectory(
		ctx context.Context,
		path string,
		maximumEntries int,
	) (DirectorySnapshot, error)
}

// PathCommitter performs guarded stable publication on operation-selected
// paths. Paths are supplied by an outer boundary; this port never selects one.
type PathCommitter interface {
	PrepareCommitParent(ctx context.Context, path string) error
	CreateFile(ctx context.Context, path string, content []byte, mode fs.FileMode) error
	ReplaceFile(
		ctx context.Context,
		path string,
		content []byte,
		mode fs.FileMode,
		expected EntryIdentity,
	) error
	RemoveEntry(ctx context.Context, path string, expected EntryIdentity) error
	PublishPreparedTree(
		ctx context.Context,
		stagedRoot string,
		destination string,
		expected EntryIdentity,
	) error
}

// RootedReader observes one destination through retained-root authority without
// consuming the capability.
type RootedReader interface {
	CaptureRootedEntryIdentity(
		ctx context.Context,
		capability rootedpath.CommitCapability,
	) (EntryIdentity, error)
	ReadRootedRegularFile(
		ctx context.Context,
		capability rootedpath.CommitCapability,
	) ([]byte, fs.FileMode, EntryIdentity, error)
	ReadRootedRegularFileUpTo(
		ctx context.Context,
		capability rootedpath.CommitCapability,
		maximumBytes int64,
	) ([]byte, fs.FileMode, EntryIdentity, error)
	SnapshotRootedDirectoryEntries(
		ctx context.Context,
		capability rootedpath.CommitCapability,
		maximumEntries int,
	) (DirectorySnapshot, error)
	SnapshotRootedDirectory(
		ctx context.Context,
		capability rootedpath.CommitCapability,
		limits TreeTraversalLimits,
		sink RootedTreeSnapshotSink,
	) (EntryIdentity, error)
	ValidateRootedDirectoryTree(
		ctx context.Context,
		capability rootedpath.CommitCapability,
		limits TreeTraversalLimits,
	) (EntryIdentity, error)
}

// RootedCommitter performs guarded publication through retained-root
// authority. Every mutating method consumes the supplied capability.
type RootedCommitter interface {
	CreateRootedFile(
		ctx context.Context,
		capability rootedpath.CommitCapability,
		content []byte,
		mode fs.FileMode,
	) error
	ReplaceRootedFile(
		ctx context.Context,
		capability rootedpath.CommitCapability,
		content []byte,
		mode fs.FileMode,
		expected EntryIdentity,
	) error
	RemoveRootedEntry(
		ctx context.Context,
		capability rootedpath.CommitCapability,
		expected EntryIdentity,
	) error
	RemoveRootedEntryWithResidue(
		ctx context.Context,
		capability rootedpath.CommitCapability,
		expected EntryIdentity,
		names LogicalRemovalNames,
	) (CommitOutcome, error)
	PrepareRootedTree(
		ctx context.Context,
		capability rootedpath.CommitCapability,
		populate func(RootedTreeWriter) error,
	) (PreparedRootedTree, error)
}

// RootedEntryCommitter performs exact same-parent entry operations through
// retained-root authority. It does not select names or interpret their
// semantics, and every method consumes the supplied capability.
type RootedEntryCommitter interface {
	RenameRootedEntry(
		ctx context.Context,
		capability rootedpath.CommitCapability,
		destinationName string,
		expected EntryIdentity,
	) (CommitOutcome, error)
	PromoteRootedRemovalResidue(
		ctx context.Context,
		capability rootedpath.CommitCapability,
		expected EntryIdentity,
		names LogicalRemovalNames,
	) (CommitOutcome, EntryIdentity, error)
	ReplaceRootedFileWithOutcome(
		ctx context.Context,
		capability rootedpath.CommitCapability,
		content []byte,
		mode fs.FileMode,
		expected EntryIdentity,
	) (CommitOutcome, error)
	ReplaceRootedFileAndRefreshParent(
		ctx context.Context,
		capability rootedpath.CommitCapability,
		content []byte,
		mode fs.FileMode,
		expected EntryIdentity,
		expectedParent EntryIdentity,
	) (CommitOutcome, EntryIdentity, error)
	CleanupRootedEntry(
		ctx context.Context,
		capability rootedpath.CommitCapability,
		expected EntryIdentity,
	) (CommitOutcome, error)
	CleanupRootedRemovalStage(
		ctx context.Context,
		capability rootedpath.CommitCapability,
		expected EntryIdentity,
		names LogicalRemovalNames,
	) (CommitOutcome, error)
	ConfirmRootedEntryAbsent(
		ctx context.Context,
		capability rootedpath.CommitCapability,
	) (CommitOutcome, error)
}

// PathStore is the direct-path subset needed by outer-boundary persistence.
type PathStore interface {
	PathReader
	PathCommitter
}

// RootedStore is the retained-root subset needed by effect execution.
type RootedStore interface {
	RootedReader
	RootedCommitter
	RootedEntryCommitter
}

// Reader is the complete bounded observation subset used by Effect packages.
type Reader interface {
	PathReader
	DirectoryReader
	RootedReader
}

// Store is the finite guarded-filesystem protocol used by current Effect
// operations. It has no path selection, arbitrary traversal, or policy API.
type Store interface {
	PathStore
	DirectoryReader
	RootedStore
}
