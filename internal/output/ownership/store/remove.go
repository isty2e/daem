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

	observations, err := store.newPathAuthorityObservationSession()
	if err != nil {
		return ownership.Registry{}, err
	}
	current, expectedEntry, exists, capability, err := store.loadForUpdate(ctx, []ownership.Claim{expected}, observations)
	if err != nil {
		return ownership.Registry{}, err
	}
	capabilityConsumed := false
	defer func() {
		if capability != nil && !capabilityConsumed {
			_ = capability.Close()
		}
	}()

	actual, present := current.registry.Conflict(expected.Address())
	if !present {
		return current.registry, nil
	}
	expectedValue, _ := ownership.PresentClaim(expected)
	actualValue, _ := ownership.PresentClaim(actual)
	if !actual.Equal(expected) {
		return ownership.Registry{}, &ownership.StaleClaimError{
			Address: expected.Address(), Expected: expectedValue, Actual: actualValue,
		}
	}
	next, err := current.registry.Apply(expected.Address(), expectedValue, ownership.NoClaim())
	if err != nil {
		return ownership.Registry{}, err
	}
	content, err := current.encodeSuccessor(ctx, next, nil, observations)
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

func (store Store) loadForUpdate(
	ctx context.Context,
	expected []ownership.Claim,
	observations *pathAuthorityObservationSession,
) (
	registryDocument,
	storagecommit.EntryIdentity,
	bool,
	rootedpath.CommitCapability,
	error,
) {
	if store.root != nil {
		return store.loadRootedForCommit(ctx, expected, observations)
	}

	snapshot, err := storagecommit.ReadRegularFileSnapshotUpTo(
		ctx,
		store.path,
		maximumOwnershipRegistryBytes,
	)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return registryDocument{registry: ownership.EmptyRegistry()}, storagecommit.EntryIdentity{}, false, nil, nil
		}
		return registryDocument{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf(
			"read ownership registry: %w",
			err,
		)
	}
	if snapshot.Mode().Perm()&0o077 != 0 {
		return registryDocument{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf(
			"ownership registry %q permissions %04o expose authority metadata",
			store.path,
			snapshot.Mode().Perm(),
		)
	}
	identity, ok := snapshot.Identity().(storagecommit.EntryIdentity)
	if !ok {
		return registryDocument{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf(
			"ownership registry snapshot has unsupported entry identity %T",
			snapshot.Identity(),
		)
	}
	document, err := decodePersistedDocument(
		ctx,
		snapshot.Content(),
		expected,
		observations,
	)
	if err != nil {
		return registryDocument{}, storagecommit.EntryIdentity{}, false, nil, err
	}
	return document, identity, true, nil, nil
}
