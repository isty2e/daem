package execute

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
)

type ownershipRegistryBinding struct {
	root        *rootedpath.CapturedRoot
	destination rootedpath.Destination
	store       ownershipmutation.RegistryStore
	controlled  bool
}

func (authority *mutationAuthority) registryReferences() []pathauthority.Reference {
	references := authority.destinations.References()
	if !authority.ownerReference.IsZero() {
		references = append(references, authority.ownerReference)
	}
	return references
}

func (authority *mutationAuthority) bindOwnershipRegistry(path string) error {
	if err := authority.bindOwnershipRegistryRead(path); err != nil {
		return err
	}
	binding := authority.ownershipRegistry
	if binding.controlled {
		return nil
	}
	if err := authority.control.BindOwnership(binding.root, binding.destination, binding.store); err != nil {
		return err
	}
	binding.controlled = true
	return nil
}

func (authority *mutationAuthority) bindOwnershipRegistryRead(path string) error {
	if authority == nil {
		return fmt.Errorf("mutation authority is required")
	}
	if authority.physicalWorkBudget == nil {
		return fmt.Errorf("mutation physical work budget is required")
	}
	if authority.ownershipRegistry != nil {
		existingKey, existingErr := mutation.CanonicalDirectoryEntryKeyBounded(authority.ownershipRegistry.store.Path(), physicalwork.MaximumPhysicalPathDepth, authority.generalTraversalPhase)
		requestedKey, requestedErr := mutation.CanonicalDirectoryEntryKeyBounded(path, physicalwork.MaximumPhysicalPathDepth, authority.generalTraversalPhase)
		if existingErr != nil || requestedErr != nil {
			return errors.Join(existingErr, requestedErr)
		}
		if existingKey != requestedKey {
			return fmt.Errorf("ownership registry has conflicting physical bindings")
		}
		return nil
	}
	if authority.ownershipRegistryBinder == nil {
		return fmt.Errorf("rooted ownership registry binder is required")
	}
	root, destination, err := rootedpath.CaptureDestinationBounded(path, physicalwork.MaximumPhysicalPathDepth, authority.generalTraversalPhase)
	if err != nil {
		return fmt.Errorf("capture ownership registry authority: %w", err)
	}
	root, err = authority.destinations.RetainRoot(root, destination)
	if err != nil {
		return err
	}
	registry, err := authority.ownershipRegistryBinder(root, destination, physicalwork.MaximumPhysicalPathDepth, authority.generalTraversalPhase)
	if err != nil {
		return err
	}
	if registry == nil {
		return fmt.Errorf("rooted ownership registry binder returned a nil store")
	}
	authority.ownershipRegistry = &ownershipRegistryBinding{
		root: root, destination: destination, store: registry,
	}
	return nil
}

func (authority *mutationAuthority) rootedOwnershipRegistry() (ownershipmutation.RegistryStore, error) {
	if authority == nil || authority.ownershipRegistry == nil || !authority.ownershipRegistry.controlled {
		return nil, fmt.Errorf("rooted ownership registry authority is unavailable")
	}
	return authority.ownershipRegistry.store, nil
}

func (authority *mutationAuthority) rootedOwnershipRegistryOption() ownershipmutation.RegistryReader {
	if authority == nil || authority.ownershipRegistry == nil {
		return nil
	}
	return authority.ownershipRegistry.store
}

type recoveryRegistryReader struct {
	authority *mutationAuthority
	path      string
}

func (authority *mutationAuthority) recoveryOwnershipRegistryOption(path string) ownershipmutation.RegistryReader {
	return recoveryRegistryReader{authority: authority, path: path}
}

func (reader recoveryRegistryReader) Load(ctx context.Context) (ownership.Registry, error) {
	if err := reader.authority.bindOwnershipRegistryRead(reader.path); err != nil {
		return ownership.Registry{}, err
	}
	return reader.authority.ownershipRegistry.store.Load(ctx)
}

func (reader recoveryRegistryReader) LoadForClaimRemovals(ctx context.Context, expected []ownership.Claim, depth int, budget rootedpath.PhysicalTraversalBudget) (ownership.Registry, error) {
	if err := reader.authority.bindOwnershipRegistryRead(reader.path); err != nil {
		return ownership.Registry{}, err
	}
	return reader.authority.ownershipRegistry.store.LoadForClaimRemovals(ctx, expected, depth, budget)
}
