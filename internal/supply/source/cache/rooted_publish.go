package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/supply/artifact"
)

// PublishDirectoryOnceRooted returns the exact content identity verified below
// a retained cache root, publishing a private build stage only when no valid
// entry exists. Only an identity-stable no-follow snapshot can enter the cache.
func PublishDirectoryOnceRooted(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	relativeRoot string,
	spec EntrySpec,
	build func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error),
) (
	contentHash artifact.ContentHash,
	contentKind artifact.ArtifactKind,
	published bool,
	returnErr error,
) {
	if err := validateContext(ctx, "rooted directory publish"); err != nil {
		return "", "", false, err
	}
	if err := ctx.Err(); err != nil {
		return "", "", false, err
	}
	if err := spec.validate(); err != nil {
		return "", "", false, err
	}
	if build == nil {
		return "", "", false, fmt.Errorf("rooted cache publish build function is required")
	}

	ready, verifiedHash, verifiedKind, verifyErr := prepareRootedDestination(ctx, root, relativeRoot, spec)
	if ready {
		return verifiedHash, verifiedKind, false, nil
	}
	if verifyErr != nil {
		return "", "", false, verifyErr
	}

	prepared, err := PrepareDirectory(ctx, spec.contentPath, build)
	if err != nil {
		return "", "", false, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, prepared.Close(context.WithoutCancel(ctx)))
	}()

	hash, kind, err := prepared.ContentIdentity()
	if err != nil {
		return "", "", false, err
	}
	if !spec.accepts(hash, kind) {
		return "", "", false, fmt.Errorf(
			"built cache content identity %q/%q does not match expected %q/%q",
			hash,
			kind,
			spec.expectedHash,
			spec.expectedKind,
		)
	}
	return publishPreparedRootedDirectory(ctx, root, relativeRoot, spec, prepared)
}

// PreparedDirectory owns one exact private build stage until Close. It allows a
// content-addressed caller to choose the final cache key after hashing without
// creating a pathname stage below the cache root.
type PreparedDirectory struct {
	stage       *privateBuildStage
	contentHash artifact.ContentHash
	contentKind artifact.ArtifactKind
}

// PrepareDirectory materializes and verifies one private directory stage.
func PrepareDirectory(
	ctx context.Context,
	contentPath string,
	build func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error),
) (prepared *PreparedDirectory, returnErr error) {
	if err := validateContext(ctx, "cache directory preparation"); err != nil {
		return nil, err
	}
	if build == nil {
		return nil, fmt.Errorf("cache directory preparation build function is required")
	}
	normalizedPath, err := normalizeContentPath(contentPath)
	if err != nil {
		return nil, err
	}
	stage, err := newPrivateBuildStage(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if prepared == nil {
			returnErr = errors.Join(returnErr, stage.close(context.WithoutCancel(ctx)))
		}
	}()

	hash, kind, err := build(stage.path)
	if err != nil {
		return nil, err
	}
	if err := validateContentIdentity(hash, kind); err != nil {
		return nil, fmt.Errorf("validate built cache content identity: %w", err)
	}
	observedHash, observedKind, err := stage.contentIdentity(ctx, normalizedPath)
	if err != nil {
		return nil, fmt.Errorf("verify private cache build stage content: %w", err)
	}
	if observedHash != hash || observedKind != kind {
		return nil, fmt.Errorf(
			"built cache content identity %q/%q does not match staged %q/%q",
			hash,
			kind,
			observedHash,
			observedKind,
		)
	}
	return &PreparedDirectory{
		stage:       stage,
		contentHash: hash,
		contentKind: kind,
	}, nil
}

// ContentIdentity returns the exact identity observed in the private stage.
func (prepared *PreparedDirectory) ContentIdentity() (
	artifact.ContentHash,
	artifact.ArtifactKind,
	error,
) {
	if prepared == nil || prepared.stage == nil {
		return "", "", fmt.Errorf("prepared cache directory is closed or uninitialized")
	}
	return prepared.contentHash, prepared.contentKind, nil
}

// PublishRooted publishes this exact stage below root. Spec must require the
// prepared identity so a concurrent cache hit cannot substitute other content.
func (prepared *PreparedDirectory) PublishRooted(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	relativeRoot string,
	spec EntrySpec,
) (bool, error) {
	if err := validateContext(ctx, "prepared rooted directory publish"); err != nil {
		return false, err
	}
	hash, kind, err := prepared.ContentIdentity()
	if err != nil {
		return false, err
	}
	if err := spec.validate(); err != nil {
		return false, err
	}
	if spec.expectedHash != hash || spec.expectedKind != kind {
		return false, fmt.Errorf(
			"prepared rooted cache publication requires exact identity %q/%q",
			hash,
			kind,
		)
	}
	ready, _, _, err := prepareRootedDestination(ctx, root, relativeRoot, spec)
	if err != nil || ready {
		return false, err
	}
	_, _, published, err := publishPreparedRootedDirectory(
		ctx,
		root,
		relativeRoot,
		spec,
		prepared,
	)
	return published, err
}

// Close removes the exact private stage. It is safe to call more than once.
func (prepared *PreparedDirectory) Close(ctx context.Context) error {
	if prepared == nil || prepared.stage == nil {
		return nil
	}
	stage := prepared.stage
	prepared.stage = nil
	return stage.close(ctx)
}

func prepareRootedDestination(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	relativeRoot string,
	spec EntrySpec,
) (bool, artifact.ContentHash, artifact.ArtifactKind, error) {
	state, hash, kind, verifyErr := verifyRootedDirectory(ctx, root, relativeRoot, spec)
	switch state {
	case rootedEntryValid:
		return true, hash, kind, nil
	case rootedEntryMissing:
		return false, "", "", nil
	case rootedEntryOwnedInvalid:
		if err := retireRootedDirectory(ctx, root, relativeRoot); err != nil {
			return false, "", "", errors.Join(verifyErr, err)
		}
		return false, "", "", nil
	default:
		return false, "", "", verifyErr
	}
}

func publishPreparedRootedDirectory(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	relativeRoot string,
	spec EntrySpec,
	prepared *PreparedDirectory,
) (artifact.ContentHash, artifact.ArtifactKind, bool, error) {
	hash, kind, err := prepared.ContentIdentity()
	if err != nil {
		return "", "", false, err
	}
	record, err := newCompletionRecord(spec, hash, kind)
	if err != nil {
		return "", "", false, err
	}
	recordContent, err := encodeCompletionRecord(record)
	if err != nil {
		return "", "", false, err
	}
	commitOutcome, commitErr := publishPrivateBuildStage(
		ctx,
		root,
		relativeRoot,
		prepared.stage,
		recordContent,
	)
	if commitErr != nil {
		state, verifiedHash, verifiedKind, verifyErr := verifyRootedDirectory(
			ctx,
			root,
			relativeRoot,
			spec,
		)
		if state == rootedEntryValid && rootedPublicationLostNoClobberRace(commitOutcome, commitErr) {
			return verifiedHash, verifiedKind, false, nil
		}
		return "", "", false, errors.Join(commitErr, verifyErr)
	}
	state, verifiedHash, verifiedKind, err := verifyRootedDirectory(
		ctx,
		root,
		relativeRoot,
		spec,
	)
	if err != nil {
		return "", "", false, err
	}
	if state != rootedEntryValid {
		return "", "", false, fmt.Errorf("published rooted cache entry is missing")
	}
	return verifiedHash, verifiedKind, true, nil
}

func publishPrivateBuildStage(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	relativeRoot string,
	stage *privateBuildStage,
	recordContent []byte,
) (mutationfs.CommitOutcome, error) {
	destination, lexicalPath, err := rootedEntryDestination(cacheRoot, relativeRoot)
	if err != nil {
		return mutationfs.CommitOutcome{}, err
	}
	capability, err := cacheRoot.Acquire(destination)
	if err != nil {
		return mutationfs.CommitOutcome{}, err
	}
	recordPath, err := mutationfs.NewTreeRelativePath(completionRecordName)
	if err != nil {
		_ = capability.Close()
		return mutationfs.CommitOutcome{}, err
	}
	prepared, err := storagecommit.PrepareRootedTreeWithLimits(
		ctx,
		capability,
		cacheEnvelopeTraversalLimits(),
		func(writer mutationfs.RootedTreeWriter) error {
			sourceCapability, err := stage.capability()
			if err != nil {
				return err
			}
			defer sourceCapability.Close()
			sink := rootedTreeCopySink{writer: writer}
			if err := stage.validate(ctx); err != nil {
				return err
			}
			_, err = storagecommit.SnapshotRootedDirectory(
				ctx,
				sourceCapability,
				cacheEnvelopeTraversalLimits(),
				sink,
			)
			if err != nil {
				return err
			}
			if err := stage.validate(ctx); err != nil {
				return fmt.Errorf("rooted cache build stage changed while copying: %w", err)
			}
			return writer.WriteFile(recordPath, 0o600, bytes.NewReader(recordContent))
		},
	)
	if err != nil {
		return mutationfs.CommitOutcome{}, fmt.Errorf(
			"prepare rooted cache publication for %q: %w",
			lexicalPath,
			err,
		)
	}
	outcome, err := prepared.CommitWithOutcome(ctx)
	if err != nil {
		return outcome, fmt.Errorf("commit rooted cache publication for %q: %w", lexicalPath, err)
	}
	return outcome, nil
}

func rootedPublicationLostNoClobberRace(
	outcome mutationfs.CommitOutcome,
	err error,
) bool {
	return outcome.State() == mutationfs.CommitOutcomeUncommitted && errors.Is(err, fs.ErrExist)
}

type privateBuildStage struct {
	root        *rootedpath.CapturedRoot
	witness     *rootedpath.CapturedRoot
	destination rootedpath.Destination
	path        string
}

func newPrivateBuildStage(ctx context.Context) (*privateBuildStage, error) {
	tempRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return nil, fmt.Errorf("resolve source cache staging parent: %w", err)
	}
	root, err := rootedpath.CaptureRootNoFollow(tempRoot)
	if err != nil {
		return nil, fmt.Errorf("capture source cache staging parent: %w", err)
	}
	stagePath, err := os.MkdirTemp(tempRoot, ".daem-source-cache-")
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("create private source cache build stage: %w", err)
	}
	stageIdentity, err := storagecommit.CaptureEntryIdentity(context.WithoutCancel(ctx), stagePath)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("capture new private source cache build stage: %w", err),
			os.Remove(stagePath),
			root.Close(),
		)
	}
	fail := func(cause error, witness *rootedpath.CapturedRoot) (*privateBuildStage, error) {
		var witnessErr error
		if witness != nil {
			witnessErr = witness.Close()
		}
		request, requestErr := storagecommit.NewLogicalRemoval(stagePath, stageIdentity)
		var removeErr error
		if requestErr == nil {
			removeErr = storagecommit.CommitLogicalRemoval(context.WithoutCancel(ctx), request)
		}
		return nil, errors.Join(cause, requestErr, removeErr, witnessErr, root.Close())
	}
	relative, err := rootedpath.NewRelativeDestination(filepath.Base(stagePath))
	if err != nil {
		return fail(fmt.Errorf("bind private source cache build stage: %w", err), nil)
	}
	authority, err := root.Authority()
	if err != nil {
		return fail(err, nil)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		return fail(err, nil)
	}
	witness, err := rootedpath.CaptureRootNoFollow(stagePath)
	if err != nil {
		return fail(fmt.Errorf("capture private source cache build stage: %w", err), nil)
	}
	if err := witness.ValidateSelection(stagePath); err != nil {
		return fail(fmt.Errorf("validate private source cache build stage: %w", err), witness)
	}
	return &privateBuildStage{
		root:        root,
		witness:     witness,
		destination: destination,
		path:        stagePath,
	}, nil
}

func (stage *privateBuildStage) capability() (rootedpath.CommitCapability, error) {
	if stage == nil || stage.root == nil {
		return nil, fmt.Errorf("private source cache build stage is not initialized")
	}
	return stage.root.Acquire(stage.destination)
}

func (stage *privateBuildStage) validate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if stage == nil || stage.witness == nil {
		return fmt.Errorf("private source cache build stage witness is not initialized")
	}
	return stage.witness.ValidateSelection(stage.path)
}

func (stage *privateBuildStage) contentIdentity(
	ctx context.Context,
	contentPath string,
) (artifact.ContentHash, artifact.ArtifactKind, error) {
	capability, err := stage.capability()
	if err != nil {
		return "", "", err
	}
	defer capability.Close()
	sink := newRootedContentHashSink(ctx, contentPath)
	if err := stage.validate(ctx); err != nil {
		return "", "", err
	}
	_, err = storagecommit.SnapshotRootedDirectory(
		ctx,
		capability,
		cacheEnvelopeTraversalLimits(),
		sink,
	)
	if err != nil {
		return "", "", err
	}
	if err := stage.validate(ctx); err != nil {
		return "", "", fmt.Errorf("private source cache build stage changed while hashing: %w", err)
	}
	return sink.result()
}

func (stage *privateBuildStage) close(ctx context.Context) error {
	if stage == nil || stage.root == nil {
		return nil
	}
	closeWitness := func() error {
		if stage.witness == nil {
			return nil
		}
		return stage.witness.Close()
	}
	if err := stage.validate(ctx); err != nil {
		return errors.Join(err, closeWitness(), stage.root.Close())
	}
	capability, err := stage.capability()
	if err != nil {
		return errors.Join(err, closeWitness(), stage.root.Close())
	}
	observed, err := storagecommit.CaptureRootedEntryIdentity(ctx, capability)
	if err != nil {
		_ = capability.Close()
		if isMissingRootedEntry(err) {
			return errors.Join(closeWitness(), stage.root.Close())
		}
		return errors.Join(err, closeWitness(), stage.root.Close())
	}
	request, err := storagecommit.NewRootedEntryCleanup(
		capability,
		observed,
		cacheEnvelopeTraversalLimits(),
	)
	if err != nil {
		_ = capability.Close()
		return errors.Join(err, closeWitness(), stage.root.Close())
	}
	_, removeErr := storagecommit.CommitRootedEntryCleanup(ctx, request)
	return errors.Join(removeErr, closeWitness(), stage.root.Close())
}

type rootedTreeCopySink struct {
	writer mutationfs.RootedTreeWriter
}

func (sink rootedTreeCopySink) VisitRoot(mode fs.FileMode) error {
	return sink.writer.SetRootMode(mode.Perm())
}

func (sink rootedTreeCopySink) VisitDirectory(
	relative mutationfs.TreeRelativePath,
	mode fs.FileMode,
) error {
	if relative.Path() == completionRecordName {
		return fmt.Errorf("cache build stage contains reserved completion record")
	}
	return sink.writer.CreateDirectory(relative, mode.Perm())
}

func (sink rootedTreeCopySink) VisitRegularFile(
	relative mutationfs.TreeRelativePath,
	mode fs.FileMode,
	_ int64,
	content io.Reader,
) error {
	if relative.Path() == completionRecordName {
		return fmt.Errorf("cache build stage contains reserved completion record")
	}
	return sink.writer.WriteFile(relative, mode.Perm(), content)
}
