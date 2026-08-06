package journal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func observeProjectRecoveryPath(
	ctx context.Context,
	journalPath string,
	contentPath string,
	aggregateContract *aggregate.ProjectionContract,
	filesystem mutationfs.RootedReader,
	manifestAuthority *manifestAuthoritySession,
	codecs aggregate.CodecCatalog,
) recoveryPathObservation {
	base := recoveryPathObservation{Path: journalPath, ContentPath: contentPath}
	if manifestAuthority == nil {
		base.Error = "project root authority is required"
		return base
	}
	destination, err := output.Parse(journalPath)
	if err != nil {
		base.Error = fmt.Sprintf("parse project destination: %v", err)
		return base
	}
	capability, err := manifestAuthority.acquire(destination)
	if err != nil {
		base.Error = fmt.Sprintf("acquire project destination: %v", err)
		return base
	}
	observation := observeRootedRecoveryCapability(
		ctx,
		journalPath,
		contentPath,
		aggregateContract,
		filesystem,
		capability,
		codecs,
	)
	if closeErr := capability.Close(); closeErr != nil {
		if observation.Error != "" {
			observation.Error += fmt.Sprintf("; close project destination capability: %v", closeErr)
		} else {
			observation.Error = fmt.Sprintf("close project destination capability: %v", closeErr)
		}
	}
	return observation
}

func observeGlobalRecoveryPath(
	ctx context.Context,
	journalPath string,
	contentPath string,
	aggregateContract *aggregate.ProjectionContract,
	resolvedPath string,
	filesystem mutationfs.RootedReader,
	resolver RootedCapabilityResolver,
	codecs aggregate.CodecCatalog,
) recoveryPathObservation {
	base := recoveryPathObservation{Path: journalPath, ContentPath: contentPath}
	destination, err := output.Parse(journalPath)
	if err != nil {
		base.Error = fmt.Sprintf("parse global destination: %v", err)
		return base
	}
	capability, present, err := acquireMatchingRootedCapability(
		destination,
		resolvedPath,
		resolver,
	)
	if err != nil {
		base.Error = fmt.Sprintf("acquire global destination: %v", err)
		return base
	}
	if !present {
		base.Error = "global destination has no retained root authority"
		return base
	}
	observation := observeRootedRecoveryCapability(
		ctx,
		journalPath,
		contentPath,
		aggregateContract,
		filesystem,
		capability,
		codecs,
	)
	if closeErr := capability.Close(); closeErr != nil {
		if observation.Error != "" {
			observation.Error += fmt.Sprintf("; close global destination capability: %v", closeErr)
		} else {
			observation.Error = fmt.Sprintf("close global destination capability: %v", closeErr)
		}
	}
	return observation
}

func observeRootedRecoveryCapability(
	ctx context.Context,
	journalPath string,
	contentPath string,
	aggregateContract *aggregate.ProjectionContract,
	filesystem mutationfs.RootedReader,
	capability rootedpath.CommitCapability,
	codecs aggregate.CodecCatalog,
) recoveryPathObservation {
	base := recoveryPathObservation{Path: journalPath, ContentPath: contentPath}
	identity, err := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return base
		}
		base.Error = fmt.Sprintf("inspect rooted destination: %v", err)
		return base
	}

	switch identity.Kind() {
	case mutationfs.EntryKindFile:
		maximumBytes, err := recoveryRegularFileMaximumBytes(contentPath, aggregateContract, codecs)
		if err != nil {
			base.Exists = true
			base.Error = err.Error()
			return base
		}
		content, mode, _, err := filesystem.ReadRootedRegularFileUpTo(
			ctx,
			capability,
			maximumBytes,
		)
		if err != nil {
			base.Exists = true
			base.Error = fmt.Sprintf("read rooted destination: %v", err)
			return base
		}
		if contentPath != "" {
			return observeRootedRecoveryContentPath(
				journalPath,
				contentPath,
				content,
				mode,
				aggregateContract,
				codecs,
			)
		}
		base.Exists = true
		base.PathMode = recovery.NewPermissionMode(mode)
		base.Kind = recovery.PathKindFile
		base.ContentHash = string(artifact.HashFileContentWithExecutable(
			content,
			mode.Perm()&0o111 != 0,
		))
		return base
	case mutationfs.EntryKindDirectory:
		base.Exists = true
		if contentPath != "" {
			base.Error = "content path requires a regular file"
			return base
		}
		sink := newRootedTreeHashSink(ctx)
		if _, err := filesystem.SnapshotRootedDirectory(
			ctx,
			capability,
			recoveryTreeTraversalLimits(),
			sink,
		); err != nil {
			base.Error = fmt.Sprintf("hash rooted destination: %v", err)
			return base
		}
		contentHash, err := sink.hash()
		if err != nil {
			base.Error = fmt.Sprintf("hash project destination: %v", err)
			return base
		}
		base.Kind = recovery.PathKindDirectory
		base.ContentHash = string(contentHash)
		return base
	default:
		base.Exists = true
		base.Error = fmt.Sprintf("unsupported rooted destination kind %q", identity.Kind())
		return base
	}
}

func observeRootedRecoveryContentPath(
	journalPath string,
	contentPath string,
	content []byte,
	mode fs.FileMode,
	aggregateContract *aggregate.ProjectionContract,
	codecs aggregate.CodecCatalog,
) recoveryPathObservation {
	base := recoveryPathObservation{
		Path:        journalPath,
		ContentPath: contentPath,
		PathExisted: true,
		PathMode:    recovery.NewPermissionMode(mode),
	}
	destination, err := output.Parse(journalPath)
	if err != nil {
		base.Error = err.Error()
		return base
	}
	projection, present, err := extractRecoveryObservationProjection(
		content,
		destination,
		output.ContentPath(contentPath),
		aggregateContract,
		codecs,
	)
	if err != nil {
		base.Exists = true
		base.Error = err.Error()
		return base
	}
	if !present {
		return base
	}
	base.Exists = true
	base.Kind = recovery.PathKindFile
	base.ContentHash = string(artifact.HashFileContent(projection))
	return base
}

type rootedTreeHashSink struct {
	ctx    context.Context
	hasher *artifact.DirectoryHashBuilder
}

func newRootedTreeHashSink(ctx context.Context) *rootedTreeHashSink {
	return &rootedTreeHashSink{ctx: ctx, hasher: artifact.NewDirectoryHashBuilder()}
}

func (*rootedTreeHashSink) VisitRoot(fs.FileMode) error {
	return nil
}

func (sink *rootedTreeHashSink) VisitDirectory(path mutationfs.TreeRelativePath, _ fs.FileMode) error {
	return sink.hasher.AddDirectory(path.Path())
}

func (sink *rootedTreeHashSink) VisitRegularFile(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
	size int64,
	content io.Reader,
) error {
	return sink.hasher.AddFile(
		sink.ctx,
		path.Path(),
		mode.Perm()&0o111 != 0,
		size,
		content,
	)
}

func (sink *rootedTreeHashSink) hash() (artifact.ContentHash, error) {
	return sink.hasher.Sum()
}
