package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output/ownership"
)

// RemoveClaim applies an idempotent exact-claim-to-absent transition. The
// caller must retain mutation authority for the removed output; this operation
// permits only that expected claim's path to have become absent while keeping
// every other persisted authority check strict.
func (store Store) RemoveClaim(
	ctx context.Context,
	expected ownership.Claim,
) (ownership.Registry, error) {
	if ctx == nil {
		return ownership.Registry{}, fmt.Errorf("ownership registry context is required")
	}
	if err := ctx.Err(); err != nil {
		return ownership.Registry{}, err
	}
	if err := expected.Validate(); err != nil {
		return ownership.Registry{}, fmt.Errorf("removed ownership claim: %w", err)
	}

	current, expectedEntry, exists, capability, err := store.loadForClaimRemovals(ctx, []ownership.Claim{expected})
	if err != nil {
		return ownership.Registry{}, err
	}
	capabilityConsumed := false
	defer func() {
		if capability != nil && !capabilityConsumed {
			_ = capability.Close()
		}
	}()

	actual, present := current.Conflict(expected.Address())
	if !present {
		return current, nil
	}
	expectedValue, _ := ownership.PresentClaim(expected)
	actualValue, _ := ownership.PresentClaim(actual)
	if !actual.Equal(expected) {
		return ownership.Registry{}, &ownership.StaleClaimError{
			Address: expected.Address(), Expected: expectedValue, Actual: actualValue,
		}
	}
	next, err := current.Apply(expected.Address(), expectedValue, ownership.NoClaim())
	if err != nil {
		return ownership.Registry{}, err
	}
	content, err := encode(next)
	if err != nil {
		return ownership.Registry{}, err
	}

	var request storagecommit.FileCommit
	if capability != nil && exists {
		request, err = storagecommit.NewRootedFileReplacement(capability, content, 0o600, expectedEntry)
	} else if exists {
		request, err = storagecommit.NewFileReplacement(store.path, content, 0o600, expectedEntry)
	} else {
		return ownership.Registry{}, fmt.Errorf("ownership registry claim exists without a registry file")
	}
	if err != nil {
		return ownership.Registry{}, fmt.Errorf("prepare ownership registry claim removal: %w", err)
	}
	capabilityConsumed = capability != nil
	if err := storagecommit.CommitFile(ctx, request); err != nil {
		return ownership.Registry{}, fmt.Errorf("commit ownership registry claim removal: %w", err)
	}
	return next, nil
}

func (store Store) loadForClaimRemovals(
	ctx context.Context,
	expected []ownership.Claim,
) (
	ownership.Registry,
	storagecommit.EntryIdentity,
	bool,
	rootedpath.CommitCapability,
	error,
) {
	if store.root != nil {
		observations, observationErr := store.newPathAuthorityObservationSession()
		if observationErr != nil {
			return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, observationErr
		}
		if !observations.bounded() {
			return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf(
				"rooted ownership registry budget is unavailable",
			)
		}
		capability, err := store.root.AcquireBounded(
			store.destination,
			observations.maximumPhysicalDepth,
			observations.budget,
		)
		if err != nil {
			return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf(
				"acquire ownership registry: %w",
				err,
			)
		}
		content, mode, identity, err := storagecommit.ReadRootedRegularFileUpTo(
			ctx,
			capability,
			maximumOwnershipRegistryBytes,
		)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return ownership.EmptyRegistry(), storagecommit.EntryIdentity{}, false, capability, nil
			}
			_ = capability.Close()
			return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf(
				"read ownership registry: %w",
				err,
			)
		}
		if mode.Perm()&0o077 != 0 {
			_ = capability.Close()
			return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf(
				"ownership registry %q permissions %04o expose authority metadata",
				store.path,
				mode.Perm(),
			)
		}
		registry, err := decodePersistedRegistryForClaimRemovals(
			ctx,
			content,
			expected,
			observations,
		)
		if err != nil {
			_ = capability.Close()
			return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, err
		}
		return registry, identity, true, capability, nil
	}

	snapshot, err := storagecommit.ReadRegularFileSnapshotUpTo(
		ctx,
		store.path,
		maximumOwnershipRegistryBytes,
	)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ownership.EmptyRegistry(), storagecommit.EntryIdentity{}, false, nil, nil
		}
		return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf(
			"read ownership registry: %w",
			err,
		)
	}
	if snapshot.Mode().Perm()&0o077 != 0 {
		return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf(
			"ownership registry %q permissions %04o expose authority metadata",
			store.path,
			snapshot.Mode().Perm(),
		)
	}
	identity, ok := snapshot.Identity().(storagecommit.EntryIdentity)
	if !ok {
		return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf(
			"ownership registry snapshot has unsupported entry identity %T",
			snapshot.Identity(),
		)
	}
	registry, err := decodePersistedRegistryForClaimRemovals(
		ctx,
		snapshot.Content(),
		expected,
		newPathAuthorityObservationSession(),
	)
	if err != nil {
		return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, err
	}
	return registry, identity, true, nil, nil
}
