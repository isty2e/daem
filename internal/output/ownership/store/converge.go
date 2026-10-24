package store

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output/ownership"
)

// Converge publishes one canonical successor using the convergence's exact
// expected-or-target matching. Document fragments must match a complete phase.
func (store Store) Converge(
	ctx context.Context,
	convergence ownership.ClaimConvergence,
) (ownership.Registry, error) {
	bindings, err := ownership.NewDocumentBindingConvergence(nil)
	if err != nil {
		return ownership.Registry{}, err
	}
	return store.ConvergeRegistry(ctx, convergence, bindings)
}

// ConvergeRegistry settles claims and document bindings in one guarded write.
// A refusal in either family publishes neither successor.
func (store Store) ConvergeRegistry(
	ctx context.Context,
	claims ownership.ClaimConvergence,
	bindings ownership.DocumentBindingConvergence,
) (ownership.Registry, error) {
	if ctx == nil {
		return ownership.Registry{}, fmt.Errorf("ownership registry context is required")
	}
	if err := ctx.Err(); err != nil {
		return ownership.Registry{}, err
	}
	if err := claims.Validate(); err != nil {
		return ownership.Registry{}, err
	}
	if err := bindings.Validate(); err != nil {
		return ownership.Registry{}, err
	}

	current, expectedEntry, exists, capability, err := store.loadForConvergence(ctx, claims.ExpectedRemovals())
	if err != nil {
		return ownership.Registry{}, err
	}
	capabilityConsumed := false
	defer func() {
		if capability != nil && !capabilityConsumed {
			_ = capability.Close()
		}
	}()

	next, claimsChanged, err := claims.Apply(current)
	if err != nil {
		return ownership.Registry{}, err
	}
	next, bindingsChanged, err := bindings.Apply(next)
	if err != nil {
		return ownership.Registry{}, err
	}
	if err := ctx.Err(); err != nil {
		return ownership.Registry{}, err
	}
	if !claimsChanged && !bindingsChanged {
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
