package hostmutation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"

	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
)

type managedPathPrecondition struct {
	operation   *Operation
	destination mutationnamespace.Destination
	existed     bool
	hash        artifact.ContentHash
	identity    mutationfs.EntryIdentity
	fileMode    os.FileMode
	treeModes   map[string]fs.FileMode
	treeLimits  mutationfs.TreeTraversalLimits
	capability  rootedpath.CommitCapability
	filesystem  mutationfs.RootedStore
	consumed    bool
}

// PreparedPath copies share one retained capability's consumption and Close.
type PreparedPath struct{ state *managedPathPrecondition }

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

func (operation *Operation) CapturePath(
	ctx context.Context,
	destination mutationnamespace.Destination,
	expectedExists bool,
	expectedHash artifact.ContentHash,
	contentKind realization.PathProjectionContentKind,
	expectedFileMode *os.FileMode,
) (PreparedPath, error) {
	if !destination.IsRooted() {
		return PreparedPath{}, fmt.Errorf("managed path mutation destination is invalid")
	}
	precondition, err := operation.captureRootedManagedPathPrecondition(
		ctx,
		destination,
		expectedExists,
		expectedHash,
		contentKind,
		expectedFileMode,
	)
	if err != nil {
		return PreparedPath{}, err
	}
	precondition.operation = operation
	return PreparedPath{state: &precondition}, nil
}

func (operation *Operation) captureRootedManagedPathPrecondition(
	ctx context.Context,
	destination mutationnamespace.Destination,
	expectedExists bool,
	expectedHash artifact.ContentHash,
	contentKind realization.PathProjectionContentKind,
	expectedFileMode *os.FileMode,
) (managedPathPrecondition, error) {
	if expectedExists {
		if err := expectedHash.ValidateManaged(); err != nil {
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
	capability, err := operation.destinations.Acquire(destination)
	if err != nil {
		return managedPathPrecondition{}, err
	}
	fail := func(err error) (managedPathPrecondition, error) {
		return managedPathPrecondition{}, errors.Join(err, capability.Close())
	}
	if !expectedExists {
		if _, err := operation.filesystem.CaptureRootedEntryIdentity(ctx, capability); err == nil {
			return fail(fmt.Errorf("managed destination %q appeared after planning", destination.Logical()))
		} else if !errors.Is(err, os.ErrNotExist) {
			return fail(err)
		}
		return managedPathPrecondition{
			destination: destination,
			capability:  capability,
			filesystem:  operation.filesystem,
		}, nil
	}
	if contentKind == realization.PathProjectionFile {
		content, mode, identity, err := operation.filesystem.ReadRootedRegularFile(ctx, capability)
		if err != nil {
			return fail(err)
		}
		if !expectedHash.MatchesFile(content) {
			return fail(fmt.Errorf(
				"managed destination %q does not match expected hash %q",
				destination.Logical(),
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
			fileMode: mode.Perm(), capability: capability, filesystem: operation.filesystem,
		}, nil
	}
	if contentKind != realization.PathProjectionDirectory {
		return fail(fmt.Errorf("managed destination %q content kind %q is unsupported", destination.Logical(), contentKind))
	}
	sink := NewDirectoryHashSink(ctx)
	sink.modes = make(map[string]fs.FileMode)
	identity, err := operation.filesystem.SnapshotRootedDirectory(
		ctx,
		capability,
		managedTreeTraversalLimits(),
		sink,
	)
	if err != nil {
		return fail(err)
	}
	hashes, err := sink.builder.Hashes()
	if err != nil {
		return fail(err)
	}
	if !hashes.Matches(expectedHash) {
		return fail(fmt.Errorf(
			"managed destination %q hash %q does not match expected %q",
			destination.Logical(),
			hashes.Installed,
			expectedHash,
		))
	}
	return managedPathPrecondition{
		destination: destination, existed: true, hash: hashes.Installed, identity: identity,
		treeModes: sink.modes, treeLimits: managedTreeTraversalLimits(),
		capability: capability, filesystem: operation.filesystem,
	}, nil
}

// Reopen through the same retained root and prove the whole tree again before
// retiring it. A child can change without changing its parent's identity.
func (p *managedPathPrecondition) refreshTree(ctx context.Context) (bool, error) {
	if p.treeModes == nil {
		return false, nil
	}
	sink := NewDirectoryHashSink(ctx)
	sink.modes = make(map[string]fs.FileMode)
	identity, err := p.filesystem.SnapshotRootedDirectory(ctx, p.capability, p.treeLimits, sink)
	if err != nil {
		return false, err
	}
	hashes, err := sink.builder.Hashes()
	if err != nil {
		return false, err
	}
	if !mutationfs.SameEntryObject(p.identity, identity) || hashes.Installed != p.hash {
		return false, fmt.Errorf("managed tree %q changed while preparing publication", p.destination.Logical())
	}
	changed := !maps.Equal(p.treeModes, sink.modes)
	p.treeModes, p.identity = sink.modes, identity
	return changed, nil
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

func (path PreparedPath) Close() error {
	return path.state.close()
}

func (operation *Operation) verifyInstalledPath(ctx context.Context, destination mutationnamespace.Destination, expectedExists bool, expectedHash artifact.ContentHash, contentKind realization.PathProjectionContentKind) error {
	observed, err := operation.CapturePath(ctx, destination, expectedExists, expectedHash, contentKind, nil)
	if err != nil {
		return err
	}
	return observed.Close()
}

type DirectoryHashSink struct {
	ctx     context.Context
	builder *artifact.DirectoryHashBuilder
	modes   map[string]fs.FileMode
}

func NewDirectoryHashSink(ctx context.Context) *DirectoryHashSink {
	return &DirectoryHashSink{ctx: ctx, builder: artifact.NewDirectoryEvidenceBuilder()}
}

func (sink *DirectoryHashSink) VisitRoot(mode fs.FileMode) error {
	if sink.modes != nil {
		sink.modes["."] = mode.Perm() | fs.ModeDir
	}
	return nil
}

func (sink *DirectoryHashSink) VisitDirectory(path mutationfs.TreeRelativePath, mode fs.FileMode) error {
	if sink.modes != nil {
		sink.modes[path.Path()] = mode.Perm() | fs.ModeDir
	}
	return sink.builder.AddDirectory(path.Path())
}

func (sink *DirectoryHashSink) VisitRegularFile(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
	size int64,
	content io.Reader,
) error {
	if sink.modes != nil {
		sink.modes[path.Path()] = mode.Perm()
	}
	return sink.builder.AddFile(sink.ctx, path.Path(), mode.Perm()&0o111 != 0, size, content)
}

func (sink *DirectoryHashSink) Sum() (artifact.ContentHash, error) {
	return sink.builder.Sum()
}
