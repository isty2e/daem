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

func (store Store) loadRootedForCommit(ctx context.Context, expected []ownership.Claim, observations *pathAuthorityObservationSession) (ownership.Registry, storagecommit.EntryIdentity, bool, rootedpath.CommitCapability, error) {
	if store.root == nil {
		return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf("rooted ownership registry authority is required")
	}
	if observations == nil || !observations.bounded() {
		return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf("rooted ownership registry budget is unavailable")
	}
	capability, err := store.root.AcquireBounded(store.destination, observations.maximumPhysicalDepth, observations.budget)
	if err != nil {
		return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf("acquire ownership registry: %w", err)
	}
	content, mode, identity, err := storagecommit.ReadRootedRegularFileUpTo(ctx, capability, maximumOwnershipRegistryBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ownership.EmptyRegistry(), storagecommit.EntryIdentity{}, false, capability, nil
		}
		_ = capability.Close()
		return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf("read ownership registry: %w", err)
	}
	if mode.Perm()&0o077 != 0 {
		_ = capability.Close()
		return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf("ownership registry %q permissions %04o expose authority metadata", store.path, mode.Perm())
	}
	registry, err := decodePersistedRegistryForClaimRemovals(ctx, content, expected, observations)
	if err != nil {
		_ = capability.Close()
		return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, err
	}
	return registry, identity, true, capability, nil
}
