package store

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output/ownership"
)

// Converge publishes one canonical ownership-registry successor. The current
// value at each address may be either the convergence's expected or target
// value, which makes retries and recovery idempotent without weakening stale
// third-state rejection.
func (store Store) Converge(
	ctx context.Context,
	convergence ownership.ClaimConvergence,
) (ownership.Registry, error) {
	if ctx == nil {
		return ownership.Registry{}, fmt.Errorf("ownership registry context is required")
	}
	if err := ctx.Err(); err != nil {
		return ownership.Registry{}, err
	}
	if err := convergence.Validate(); err != nil {
		return ownership.Registry{}, err
	}

	removals := convergence.ExpectedRemovals()
	current, expectedEntry, exists, capability, err := store.loadForConvergence(ctx, removals)
	if err != nil {
		return ownership.Registry{}, err
	}
	capabilityConsumed := false
	defer func() {
		if capability != nil && !capabilityConsumed {
			_ = capability.Close()
		}
	}()

	next, changed, err := convergence.Apply(current)
	if err != nil {
		return ownership.Registry{}, err
	}
	if err := ctx.Err(); err != nil {
		return ownership.Registry{}, err
	}
	if !changed {
		return next, nil
	}
	content, err := encode(next)
	if err != nil {
		return ownership.Registry{}, err
	}
	if err := ctx.Err(); err != nil {
		return ownership.Registry{}, err
	}

	var request storagecommit.FileCommit
	if capability != nil && exists {
		request, err = storagecommit.NewRootedFileReplacement(capability, content, 0o600, expectedEntry)
	} else if capability != nil {
		request, err = storagecommit.NewRootedFileCreate(capability, content, 0o600)
	} else if exists {
		request, err = storagecommit.NewFileReplacement(store.path, content, 0o600, expectedEntry)
	} else {
		request, err = storagecommit.NewFileCreate(store.path, content, 0o600)
	}
	if err != nil {
		return ownership.Registry{}, fmt.Errorf("prepare ownership registry convergence: %w", err)
	}
	capabilityConsumed = capability != nil
	if err := storagecommit.CommitFile(ctx, request); err != nil {
		return ownership.Registry{}, fmt.Errorf("commit ownership registry convergence: %w", err)
	}
	return next, nil
}

func (store Store) loadForConvergence(
	ctx context.Context,
	removals []ownership.Claim,
) (
	ownership.Registry,
	storagecommit.EntryIdentity,
	bool,
	rootedpath.CommitCapability,
	error,
) {
	if len(removals) != 0 {
		return store.loadForClaimRemovals(ctx, removals)
	}
	return store.loadForCommit(ctx)
}
