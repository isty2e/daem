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

	state, verifiedHash, verifiedKind, verifyErr := verifyRootedDirectory(
		ctx,
		root,
		relativeRoot,
		spec,
	)
	switch state {
	case rootedEntryValid:
		return verifiedHash, verifiedKind, false, nil
	case rootedEntryMissing:
	case rootedEntryOwnedInvalid:
		if err := retireRootedDirectory(ctx, root, relativeRoot); err != nil {
			return "", "", false, errors.Join(verifyErr, err)
		}
	default:
		return "", "", false, verifyErr
	}

	stage, err := newPrivateBuildStage(ctx)
	if err != nil {
		return "", "", false, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, stage.close(context.WithoutCancel(ctx)))
	}()

	hash, kind, err := build(stage.path)
	if err != nil {
		return "", "", false, err
	}
	if err := stage.validate(ctx); err != nil {
		return "", "", false, fmt.Errorf("validate rooted cache build stage: %w", err)
	}
	if err := validateContentIdentity(hash, kind); err != nil {
		return "", "", false, fmt.Errorf("validate built cache content identity: %w", err)
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
	observedHash, observedKind, err := stage.contentIdentity(ctx, spec.contentPath)
	if err != nil {
		return "", "", false, fmt.Errorf("verify rooted cache build stage content: %w", err)
	}
	if observedHash != hash || observedKind != kind {
		return "", "", false, fmt.Errorf(
			"built cache content identity %q/%q does not match staged %q/%q",
			hash,
			kind,
			observedHash,
			observedKind,
		)
	}

	record, err := newCompletionRecord(spec, hash, kind)
	if err != nil {
		return "", "", false, err
	}
	recordContent, err := encodeCompletionRecord(record)
	if err != nil {
		return "", "", false, err
	}
	if err := publishPrivateBuildStage(ctx, root, relativeRoot, stage, recordContent); err != nil {
		state, verifiedHash, verifiedKind, verifyErr := verifyRootedDirectory(
			ctx,
			root,
			relativeRoot,
			spec,
		)
		if state == rootedEntryValid {
			return verifiedHash, verifiedKind, false, nil
		}
		return "", "", false, errors.Join(err, verifyErr)
	}
	state, verifiedHash, verifiedKind, err = verifyRootedDirectory(
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
) error {
	destination, lexicalPath, err := rootedEntryDestination(cacheRoot, relativeRoot)
	if err != nil {
		return err
	}
	capability, err := cacheRoot.Acquire(destination)
	if err != nil {
		return err
	}
	recordPath, err := mutationfs.NewTreeRelativePath(completionRecordName)
	if err != nil {
		_ = capability.Close()
		return err
	}
	prepared, err := storagecommit.PrepareRootedTree(
		ctx,
		capability,
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
				cacheTreeTraversalLimits(),
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
		return fmt.Errorf("prepare rooted cache publication for %q: %w", lexicalPath, err)
	}
	if err := prepared.Commit(ctx); err != nil {
		return fmt.Errorf("commit rooted cache publication for %q: %w", lexicalPath, err)
	}
	return nil
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
		cacheTreeTraversalLimits(),
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
	request, err := storagecommit.NewRootedLogicalRemoval(capability, observed)
	if err != nil {
		_ = capability.Close()
		return errors.Join(err, closeWitness(), stage.root.Close())
	}
	removeErr := storagecommit.CommitLogicalRemoval(ctx, request)
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
