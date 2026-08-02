package cache

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

// PublishDirectoryOnce builds and durably publishes finalRoot once. It returns
// true only when this call publishes the final root. Present invalid entries
// are identity-guarded retired before rebuilding. The caller must hold the
// exact entry lock for the entire call.
func PublishDirectoryOnce(
	ctx context.Context,
	finalRoot string,
	spec EntrySpec,
	build func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error),
) (bool, error) {
	if err := validateContext(ctx, "directory publish"); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if finalRoot == "" {
		return false, fmt.Errorf("cache final root is required")
	}
	if err := spec.validate(); err != nil {
		return false, err
	}
	if build == nil {
		return false, fmt.Errorf("cache publish build function is required for %q", finalRoot)
	}

	finalRoot, err := canonicalCacheEntryPath(finalRoot)
	if err != nil {
		return false, err
	}
	ready, err := prepareDestination(ctx, finalRoot, spec)
	if err != nil || ready {
		return false, err
	}
	if _, err := storagecommit.PrepareCommitParent(ctx, finalRoot); err != nil {
		return false, fmt.Errorf("prepare cache publication parent for %q: %w", finalRoot, err)
	}

	tempRoot, err := os.MkdirTemp(filepath.Dir(finalRoot), "."+filepath.Base(finalRoot)+".*.tmp")
	if err != nil {
		return false, fmt.Errorf("create temporary cache directory for %q: %w", finalRoot, err)
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(tempRoot)
		}
	}()

	hash, kind, err := build(tempRoot)
	if err != nil {
		return false, err
	}
	published, err = publishPreparedDirectory(ctx, tempRoot, finalRoot, spec, hash, kind)
	return published, err
}

// PublishPreparedDirectory durably publishes an already prepared same-parent
// cache tree. The caller must hold the exact entry lock for the entire call and
// retains cleanup ownership when this returns false.
func PublishPreparedDirectory(
	ctx context.Context,
	tempRoot string,
	finalRoot string,
	spec EntrySpec,
	hash artifact.ContentHash,
	kind artifact.ArtifactKind,
) (bool, error) {
	if err := validateContext(ctx, "prepared directory publish"); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if tempRoot == "" || finalRoot == "" {
		return false, fmt.Errorf("cache prepared and final roots are required")
	}
	tempRoot, err := canonicalCacheEntryPath(tempRoot)
	if err != nil {
		return false, err
	}
	finalRoot, err = canonicalCacheEntryPath(finalRoot)
	if err != nil {
		return false, err
	}
	if filepath.Dir(tempRoot) != filepath.Dir(finalRoot) {
		return false, fmt.Errorf("cache prepared and final roots must share one parent")
	}
	ready, err := prepareDestination(ctx, finalRoot, spec)
	if err != nil || ready {
		return false, err
	}
	return publishPreparedDirectory(ctx, tempRoot, finalRoot, spec, hash, kind)
}

func publishPreparedDirectory(
	ctx context.Context,
	tempRoot string,
	finalRoot string,
	spec EntrySpec,
	hash artifact.ContentHash,
	kind artifact.ArtifactKind,
) (bool, error) {
	info, err := os.Lstat(tempRoot)
	if err != nil {
		return false, fmt.Errorf("inspect prepared cache tree %q: %w", tempRoot, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("prepared cache root %q is not a non-symlink directory", tempRoot)
	}
	contentPath, err := nonSymlinkContentPath(tempRoot, spec.contentPath)
	if err != nil {
		return false, fmt.Errorf("verify prepared cache content path for %q: %w", tempRoot, err)
	}
	preparedHash, preparedKind, err := access.HashPath(ctx, contentPath)
	if err != nil {
		return false, fmt.Errorf("verify prepared cache content for %q: %w", tempRoot, err)
	}
	if preparedHash != hash || preparedKind != kind {
		return false, fmt.Errorf(
			"prepared cache content identity %q/%q does not match declared %q/%q",
			preparedHash,
			preparedKind,
			hash,
			kind,
		)
	}
	record, err := newCompletionRecord(spec, hash, kind)
	if err != nil {
		return false, err
	}
	encoded, err := encodeCompletionRecord(record)
	if err != nil {
		return false, err
	}
	if err := writeCompletionRecord(filepath.Join(tempRoot, completionRecordName), encoded); err != nil {
		return false, fmt.Errorf("write cache completion record for %q: %w", tempRoot, err)
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	identity, err := storagecommit.CaptureEntryIdentity(ctx, tempRoot)
	if err != nil {
		return false, fmt.Errorf("capture prepared cache tree %q: %w", tempRoot, err)
	}
	request, err := storagecommit.NewPreparedTreeCommit(tempRoot, finalRoot, identity)
	if err != nil {
		return false, fmt.Errorf("construct cache tree publication for %q: %w", finalRoot, err)
	}
	if err := storagecommit.CommitPreparedTree(ctx, request); err != nil {
		valid, verifyErr := VerifyDirectory(ctx, finalRoot, spec)
		kind, classified := mutationfs.FailureKindOf(err)
		if valid && classified && kind == mutationfs.FailureUncommitted {
			return false, nil
		}
		if verifyErr != nil {
			return false, errors.Join(err, verifyErr)
		}
		return false, err
	}
	valid, err := VerifyDirectory(ctx, finalRoot, spec)
	if err != nil {
		return false, err
	}
	if !valid {
		return false, fmt.Errorf("published cache entry %q is missing", finalRoot)
	}
	return true, nil
}

func writeCompletionRecord(path string, content []byte) (returnErr error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, file.Close())
	}()
	written, err := file.Write(content)
	if err != nil {
		return err
	}
	if written != len(content) {
		return fmt.Errorf("short completion record write: wrote %d of %d bytes", written, len(content))
	}
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("set completion record mode: %w", err)
	}
	return nil
}

func prepareDestination(ctx context.Context, finalRoot string, spec EntrySpec) (bool, error) {
	valid, err := VerifyDirectory(ctx, finalRoot, spec)
	if err == nil {
		return valid, nil
	}
	if !errors.Is(err, ErrInvalidEntry) {
		return false, err
	}
	if err := retireInvalidEntry(ctx, finalRoot); err != nil {
		return false, fmt.Errorf("retire invalid cache entry %q: %w", finalRoot, err)
	}
	return false, nil
}

// RetireDirectory identity-guards removal of exactly one cache entry. The
// caller must hold that entry's exact advisory lock. Missing entries are a
// successful no-op.
func RetireDirectory(ctx context.Context, root string) error {
	if err := validateContext(ctx, "cache entry retirement"); err != nil {
		return err
	}
	if root == "" {
		return fmt.Errorf("cache entry root is required")
	}
	canonicalRoot, err := canonicalCacheEntryPath(root)
	if err != nil {
		return err
	}
	return retireInvalidEntry(ctx, canonicalRoot)
}

func retireInvalidEntry(ctx context.Context, root string) error {
	identity, err := storagecommit.CaptureEntryIdentity(ctx, root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	request, err := storagecommit.NewLogicalRemoval(root, identity)
	if err != nil {
		return err
	}
	return storagecommit.CommitLogicalRemoval(ctx, request)
}
