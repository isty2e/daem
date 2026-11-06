package execute

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func (authority *mutationAuthority) bindOwnershipRegistry(path string) error {
	if authority == nil {
		return fmt.Errorf("mutation authority is required")
	}
	if authority.physicalWorkBudget == nil {
		return fmt.Errorf("mutation physical work budget is required")
	}
	if authority.hasOwnershipRegistry {
		existingKey, existingErr := mutation.CanonicalDirectoryEntryKeyBounded(authority.ownershipRegistry.Path(), physicalwork.MaximumPhysicalPathDepth, authority.generalTraversalPhase)
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
	if err := authority.control.BindOwnership(root, destination, registry); err != nil {
		return err
	}
	authority.ownershipRegistry = registry
	authority.hasOwnershipRegistry = true
	return nil
}

func (authority *mutationAuthority) rootedOwnershipRegistry() (ownershipmutation.RegistryStore, error) {
	if authority == nil || !authority.hasOwnershipRegistry {
		return nil, fmt.Errorf("rooted ownership registry authority is unavailable")
	}
	return authority.ownershipRegistry, nil
}

func (authority *mutationAuthority) rootedOwnershipRegistryOption() ownershipmutation.RegistryReader {
	if authority == nil || !authority.hasOwnershipRegistry {
		return nil
	}
	return authority.ownershipRegistry
}
