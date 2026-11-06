package execute

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/claimoperation"
	"github.com/isty2e/daem/internal/effect/execute/hostmutation"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
)

type applyStateTransition struct {
	nextState                durable.Snapshot
	retiredProjectClaimCount int
	adoptedProjectClaimCount int
	changed                  bool
}

func deriveApplyStateTransition(input ApplyInput) (applyStateTransition, error) {
	if err := rejectUnsupportedNativeInput(input); err != nil {
		return applyStateTransition{}, err
	}

	nextState, err := effectpreparation.AfterManagedPaths(input.CurrentState, input.ManagedPathEffects)
	if err != nil {
		return applyStateTransition{}, err
	}
	nextState, err = effectpreparation.AfterAggregates(nextState, input.AggregateEffects)
	if err != nil {
		return applyStateTransition{}, err
	}
	nextState, globalCarrierStateChanged, err := nextState.WithConvergedGlobalCarrierClaims(
		input.GlobalCarrierClaims,
	)
	if err != nil {
		return applyStateTransition{}, err
	}
	nextState, retiredProjectClaimCount, err := effectpreparation.AfterRetiredProjectCarrierClaims(
		nextState,
		input.RetiredProjectCarrierClaims,
	)
	if err != nil {
		return applyStateTransition{}, err
	}
	completion, err := effectpreparation.CompleteProjectCarrierInstalls(
		nextState,
		input.ConfirmedRelationActions,
	)
	if err != nil {
		return applyStateTransition{}, err
	}
	nextState = completion.AfterState()
	projectClaimCountBeforeAdoption := len(nextState.ManagedCarrierClaims())
	nextState, adoptionStateChanged, err := nextState.WithAdoptedCarrierClaims(
		input.AdoptedProjectCarrierClaims,
	)
	if err != nil {
		return applyStateTransition{}, err
	}
	adoptedProjectClaimCount := len(nextState.ManagedCarrierClaims()) - projectClaimCountBeforeAdoption
	changed := len(input.ManagedPathEffects) != 0 ||
		len(input.AggregateEffects) != 0 ||
		globalCarrierStateChanged ||
		retiredProjectClaimCount != 0 ||
		completion.Changed() ||
		adoptionStateChanged
	return applyStateTransition{
		nextState:                nextState,
		retiredProjectClaimCount: retiredProjectClaimCount,
		adoptedProjectClaimCount: adoptedProjectClaimCount,
		changed:                  changed,
	}, nil
}

// MaximumForwardEffectValidationCount returns the exact maximum number of
// forward visibility-gate validations reachable from one ApplyInput. Failure
// and compensation paths use separate authority.
func MaximumForwardEffectValidationCount(input ApplyInput) (int, error) {
	transition, err := deriveApplyStateTransition(input)
	if err != nil {
		return 0, err
	}
	if !transition.changed {
		return 0, nil
	}
	managedSchedule, err := hostmutation.PlanManagedPaths(input.ManagedPathEffects)
	if err != nil {
		return 0, err
	}
	managedMutations := managedSchedule.WriteCount()
	aggregateMutations := 0
	for _, effect := range input.AggregateEffects {
		if effect.Kind() != effectpreparation.AggregateEffectRecord {
			aggregateMutations++
		}
	}
	operationID := journalpublication.PlanningOperationID()
	managedOwnership, err := claimoperation.PlanManagedPaths(
		input.ManagedPathEffects,
		input.Owner,
		input.Ownership,
		operationID,
	)
	if err != nil {
		return 0, fmt.Errorf("derive managed path ownership validation plan: %w", err)
	}
	aggregateOwnership, err := claimoperation.PlanAggregates(
		input.AggregateEffects,
		input.Owner,
		input.Ownership,
		operationID,
	)
	if err != nil {
		return 0, fmt.Errorf("derive managed aggregate ownership validation plan: %w", err)
	}
	ownershipState := claimoperation.NewState(managedOwnership, aggregateOwnership)
	count := 3 // journal publication, statefile publication, and journal retirement
	count, err = applyprogram.AddValidationCount(count, managedMutations)
	if err != nil {
		return 0, err
	}
	count, err = applyprogram.AddValidationCount(count, aggregateMutations)
	if err != nil {
		return 0, err
	}
	if ownershipState.HasTransitions() {
		count, err = applyprogram.AddValidationCount(count, 2) // preparation and finalization
		if err != nil {
			return 0, err
		}
	} else if ownershipState.IntentCount() != 0 {
		// A successful provisional promotion appends an exact transition after
		// the initial preparation phase. The resulting set still finalizes once.
		count, err = applyprogram.AddValidationCount(count, 1)
		if err != nil {
			return 0, err
		}
	}
	provisionalValidations, err := applyprogram.MultiplyValidationCount(ownershipState.IntentCount(), 2)
	if err != nil {
		return 0, err
	}
	return applyprogram.AddValidationCount(count, provisionalValidations)
}
