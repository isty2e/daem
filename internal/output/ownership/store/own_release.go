package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output/ownership"
)

func BindOwnReleaseRooted(root *rootedpath.CapturedRoot, destination rootedpath.Destination, depth int, budget rootedpath.PhysicalTraversalBudget) (ownershipmutation.OwnReleaseRegistryStore, error) {
	store, err := NewRooted(root, destination, depth, budget)
	if err != nil {
		return nil, err
	}
	return store, nil
}

func (store Store) LoadRecordedRegistry(ctx context.Context) (ownership.Registry, error) {
	return store.LoadProtectionRegistry(ctx)
}

// ConvergeOwnReleaseAfterCommit settles one recorded own metadata release. The
// containing release-only native operation must hold its leases and establish
// the local FileSet commit first. This adapter grants no host-write authority.
// It authenticates the registry artifact without reobserving vanished host or
// state locators, preserves other owners, and publishes one guarded successor.
func (store Store) ConvergeOwnReleaseAfterCommit(ctx context.Context, release ownership.OwnDocumentRelease) (result ownership.Registry, returnErr error) {
	if ctx == nil {
		return ownership.Registry{}, fmt.Errorf("own release context is required")
	}
	if err := ctx.Err(); err != nil {
		return ownership.Registry{}, err
	}
	if err := release.Validate(); err != nil {
		return ownership.Registry{}, err
	}
	current, expectedEntry, exists, capability, err := store.loadRecordedForUpdate(ctx)
	if err != nil {
		return ownership.Registry{}, err
	}
	consumed := false
	defer func() {
		if capability != nil && !consumed {
			returnErr = errors.Join(returnErr, capability.Close())
		}
		returnErr = errors.Join(returnErr, ctx.Err())
		if returnErr != nil {
			result = ownership.Registry{}
		}
	}()
	next, changed, err := release.ApplyAfterCommit(current.registry)
	if err != nil {
		return ownership.Registry{}, err
	}
	if !changed {
		return next, nil
	}
	if !exists {
		return ownership.Registry{}, fmt.Errorf("own metadata exists without a registry artifact")
	}
	// The sealed release only removes facts; every surviving located identity
	// already has a recorded reference. No addition or live-path observer exists.
	content, err := current.encodeSuccessor(ctx, next, nil, nil)
	if err != nil {
		return ownership.Registry{}, err
	}
	if capability != nil {
		if _, accountsPayload := store.physicalWorkBudget.(mutation.PhysicalObservationBudget); accountsPayload {
			if err := capability.AdmitPhysicalWork(0, 1, int64(len(content))); err != nil {
				return ownership.Registry{}, err
			}
		}
	}
	var request storagecommit.FileCommit
	if capability != nil {
		request, err = storagecommit.NewRootedFileReplacement(capability, content, 0o600, expectedEntry)
	} else {
		request, err = storagecommit.NewFileReplacement(store.path, content, 0o600, expectedEntry)
	}
	if err != nil {
		return ownership.Registry{}, fmt.Errorf("prepare own metadata release: %w", err)
	}
	consumed = capability != nil
	if err := storagecommit.CommitFile(ctx, request); err != nil {
		return ownership.Registry{}, fmt.Errorf("commit own metadata release: %w", err)
	}
	return next, nil
}
