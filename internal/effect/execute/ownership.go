package execute

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

func claimTransitionsForManagedPathEffects(
	effects []ManagedPathEffect,
	owner stateauthority.Authority,
	observations []observe.OwnershipObservation,
	operationID string,
) ([]ownershipmutation.ClaimTransition, error) {
	byOutput := make(map[ownershipOutputKey]observe.OwnershipObservation, len(observations))
	for _, observation := range observations {
		key := ownershipOutputKey{destination: observation.Destination, contentPath: observation.ContentPath}
		if _, exists := byOutput[key]; exists {
			return nil, fmt.Errorf("duplicate execution ownership observation for %q content path %q", observation.Destination, observation.ContentPath)
		}
		byOutput[key] = observation
	}
	transitions := make([]ownershipmutation.ClaimTransition, 0, len(effects)+1)
	for _, effect := range effects {
		previous, hasPrevious := effect.PreviousState()
		if hasPrevious && (previous.Scope() != effect.Scope() || previous.Destination() != effect.Destination()) {
			if previous.Scope() == target.ScopeGlobal {
				old, present := byOutput[ownershipOutputKey{destination: previous.Destination()}]
				if !present {
					return nil, fmt.Errorf("previous global managed path %q has no ownership observation", previous.Destination())
				}
				oldClaim, claimed := old.Claim.Get()
				if !claimed || !oldClaim.Address().Equal(old.Address) || !oldClaim.OwnedBy(owner) || oldClaim.State() != ownership.ClaimActive {
					return nil, fmt.Errorf("previous global managed path %q lacks its exact active ownership claim", previous.Destination())
				}
				release, err := ownershipmutation.NewReleaseTransition(oldClaim)
				if err != nil {
					return nil, err
				}
				transitions = append(transitions, release)
			}
			if effect.Scope() == target.ScopeGlobal {
				current, present := byOutput[ownershipOutputKey{destination: effect.Destination()}]
				if !present {
					return nil, fmt.Errorf("new global managed path %q has no ownership observation", effect.Destination())
				}
				if _, claimed := current.Claim.Get(); claimed {
					return nil, fmt.Errorf("new global managed path %q unexpectedly has an ownership claim", effect.Destination())
				}
				acquire, err := ownershipmutation.NewAcquireTransition(current.Address, owner, operationID)
				if err != nil {
					return nil, err
				}
				transitions = append(transitions, acquire)
			}
			continue
		}
		if effect.Scope() != target.ScopeGlobal {
			continue
		}
		current, present := byOutput[ownershipOutputKey{destination: effect.Destination()}]
		if !present {
			return nil, fmt.Errorf("global managed path effect for %q has no ownership observation", effect.Destination())
		}

		claim, claimed := current.Claim.Get()
		if !hasPrevious {
			if claimed {
				return nil, fmt.Errorf("new global managed path %q unexpectedly has an ownership claim", effect.Destination())
			}
			acquire, err := ownershipmutation.NewAcquireTransition(current.Address, owner, operationID)
			if err != nil {
				return nil, err
			}
			transitions = append(transitions, acquire)
			continue
		}
		if !claimed || !claim.Address().Equal(current.Address) || !claim.OwnedBy(owner) || claim.State() != ownership.ClaimActive {
			return nil, fmt.Errorf("existing global managed path %q lacks its exact active ownership claim", effect.Destination())
		}
		var transition ownershipmutation.ClaimTransition
		var err error
		if effect.Kind() == ManagedPathEffectRemove {
			transition, err = ownershipmutation.NewReleaseTransition(claim)
		} else {
			transition, err = ownershipmutation.NewRetainTransition(claim)
		}
		if err != nil {
			return nil, err
		}
		transitions = append(transitions, transition)
	}
	return transitions, nil
}

func claimTransitionsForAggregateEffects(
	effects []AggregateEffect,
	owner stateauthority.Authority,
	observations []observe.OwnershipObservation,
	operationID string,
) ([]ownershipmutation.ClaimTransition, error) {
	byOutput := make(map[ownershipOutputKey]observe.OwnershipObservation, len(observations))
	for _, observation := range observations {
		key := ownershipOutputKey{destination: observation.Destination, contentPath: observation.ContentPath}
		if _, exists := byOutput[key]; exists {
			return nil, fmt.Errorf(
				"duplicate execution ownership observation for %q content path %q",
				observation.Destination,
				observation.ContentPath,
			)
		}
		byOutput[key] = observation
	}

	transitions := make([]ownershipmutation.ClaimTransition, 0)
	for _, effect := range effects {
		document := effect.DocumentAddress()
		if document.Scope() != target.ScopeGlobal {
			continue
		}
		for _, projection := range effect.projections {
			if len(projection.subjects) == 0 {
				continue
			}
			address := projection.contract.Address()
			key := ownershipOutputKey{
				destination: document.AggregateRoot(),
				contentPath: output.ContentPath(address.ContentPath()),
			}
			observation, present := byOutput[key]
			if !present {
				return nil, fmt.Errorf(
					"global aggregate projection for %q content path %q has no ownership observation",
					key.destination,
					key.contentPath,
				)
			}

			claim, claimed := observation.Claim.Get()
			if len(projection.previous) == 0 {
				if claimed {
					return nil, fmt.Errorf(
						"new global aggregate projection for %q content path %q unexpectedly has an ownership claim",
						key.destination,
						key.contentPath,
					)
				}
				acquire, err := ownershipmutation.NewAcquireTransition(observation.Address, owner, operationID)
				if err != nil {
					return nil, err
				}
				transitions = append(transitions, acquire)
				continue
			}
			if !claimed || !claim.Address().Equal(observation.Address) ||
				!claim.OwnedBy(owner) || claim.State() != ownership.ClaimActive {
				return nil, fmt.Errorf(
					"existing global aggregate projection for %q content path %q lacks its exact active ownership claim",
					key.destination,
					key.contentPath,
				)
			}
			if projection.kind == AggregateEffectRecord {
				continue
			}

			var transition ownershipmutation.ClaimTransition
			var err error
			if projection.kind == AggregateEffectRemove {
				transition, err = ownershipmutation.NewReleaseTransition(claim)
			} else {
				transition, err = ownershipmutation.NewRetainTransition(claim)
			}
			if err != nil {
				return nil, err
			}
			transitions = append(transitions, transition)
		}
	}
	return transitions, nil
}

type ownershipOutputKey struct {
	destination output.Destination
	contentPath output.ContentPath
}

func prepareClaimTransitions(
	ctx context.Context,
	registryStore ownershipmutation.RegistryStore,
	transitions []ownershipmutation.ClaimTransition,
) error {
	if len(transitions) == 0 {
		return nil
	}
	for index, transition := range transitions {
		if err := convergeClaim(ctx, registryStore, transition.Address(), transition.Before(), transition.Prepared()); err != nil {
			rollbackErr := rollbackClaimTransitions(context.WithoutCancel(ctx), registryStore, transitions[:index+1])
			if rollbackErr != nil {
				return fmt.Errorf("prepare ownership claim: %w; ownership rollback failed: %v", err, rollbackErr)
			}
			return fmt.Errorf("prepare ownership claim: %w", err)
		}
	}
	return nil
}

func finalizeClaimTransitions(
	ctx context.Context,
	registryStore ownershipmutation.RegistryStore,
	transitions []ownershipmutation.ClaimTransition,
) error {
	if len(transitions) == 0 {
		return nil
	}
	for _, transition := range transitions {
		if err := convergeClaim(ctx, registryStore, transition.Address(), transition.Prepared(), transition.After()); err != nil {
			return fmt.Errorf("finalize ownership claim: %w", err)
		}
	}
	return nil
}

func rollbackClaimsToBefore(
	ctx context.Context,
	registryStore ownershipmutation.RegistryStore,
	transitions []ownershipmutation.ClaimTransition,
) error {
	if len(transitions) == 0 {
		return nil
	}
	return rollbackClaimTransitions(ctx, registryStore, transitions)
}

func rollbackClaimTransitions(
	ctx context.Context,
	registryStore ownershipmutation.RegistryStore,
	transitions []ownershipmutation.ClaimTransition,
) error {
	for index := len(transitions) - 1; index >= 0; index-- {
		transition := transitions[index]
		if err := convergeClaim(ctx, registryStore, transition.Address(), transition.Prepared(), transition.Before()); err != nil {
			return fmt.Errorf("rollback ownership claim: %w", err)
		}
	}
	return nil
}

func convergeClaim(
	ctx context.Context,
	registryStore ownershipmutation.RegistryStore,
	address ownership.ManagedAddress,
	from ownership.ClaimValue,
	to ownership.ClaimValue,
) error {
	registry, err := registryStore.Load(ctx)
	if err != nil {
		return err
	}
	actual := ownership.NoClaim()
	if claim, present := registry.Conflict(address); present {
		actual, _ = ownership.PresentClaim(claim)
	}
	if actual.Equal(to) {
		return nil
	}
	if !actual.Equal(from) {
		return &ownership.StaleClaimError{Address: address, Expected: from, Actual: actual}
	}
	_, err = registryStore.Apply(ctx, address, from, to)
	return err
}

func (authority *mutationAuthority) bindOwnershipRegistry(path string) error {
	if authority == nil {
		return fmt.Errorf("mutation authority is required")
	}
	if authority.hasOwnershipRegistry {
		existingKey, existingErr := mutation.CanonicalDirectoryEntryKey(authority.ownershipRegistry.Path())
		requestedKey, requestedErr := mutation.CanonicalDirectoryEntryKey(path)
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
	root, destination, err := rootedpath.CaptureDestination(path)
	if err != nil {
		return fmt.Errorf("capture ownership registry authority: %w", err)
	}
	root, err = authority.retainGlobalRoot(root, destination)
	if err != nil {
		return err
	}
	registry, err := authority.ownershipRegistryBinder(root, destination)
	if err != nil {
		return err
	}
	if registry == nil {
		return fmt.Errorf("rooted ownership registry binder returned a nil store")
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
	return authority.ownershipRegistry.Load
}
