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
	budget *recovery.PhysicalWorkBudget,
) (recoveryPathObservation, error) {
	base := recoveryPathObservation{Path: journalPath, ContentPath: contentPath}
	if manifestAuthority == nil {
		base.Error = "project root authority is required"
		return base, nil
	}
	destination, err := output.Parse(journalPath)
	if err != nil {
		base.Error = fmt.Sprintf("parse project destination: %v", err)
		return base, nil
	}
	capability, err := manifestAuthority.acquireBounded(destination, budget)
	if err != nil {
		base.Error = fmt.Sprintf("acquire project destination: %v", err)
		return base, nil
	}
	observation, observeErr := observeRootedRecoveryCapability(
		ctx,
		journalPath,
		contentPath,
		aggregateContract,
		filesystem,
		capability,
		codecs,
		budget,
	)
	closeErr := capability.Close()
	if observeErr != nil {
		return recoveryPathObservation{}, errors.Join(observeErr, closeErr)
	}
	if closeErr != nil {
		if observation.Error != "" {
			observation.Error += fmt.Sprintf("; close project destination capability: %v", closeErr)
		} else {
			observation.Error = fmt.Sprintf("close project destination capability: %v", closeErr)
		}
	}
	return observation, nil
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
	budget *recovery.PhysicalWorkBudget,
) (recoveryPathObservation, error) {
	base := recoveryPathObservation{Path: journalPath, ContentPath: contentPath}
	destination, err := output.Parse(journalPath)
	if err != nil {
		base.Error = fmt.Sprintf("parse global destination: %v", err)
		return base, nil
	}
	capability, present, err := acquireMatchingRootedCapability(
		destination,
		resolvedPath,
		resolver,
		budget,
	)
	if err != nil {
		base.Error = fmt.Sprintf("acquire global destination: %v", err)
		return base, nil
	}
	if !present {
		if resolver != nil {
			base.Error = "global destination has no retained root authority"
			return base, nil
		}
		root, bound, captureErr := rootedpath.CaptureDestinationBounded(
			resolvedPath,
			recovery.MaximumPhysicalPathDepth,
			budget,
		)
		if captureErr != nil {
			base.Error = fmt.Sprintf("capture global destination: %v", captureErr)
			return base, nil
		}
		capability, err = root.AcquireBounded(
			bound,
			recovery.MaximumPhysicalPathDepth,
			budget,
		)
		closeErr := root.Close()
		if err != nil || closeErr != nil {
			if capability != nil {
				closeErr = errors.Join(closeErr, capability.Close())
			}
			base.Error = fmt.Sprintf("acquire captured global destination: %v", errors.Join(err, closeErr))
			return base, nil
		}
	}
	observation, observeErr := observeRootedRecoveryCapability(
		ctx,
		journalPath,
		contentPath,
		aggregateContract,
		filesystem,
		capability,
		codecs,
		budget,
	)
	closeErr := capability.Close()
	if observeErr != nil {
		return recoveryPathObservation{}, errors.Join(observeErr, closeErr)
	}
	if closeErr != nil {
		if observation.Error != "" {
			observation.Error += fmt.Sprintf("; close global destination capability: %v", closeErr)
		} else {
			observation.Error = fmt.Sprintf("close global destination capability: %v", closeErr)
		}
	}
	return observation, nil
}

func observeRootedRecoveryCapability(
	ctx context.Context,
	journalPath string,
	contentPath string,
	aggregateContract *aggregate.ProjectionContract,
	filesystem mutationfs.RootedReader,
	capability rootedpath.CommitCapability,
	codecs aggregate.CodecCatalog,
	budget *recovery.PhysicalWorkBudget,
) (recoveryPathObservation, error) {
	base := recoveryPathObservation{Path: journalPath, ContentPath: contentPath}
	identity, err := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return base, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return recoveryPathObservation{}, err
		}
		var rootedFailure *rootedpath.Failure
		if errors.As(err, &rootedFailure) && rootedFailure.Kind() == rootedpath.FailureFinalSymlink {
			return observeRootedRecoverySymlink(
				ctx,
				base,
				contentPath,
				filesystem,
				capability,
				budget,
			)
		}
		base.Error = fmt.Sprintf("inspect rooted destination: %v", err)
		return base, nil
	}

	switch identity.Kind() {
	case mutationfs.EntryKindFile:
		maximumBytes, err := recoveryRegularFileMaximumBytes(contentPath, aggregateContract, codecs)
		if err != nil {
			base.Exists = true
			base.Error = err.Error()
			return base, nil
		}
		maximum, readerCapacity, err := recoveryArtifactWorkLimits(false, maximumBytes, budget)
		if err != nil {
			return recoveryPathObservation{}, err
		}
		content, mode, _, err := filesystem.ReadRootedRegularFileUpTo(
			ctx,
			capability,
			readerCapacity.Bytes(),
		)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return recoveryPathObservation{}, err
			}
			if chargeErr := budget.AdmitIndeterminateTreeWork(maximum, readerCapacity); chargeErr != nil {
				return recoveryPathObservation{}, chargeErr
			}
			base.Exists = true
			base.Error = fmt.Sprintf("read rooted destination: %v", err)
			return base, nil
		}
		work, err := recovery.NewArtifactWork(0, int64(len(content)))
		if err != nil {
			return recoveryPathObservation{}, err
		}
		if err := budget.AdmitTreeWithin(work, maximum); err != nil {
			return recoveryPathObservation{}, err
		}
		if contentPath != "" {
			observation := observeRootedRecoveryContentPath(
				journalPath,
				contentPath,
				content,
				mode,
				aggregateContract,
				codecs,
			)
			observation.Work = work
			return observation, nil
		}
		base.Exists = true
		base.PathMode = recovery.NewPermissionMode(mode)
		base.Kind = recovery.PathKindFile
		base.ContentHash = string(artifact.HashFileContentWithExecutable(
			content,
			mode.Perm()&0o111 != 0,
		))
		base.Work = work
		return base, nil
	case mutationfs.EntryKindDirectory:
		base.Exists = true
		if contentPath != "" {
			base.Error = "content path requires a regular file"
			return base, nil
		}
		sink := newRootedTreeHashSink(ctx)
		limits, maximum, readerCapacity, err := recoveryTreeTraversalLimitsForBudget(
			budget.RemainingTreeWork(),
			budget,
		)
		if err != nil {
			return recoveryPathObservation{}, err
		}
		if _, err := filesystem.SnapshotRootedDirectory(
			ctx,
			capability,
			limits,
			sink,
		); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return recoveryPathObservation{}, err
			}
			if chargeErr := budget.AdmitIndeterminateDirectoryWork(maximum, readerCapacity); chargeErr != nil {
				return recoveryPathObservation{}, chargeErr
			}
			base.Error = fmt.Sprintf("hash rooted destination: %v", err)
			return base, nil
		}
		work, err := sink.removalWork()
		if err != nil {
			return recoveryPathObservation{}, err
		}
		if err := budget.AdmitTreeWithin(work, maximum); err != nil {
			return recoveryPathObservation{}, err
		}
		contentHash, err := sink.hash()
		if err != nil {
			base.Error = fmt.Sprintf("hash project destination: %v", err)
			return base, nil
		}
		base.Kind = recovery.PathKindDirectory
		base.ContentHash = string(contentHash)
		base.Work = work
		return base, nil
	default:
		base.Exists = true
		base.Error = fmt.Sprintf("unsupported rooted destination kind %q", identity.Kind())
		return base, nil
	}
}

func observeRootedRecoverySymlink(
	ctx context.Context,
	base recoveryPathObservation,
	contentPath string,
	filesystem mutationfs.RootedReader,
	capability rootedpath.CommitCapability,
	budget *recovery.PhysicalWorkBudget,
) (recoveryPathObservation, error) {
	base.Exists = true
	if contentPath != "" {
		base.Error = "content path requires a regular file"
		return base, nil
	}
	target, _, err := filesystem.ReadRootedSymlinkTarget(ctx, capability)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return recoveryPathObservation{}, err
		}
		base.Error = fmt.Sprintf("read rooted symbolic link: %v", err)
		return base, nil
	}
	base.Kind = recovery.PathKindSymlink
	base.LinkTarget = target
	return base, nil
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
	ctx     context.Context
	hasher  *artifact.DirectoryHashBuilder
	entries int
	bytes   int64
}

func newRootedTreeHashSink(ctx context.Context) *rootedTreeHashSink {
	return &rootedTreeHashSink{ctx: ctx, hasher: artifact.NewDirectoryHashBuilder()}
}

func (*rootedTreeHashSink) VisitRoot(fs.FileMode) error {
	return nil
}

func (sink *rootedTreeHashSink) VisitDirectory(path mutationfs.TreeRelativePath, _ fs.FileMode) error {
	sink.entries++
	return sink.hasher.AddDirectory(path.Path())
}

func (sink *rootedTreeHashSink) VisitRegularFile(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
	size int64,
	content io.Reader,
) error {
	sink.entries++
	sink.bytes += size
	return sink.hasher.AddFile(
		sink.ctx,
		path.Path(),
		mode.Perm()&0o111 != 0,
		size,
		content,
	)
}

func (sink *rootedTreeHashSink) removalWork() (recovery.ArtifactWork, error) {
	return recovery.NewArtifactWork(sink.entries, sink.bytes)
}

func (sink *rootedTreeHashSink) hash() (artifact.ContentHash, error) {
	return sink.hasher.Sum()
}
