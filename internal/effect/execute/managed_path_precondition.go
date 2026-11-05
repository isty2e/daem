package execute

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
)

type managedPathPrecondition struct {
	destination mutationnamespace.Destination
	existed     bool
	hash        artifact.ContentHash
	identity    mutationfs.EntryIdentity
	fileMode    os.FileMode
	capability  rootedpath.CommitCapability
	filesystem  mutationfs.RootedStore
	consumed    bool
}

const (
	maximumManagedTreeEntries = 100_000
	maximumManagedTreeDepth   = 64
	maximumManagedTreeBytes   = 4 << 30
)

func managedTreeTraversalLimits() mutationfs.TreeTraversalLimits {
	limits, err := mutationfs.NewTreeTraversalLimits(
		maximumManagedTreeEntries,
		maximumManagedTreeDepth,
		maximumManagedTreeBytes,
	)
	if err != nil {
		panic(err)
	}
	return limits
}

func captureManagedPathPrecondition(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationnamespace.Destination,
	expectedExists bool,
	expectedHash artifact.ContentHash,
	contentKind realization.PathProjectionContentKind,
	expectedFileMode *os.FileMode,
) (managedPathPrecondition, error) {
	if !destination.IsRooted() {
		return managedPathPrecondition{}, fmt.Errorf("managed path mutation destination is invalid")
	}
	return captureRootedManagedPathPrecondition(
		ctx,
		authority,
		destination,
		expectedExists,
		expectedHash,
		contentKind,
		expectedFileMode,
	)
}

func captureRootedManagedPathPrecondition(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationnamespace.Destination,
	expectedExists bool,
	expectedHash artifact.ContentHash,
	contentKind realization.PathProjectionContentKind,
	expectedFileMode *os.FileMode,
) (managedPathPrecondition, error) {
	if expectedExists {
		if err := expectedHash.Validate(); err != nil {
			return managedPathPrecondition{}, fmt.Errorf(
				"managed destination %q expected hash: %w",
				destination.Logical(),
				err,
			)
		}
	} else if expectedHash != "" {
		return managedPathPrecondition{}, fmt.Errorf(
			"absent managed destination %q must not carry an expected hash",
			destination.Logical(),
		)
	}
	capability, err := authority.destinations.Acquire(destination)
	if err != nil {
		return managedPathPrecondition{}, err
	}
	fail := func(err error) (managedPathPrecondition, error) {
		return managedPathPrecondition{}, errors.Join(err, capability.Close())
	}
	if !expectedExists {
		if _, err := authority.filesystem.CaptureRootedEntryIdentity(ctx, capability); err == nil {
			return fail(fmt.Errorf("managed destination %q appeared after planning", destination.Logical()))
		} else if !errors.Is(err, os.ErrNotExist) {
			return fail(err)
		}
		return managedPathPrecondition{
			destination: destination,
			capability:  capability,
			filesystem:  authority.filesystem,
		}, nil
	}
	if contentKind == realization.PathProjectionFile {
		content, mode, identity, err := authority.filesystem.ReadRootedRegularFile(ctx, capability)
		if err != nil {
			return fail(err)
		}
		hash := artifact.HashFileContentWithExecutable(
			content,
			mode.Perm()&0o111 != 0,
		)

		if hash != expectedHash {
			return fail(fmt.Errorf(
				"managed destination %q hash %q does not match expected %q",
				destination.Logical(),
				hash,
				expectedHash,
			))
		}
		if !managedPathFileModeMatches(mode, expectedFileMode) {
			return fail(fmt.Errorf(
				"managed destination %q mode %04o does not match expected %04o",
				destination.Logical(),
				mode.Perm(),
				expectedFileMode.Perm(),
			))
		}
		return managedPathPrecondition{
			destination: destination, existed: true, hash: expectedHash, identity: identity,
			fileMode: mode.Perm(), capability: capability, filesystem: authority.filesystem,
		}, nil
	}
	if contentKind != realization.PathProjectionDirectory {
		return fail(fmt.Errorf("managed destination %q content kind %q is unsupported", destination.Logical(), contentKind))
	}
	sink := newManagedPathHashSink(ctx)
	identity, err := authority.filesystem.SnapshotRootedDirectory(
		ctx,
		capability,
		managedTreeTraversalLimits(),
		sink,
	)
	if err != nil {
		return fail(err)
	}
	hash, err := sink.sum()
	if err != nil {
		return fail(err)
	}
	if hash != expectedHash {
		return fail(fmt.Errorf(
			"managed destination %q hash %q does not match expected %q",
			destination.Logical(),
			hash,
			expectedHash,
		))
	}
	return managedPathPrecondition{
		destination: destination, existed: true, hash: expectedHash, identity: identity,
		capability: capability, filesystem: authority.filesystem,
	}, nil
}

func managedPathFileModeMatches(actual os.FileMode, expected *os.FileMode) bool {
	return expected == nil || actual.Perm() == expected.Perm()
}

func (precondition *managedPathPrecondition) takeRootedCapability() (rootedpath.CommitCapability, error) {
	if precondition == nil || !precondition.destination.IsRooted() || precondition.capability == nil || precondition.consumed {
		return nil, fmt.Errorf("managed rooted precondition capability is unavailable")
	}
	precondition.consumed = true
	return precondition.capability, nil
}

func (precondition *managedPathPrecondition) close() error {
	if precondition == nil || precondition.capability == nil || precondition.consumed {
		return nil
	}
	precondition.consumed = true
	return precondition.capability.Close()
}

type managedPathHashSink struct {
	ctx     context.Context
	builder *artifact.DirectoryHashBuilder
}

func newManagedPathHashSink(ctx context.Context) *managedPathHashSink {
	return &managedPathHashSink{ctx: ctx, builder: artifact.NewDirectoryHashBuilder()}
}

func (*managedPathHashSink) VisitRoot(fs.FileMode) error { return nil }

func (sink *managedPathHashSink) VisitDirectory(path mutationfs.TreeRelativePath, _ fs.FileMode) error {
	return sink.builder.AddDirectory(path.Path())
}

func (sink *managedPathHashSink) VisitRegularFile(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
	size int64,
	content io.Reader,
) error {
	return sink.builder.AddFile(sink.ctx, path.Path(), mode.Perm()&0o111 != 0, size, content)
}

func (sink *managedPathHashSink) sum() (artifact.ContentHash, error) {
	return sink.builder.Sum()
}
