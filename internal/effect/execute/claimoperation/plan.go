package claimoperation

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

type Plan struct {
	transitions []ownershipmutation.ClaimTransition
	intents     []ownership.ProvisionalAcquireIntent
}

func PlanManagedPaths(
	effects []effectpreparation.ManagedPathEffect,
	owner stateauthority.Authority,
	observations []observe.OwnershipObservation,
	operationID string,
) (Plan, error) {
	byOutput, err := ownershipObservationsByOutput(observations)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{
		transitions: make([]ownershipmutation.ClaimTransition, 0, len(effects)+1),
		intents:     make([]ownership.ProvisionalAcquireIntent, 0),
	}
	for _, effect := range effects {
		previous, hasPrevious := effect.PreviousState()
		if hasPrevious && (previous.Scope() != effect.Scope() || previous.Destination() != effect.Destination()) {
			if previous.Scope() == target.ScopeGlobal {
				old, present := byOutput[ownership.LogicalKey{Destination: previous.Destination(), Footprint: output.WholePathFootprint()}]
				if !present {
					return Plan{}, fmt.Errorf("previous global managed path %q has no ownership observation", previous.Destination())
				}
				oldClaim, err := exactActiveOwnershipClaim(old, owner)
				if err != nil {
					return Plan{}, fmt.Errorf("previous global managed path %q: %w", previous.Destination(), err)
				}
				release, err := ownershipmutation.NewReleaseTransition(oldClaim)
				if err != nil {
					return Plan{}, err
				}
				plan.transitions = append(plan.transitions, release)
			}
			if effect.Scope() == target.ScopeGlobal {
				current, present := byOutput[ownership.LogicalKey{Destination: effect.Destination(), Footprint: output.WholePathFootprint()}]
				if !present {
					return Plan{}, fmt.Errorf("new global managed path %q has no ownership observation", effect.Destination())
				}
				if err := appendOwnershipAcquire(&plan, current, owner, operationID); err != nil {
					return Plan{}, fmt.Errorf("new global managed path %q: %w", effect.Destination(), err)
				}
			}
			continue
		}
		if effect.Scope() != target.ScopeGlobal {
			continue
		}
		current, present := byOutput[ownership.LogicalKey{Destination: effect.Destination(), Footprint: output.WholePathFootprint()}]
		if !present {
			return Plan{}, fmt.Errorf("global managed path effect for %q has no ownership observation", effect.Destination())
		}
		if !hasPrevious {
			if err := appendOwnershipAcquire(&plan, current, owner, operationID); err != nil {
				return Plan{}, fmt.Errorf("new global managed path %q: %w", effect.Destination(), err)
			}
			continue
		}
		claim, err := exactActiveOwnershipClaim(current, owner)
		if err != nil {
			return Plan{}, fmt.Errorf("existing global managed path %q: %w", effect.Destination(), err)
		}
		var transition ownershipmutation.ClaimTransition
		if effect.Kind() == effectpreparation.ManagedPathEffectRemove {
			transition, err = ownershipmutation.NewReleaseTransition(claim)
		} else {
			transition, err = ownershipmutation.NewRetainTransition(claim)
		}
		if err != nil {
			return Plan{}, err
		}
		plan.transitions = append(plan.transitions, transition)
	}
	return plan, nil
}

func PlanAggregates(
	effects []effectpreparation.AggregateEffect,
	owner stateauthority.Authority,
	observations []observe.OwnershipObservation,
	operationID string,
) (Plan, error) {
	byOutput, err := ownershipObservationsByOutput(observations)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{}
	for _, effect := range effects {
		document := effect.DocumentAddress()
		if document.Scope() != target.ScopeGlobal {
			continue
		}
		for _, projection := range effect.ProjectionEffects() {
			if len(projection.Subjects()) == 0 {
				continue
			}
			address := projection.Contract().Address()
			key := ownership.LogicalKey{Destination: document.AggregateRoot(), Footprint: address.Footprint()}
			observation, present := byOutput[key]
			if !present {
				return Plan{}, fmt.Errorf("global aggregate projection for %q footprint %s has no ownership observation", key.Destination, key.ScopeLabel())
			}
			if len(projection.PreviousStates()) == 0 {
				if err := appendOwnershipAcquire(&plan, observation, owner, operationID); err != nil {
					return Plan{}, fmt.Errorf("new global aggregate projection for %q footprint %s: %w", key.Destination, key.ScopeLabel(), err)
				}
				continue
			}
			claim, err := exactActiveOwnershipClaim(observation, owner)
			if err != nil {
				return Plan{}, fmt.Errorf("existing global aggregate projection for %q footprint %s: %w", key.Destination, key.ScopeLabel(), err)
			}
			if projection.Kind() == effectpreparation.AggregateEffectRecord {
				continue
			}
			var transition ownershipmutation.ClaimTransition
			if projection.Kind() == effectpreparation.AggregateEffectRemove {
				transition, err = ownershipmutation.NewReleaseTransition(claim)
			} else {
				transition, err = ownershipmutation.NewRetainTransition(claim)
			}
			if err != nil {
				return Plan{}, err
			}
			plan.transitions = append(plan.transitions, transition)
		}
	}
	return plan, nil
}

func ownershipObservationsByOutput(observations []observe.OwnershipObservation) (map[ownership.LogicalKey]observe.OwnershipObservation, error) {
	byOutput := make(map[ownership.LogicalKey]observe.OwnershipObservation, len(observations))
	for index, observation := range observations {
		if err := observation.Validate(); err != nil {
			return nil, fmt.Errorf("execution ownership observation[%d]: %w", index, err)
		}
		key := ownership.LogicalKey{Destination: observation.Destination(), Footprint: observation.Footprint()}
		if _, exists := byOutput[key]; exists {
			return nil, fmt.Errorf("duplicate execution ownership observation for %q footprint %s", key.Destination, key.ScopeLabel())
		}
		byOutput[key] = observation
	}
	return byOutput, nil
}

func appendOwnershipAcquire(plan *Plan, observation observe.OwnershipObservation, owner stateauthority.Authority, operationID string) error {
	if plan == nil {
		return fmt.Errorf("ownership mutation plan is required")
	}
	if _, claimed := observation.Claim().Get(); claimed {
		return fmt.Errorf("new global output unexpectedly has an ownership claim")
	}
	if address, exact := observation.ExactAddress(); exact {
		transition, err := ownershipmutation.NewAcquireTransition(address, owner, operationID)
		if err != nil {
			return err
		}
		plan.transitions = append(plan.transitions, transition)
		return nil
	}
	provisional, present := observation.ProvisionalPath()
	if !present {
		return fmt.Errorf("new global output has neither exact nor provisional authority")
	}
	intent, err := ownership.NewProvisionalAcquireIntent(observation.Destination(), observation.Footprint(), provisional, owner, operationID)
	if err != nil {
		return err
	}
	plan.intents = append(plan.intents, intent)
	return nil
}

func exactActiveOwnershipClaim(observation observe.OwnershipObservation, owner stateauthority.Authority) (ownership.Claim, error) {
	address, exact := observation.ExactAddress()
	if !exact {
		return ownership.Claim{}, fmt.Errorf("lacks exact path authority")
	}
	claim, claimed := observation.Claim().Get()
	if !claimed || !claim.Address().Equal(address) || !claim.OwnedBy(owner) || claim.State() != ownership.ClaimActive {
		return ownership.Claim{}, fmt.Errorf("lacks its exact active ownership claim")
	}
	return claim, nil
}
