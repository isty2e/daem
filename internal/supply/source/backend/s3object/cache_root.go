package s3object

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

const s3CacheAnchorName = ".daem-cache-root"

func (resolver Resolver) captureCacheRoot(ctx context.Context) (*rootedpath.CapturedRoot, error) {
	state, err := resolver.requireState()
	if err != nil {
		return nil, err
	}
	if err := storagecommit.PrepareCommitParent(
		ctx,
		filepath.Join(state.cacheRoot, s3CacheAnchorName),
	); err != nil {
		return nil, fmt.Errorf("prepare S3 source cache root: %w", err)
	}
	root, err := rootedpath.CaptureRootNoFollow(state.cacheRoot)
	if err != nil {
		return nil, fmt.Errorf("capture S3 source cache root authority: %w", err)
	}
	if err := validateS3CacheNamespaces(ctx, root); err != nil {
		_ = root.Close()
		return nil, err
	}
	return root, nil
}

func validateS3CacheNamespaces(ctx context.Context, root *rootedpath.CapturedRoot) error {
	for _, name := range []string{"artifacts", "indexes", "locks"} {
		authority, err := root.Authority()
		if err != nil {
			return err
		}
		relative, err := rootedpath.NewRelativeDestination(name)
		if err != nil {
			return err
		}
		destination, err := authority.Bind(relative)
		if err != nil {
			return err
		}
		capability, err := root.Acquire(destination)
		if err != nil {
			return err
		}
		identity, err := storagecommit.CaptureRootedEntryIdentity(ctx, capability)
		closeErr := capability.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("validate S3 cache namespace %q: %w", name, err)
		}
		if identity.Kind() != mutationfs.EntryKindDirectory {
			return fmt.Errorf("S3 cache namespace %q is not a directory", name)
		}
	}
	return nil
}
