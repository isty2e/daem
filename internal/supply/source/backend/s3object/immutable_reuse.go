package s3object

import (
	"context"
	"errors"
	"path/filepath"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	sourcecache "github.com/isty2e/daem/internal/supply/source/cache"
	"github.com/isty2e/daem/internal/supply/source/directfile"
)

func (resolver Resolver) resolveOnce(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	request resolveRequest,
) (acquisition.Resolution, error) {
	state, err := resolver.requireState()
	if err != nil {
		return acquisition.Resolution{}, err
	}
	identity, eligible, err := newImmutableLookupIdentity(request.sourceID, request.s3Source.VersionID())
	if err != nil {
		return acquisition.Resolution{}, err
	}
	if !eligible {
		return resolver.resolveRemote(ctx, cacheRoot, request)
	}

	var resolved acquisition.Resolution
	lockBodyRan := false
	err = state.immutableIndex.doRooted(ctx, cacheRoot, identity, func() error {
		lockBodyRan = true
		var resolveErr error
		resolved, resolveErr = resolver.resolveImmutableLocked(ctx, cacheRoot, request, identity)
		return resolveErr
	})
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return acquisition.Resolution{}, contextErr
		}
		if lockBodyRan || isS3CacheAuthorityFailure(err) {
			return acquisition.Resolution{}, err
		}
		return resolver.resolveRemote(ctx, cacheRoot, request)
	}
	return resolved, nil
}

func (resolver Resolver) resolveImmutableLocked(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	request resolveRequest,
	identity immutableLookupIdentity,
) (acquisition.Resolution, error) {
	state, err := resolver.requireState()
	if err != nil {
		return acquisition.Resolution{}, err
	}

	record, found, lookupErr := state.immutableIndex.read(ctx, cacheRoot, identity)
	if lookupErr == nil && found {
		resolved, valid, verifyErr := resolver.verifyImmutableLookupArtifact(ctx, cacheRoot, request, record)
		if verifyErr == nil && valid {
			request.options.Emit(acquisition.EventCacheHit, request.sourceSpec, request.sourceID, record.ResolvedRef, nil)
			return resolved, nil
		}
		if errors.Is(verifyErr, directfile.ErrLimitExceeded) {
			return acquisition.Resolution{}, verifyErr
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return acquisition.Resolution{}, contextErr
		}
	} else {
		if contextErr := ctx.Err(); contextErr != nil {
			return acquisition.Resolution{}, contextErr
		}
		if lookupErr != nil {
			if isS3CacheAuthorityFailure(lookupErr) {
				return acquisition.Resolution{}, lookupErr
			}
			if retireErr := state.immutableIndex.retire(ctx, cacheRoot, identity); isS3CacheAuthorityFailure(retireErr) {
				return acquisition.Resolution{}, retireErr
			}
		}
	}

	resolved, err := resolver.resolveRemote(ctx, cacheRoot, request)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	record, err = newImmutableLookupRecord(identity, resolved)
	if err != nil {
		return resolved, nil
	}
	if err := state.immutableIndex.publish(ctx, cacheRoot, identity, record); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return acquisition.Resolution{}, contextErr
		}
		if isS3CacheAuthorityFailure(err) {
			return acquisition.Resolution{}, err
		}
	}
	return resolved, nil
}

func (resolver Resolver) verifyImmutableLookupArtifact(
	ctx context.Context,
	cacheRoot *rootedpath.CapturedRoot,
	request resolveRequest,
	record immutableLookupRecord,
) (acquisition.Resolution, bool, error) {
	state, err := resolver.requireState()
	if err != nil {
		return acquisition.Resolution{}, false, err
	}
	key, err := cacheKeyForS3Artifact(request.sourceID, record.ResolvedRef, record.ContentHash)
	if err != nil {
		return acquisition.Resolution{}, false, err
	}
	spec, err := sourcecache.NewEntrySpec(key, "content", record.ContentHash, record.Kind)
	if err != nil {
		return acquisition.Resolution{}, false, err
	}
	entryRoot := resolver.artifactEntryRoot(request.sourceID, record.ResolvedRef, record.ContentHash)

	request.options.Emit(acquisition.EventCacheWait, request.sourceSpec, request.sourceID, record.ResolvedRef, nil)
	valid := false
	err = state.artifactLocker.DoRooted(ctx, cacheRoot, key, func() error {
		var verifyErr error
		relativeRoot := resolver.artifactEntryRelativeRoot(
			request.sourceID,
			record.ResolvedRef,
			record.ContentHash,
		)
		if record.Kind == artifact.ArtifactKindFile {
			valid, verifyErr = sourcecache.VerifyFileRooted(
				ctx,
				cacheRoot,
				relativeRoot,
				spec,
				int(directfile.MaximumBytes),
			)
			var limitErr *sourcecache.VerifiedFileLimitError
			if errors.As(verifyErr, &limitErr) {
				return directfile.CheckKnownSize(limitErr.Observed())
			}
			return verifyErr
		}
		valid, verifyErr = sourcecache.VerifyDirectoryRooted(
			ctx,
			cacheRoot,
			relativeRoot,
			spec,
		)
		return verifyErr
	})
	if err != nil {
		return acquisition.Resolution{}, false, err
	}
	if !valid {
		return acquisition.Resolution{}, false, nil
	}
	resolved, err := resolutionFromMaterialized(
		ctx,
		request.sourceSpec,
		request.sourceID,
		record.ResolvedRef,
		filepath.Join(entryRoot, "content"),
		record.Kind,
		record.ContentHash,
	)
	if err != nil {
		return acquisition.Resolution{}, false, err
	}
	return resolved, true, nil
}

func isS3CacheAuthorityFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sourcecache.ErrRootedLockAuthority) {
		return true
	}
	var pathFailure *rootedpath.Failure
	if errors.As(err, &pathFailure) {
		return true
	}
	var limitErr *sourcecache.VerifiedFileLimitError
	if errors.As(err, &limitErr) {
		return false
	}
	_, classified := mutationfs.FailureKindOf(err)
	if classified {
		return true
	}
	return false
}
