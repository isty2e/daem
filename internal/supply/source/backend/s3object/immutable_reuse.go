package s3object

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	sourcecache "github.com/isty2e/daem/internal/supply/source/cache"
	"github.com/isty2e/daem/internal/supply/source/directfile"
)

func (resolver Resolver) resolveOnce(ctx context.Context, request resolveRequest) (acquisition.Resolution, error) {
	state, err := resolver.requireState()
	if err != nil {
		return acquisition.Resolution{}, err
	}
	identity, eligible, err := newImmutableLookupIdentity(request.sourceID, request.s3Source.VersionID())
	if err != nil {
		return acquisition.Resolution{}, err
	}
	if !eligible {
		return resolver.resolveRemote(ctx, request)
	}

	lookupLock, err := state.immutableIndex.acquire(ctx, identity)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return acquisition.Resolution{}, contextErr
		}
		return resolver.resolveRemote(ctx, request)
	}

	resolved, resolveErr := resolver.resolveImmutableLocked(ctx, request, identity)
	releaseErr := lookupLock.Release()
	return finishImmutableLookup(resolved, resolveErr, releaseErr)
}

func finishImmutableLookup(
	resolved acquisition.Resolution,
	resolveErr error,
	releaseErr error,
) (acquisition.Resolution, error) {
	if err := errors.Join(resolveErr, releaseErr); err != nil {
		return acquisition.Resolution{}, err
	}
	return resolved, nil
}

func (resolver Resolver) resolveImmutableLocked(
	ctx context.Context,
	request resolveRequest,
	identity immutableLookupIdentity,
) (acquisition.Resolution, error) {
	state, err := resolver.requireState()
	if err != nil {
		return acquisition.Resolution{}, err
	}

	record, found, lookupErr := state.immutableIndex.read(ctx, identity)
	if lookupErr == nil && found {
		resolved, valid, verifyErr := resolver.verifyImmutableLookupArtifact(ctx, request, record)
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
			_ = state.immutableIndex.retire(ctx, identity)
		}
	}

	resolved, err := resolver.resolveRemote(ctx, request)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	record, err = newImmutableLookupRecord(identity, resolved)
	if err != nil {
		return resolved, nil
	}
	if err := state.immutableIndex.publish(ctx, identity, record); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return acquisition.Resolution{}, contextErr
		}
	}
	return resolved, nil
}

func (resolver Resolver) verifyImmutableLookupArtifact(
	ctx context.Context,
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
	err = state.artifactLocker.Do(ctx, key, func() error {
		if record.Kind == artifact.ArtifactKindFile {
			view, openErr := access.OpenView(filepath.Join(entryRoot, "content"))
			if openErr != nil {
				return openErr
			}
			if _, hashErr := directfile.Hash(ctx, view); hashErr != nil {
				return hashErr
			}
		}
		var verifyErr error
		valid, verifyErr = sourcecache.VerifyDirectory(ctx, entryRoot, spec)
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
