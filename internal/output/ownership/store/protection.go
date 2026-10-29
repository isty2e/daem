package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output/ownership"
)

var _ ownershipmutation.ProtectionRegistryStore = Store{}

func (store Store) LoadPendingProtections(ctx context.Context) ([]ownership.PendingProtection, error) {
	registry, _, _, capability, err := store.loadForProtection(ctx)
	if capability != nil {
		err = errors.Join(err, capability.Close())
	}
	if err != nil {
		return nil, err
	}
	return registry.PendingProtections(), nil
}

func (store Store) TransitionProtection(ctx context.Context, owner stateauthority.Authority, transition ownership.ProtectionTransition) (ownership.Registry, error) {
	if err := transition.Validate(); err != nil {
		return ownership.Registry{}, err
	}
	if err := owner.Validate(); err != nil {
		return ownership.Registry{}, err
	}
	current, expectedEntry, exists, capability, err := store.loadForProtection(ctx)
	if err != nil {
		return ownership.Registry{}, err
	}
	capabilityConsumed := false
	defer func() {
		if capability != nil && !capabilityConsumed {
			_ = capability.Close()
		}
	}()

	next, changed, err := current.ApplyProtection(owner, transition)
	if err != nil {
		return ownership.Registry{}, err
	}
	if err := ctx.Err(); err != nil {
		return ownership.Registry{}, err
	}
	if !changed {
		return next, nil
	}
	if err := store.validateProtectionSeparation(next); err != nil {
		return ownership.Registry{}, err
	}
	content, err := encode(next)
	if err != nil {
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
		return ownership.Registry{}, fmt.Errorf("prepare ownership protection transition: %w", err)
	}
	capabilityConsumed = capability != nil
	if err := storagecommit.CommitFile(ctx, request); err != nil {
		return ownership.Registry{}, fmt.Errorf("commit ownership protection transition: %w", err)
	}
	return next, nil
}

// This read authenticates the registry artifact, not its referenced claim paths.
// Protection recovery requalifies only its own frozen targets at the effect edge.
func (store Store) loadForProtection(ctx context.Context) (ownership.Registry, storagecommit.EntryIdentity, bool, rootedpath.CommitCapability, error) {
	if ctx == nil {
		return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf("ownership registry context is required")
	}
	if err := ctx.Err(); err != nil {
		return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, err
	}
	var (
		content    []byte
		mode       fs.FileMode
		identity   storagecommit.EntryIdentity
		capability rootedpath.CommitCapability
		err        error
	)
	if store.root != nil {
		capability, err = store.root.AcquireBounded(store.destination, store.maximumPhysicalDepth, store.physicalWorkBudget)
		if err != nil {
			return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, err
		}
		content, mode, identity, err = storagecommit.ReadRootedRegularFileUpTo(ctx, capability, maximumOwnershipRegistryBytes)
	} else {
		snapshot, readErr := storagecommit.ReadRegularFileSnapshotUpTo(ctx, store.path, maximumOwnershipRegistryBytes)
		err = readErr
		if err == nil {
			var valid bool
			identity, valid = snapshot.Identity().(storagecommit.EntryIdentity)
			if !valid {
				err = fmt.Errorf("ownership protection snapshot has unsupported entry identity %T", snapshot.Identity())
			} else {
				content, mode = snapshot.Content(), snapshot.Mode()
			}
		}
	}
	if errors.Is(err, fs.ErrNotExist) {
		return ownership.EmptyRegistry(), storagecommit.EntryIdentity{}, false, capability, nil
	}
	if err == nil && mode.Perm()&0o077 != 0 {
		err = fmt.Errorf("ownership registry %q permissions %04o expose authority metadata", store.path, mode.Perm())
	}
	var registry ownership.Registry
	if err == nil {
		registry, err = decode(content)
	}
	if err == nil {
		err = store.validateProtectionSeparation(registry)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		if capability != nil {
			err = errors.Join(err, capability.Close())
		}
		return ownership.Registry{}, storagecommit.EntryIdentity{}, false, nil, fmt.Errorf("read ownership protection: %w", err)
	}
	return registry, identity, true, capability, nil
}

func (store Store) validateProtectionSeparation(registry ownership.Registry) error {
	protections := registry.PendingProtections()
	if len(protections) == 0 {
		return nil
	}
	observations, err := store.newPathAuthorityObservationSession()
	if err != nil {
		return err
	}
	control, err := observations.observePersisted(store.path)
	if err != nil {
		return err
	}
	for _, protection := range protections {
		for _, document := range protection.Documents() {
			physical := document.PathAuthority()
			if physical.Contains(control) || control.Contains(physical) {
				return fmt.Errorf("protected document overlaps its ownership registry")
			}
		}
	}
	return nil
}
