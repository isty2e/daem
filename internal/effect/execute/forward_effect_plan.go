package execute

import (
	"fmt"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal"
)

type applyStateTransition struct {
	nextState                durable.Snapshot
	retiredProjectClaimCount int
	adoptedProjectClaimCount int
	changed                  bool
}

func deriveApplyStateTransition(input ApplyInput) (applyStateTransition, error) {
	nextState, err := snapshotAfterManagedPathEffects(input.CurrentState, input.ManagedPathEffects)
	if err != nil {
		return applyStateTransition{}, err
	}
	nextState, err = snapshotAfterAggregateEffects(nextState, input.AggregateEffects)
	if err != nil {
		return applyStateTransition{}, err
	}
	nextState, globalCarrierStateChanged, err := nextState.WithConvergedGlobalCarrierClaims(
		input.GlobalCarrierClaims,
	)
	if err != nil {
		return applyStateTransition{}, err
	}
	nextState, retiredProjectClaimCount, err := snapshotAfterRetiredProjectCarrierClaims(
		nextState,
		input.RetiredProjectCarrierClaims,
	)
	if err != nil {
		return applyStateTransition{}, err
	}
	promotedClaims, err := promotedProjectCarrierClaims(
		nextState,
		input.ConfirmedRelationActions,
	)
	if err != nil {
		return applyStateTransition{}, err
	}
	nextState, relationStateChanged, err := nextState.WithPromotedCarrierClaims(promotedClaims)
	if err != nil {
		return applyStateTransition{}, err
	}
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
		relationStateChanged ||
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
	managedSchedule, err := newManagedPathExecutionSchedule(input.ManagedPathEffects)
	if err != nil {
		return 0, err
	}
	managedMutations := managedPathValidationCount(managedSchedule)
	aggregateMutations := 0
	for _, effect := range input.AggregateEffects {
		if effect.Kind() != AggregateEffectRecord {
			aggregateMutations++
		}
	}
	operationID := journal.OperationID(time.Unix(0, 0).UTC())
	managedOwnership, err := ownershipPlanForManagedPathEffects(
		input.ManagedPathEffects,
		input.Owner,
		input.Ownership,
		operationID,
	)
	if err != nil {
		return 0, fmt.Errorf("derive managed path ownership validation plan: %w", err)
	}
	aggregateOwnership, err := ownershipPlanForAggregateEffects(
		input.AggregateEffects,
		input.Owner,
		input.Ownership,
		operationID,
	)
	if err != nil {
		return 0, fmt.Errorf("derive managed aggregate ownership validation plan: %w", err)
	}
	ownershipState := newOwnershipMutationState(managedOwnership, aggregateOwnership)
	count := 3 // journal publication, statefile publication, and journal retirement
	count, err = addForwardValidationCount(count, managedMutations)
	if err != nil {
		return 0, err
	}
	count, err = addForwardValidationCount(count, aggregateMutations)
	if err != nil {
		return 0, err
	}
	if len(ownershipState.transitions) != 0 {
		count, err = addForwardValidationCount(count, 2) // preparation and finalization
		if err != nil {
			return 0, err
		}
	} else if len(ownershipState.provisional) != 0 {
		// A successful provisional promotion appends an exact transition after
		// the initial preparation phase. The resulting set still finalizes once.
		count, err = addForwardValidationCount(count, 1)
		if err != nil {
			return 0, err
		}
	}
	provisionalValidations, err := multiplyForwardValidationCount(len(ownershipState.provisional), 2)
	if err != nil {
		return 0, err
	}
	return addForwardValidationCount(count, provisionalValidations)
}

func managedPathValidationCount(schedule managedPathExecutionSchedule) int {
	count := 0
	for _, operations := range [][]managedPathPhaseOperation{schedule.publish, schedule.retire} {
		for _, operation := range operations {
			if schedule.effects[operation.effectIndex].Kind() != ManagedPathEffectRecord {
				count++
			}
		}
	}
	return count
}

func addForwardValidationCount(left int, right int) (int, error) {
	if left < 0 || right < 0 || left > int(^uint(0)>>1)-right {
		return 0, fmt.Errorf("forward effect validation count overflows")
	}
	return left + right, nil
}

func multiplyForwardValidationCount(value int, multiplier int) (int, error) {
	if value < 0 || multiplier < 0 || value != 0 && multiplier > int(^uint(0)>>1)/value {
		return 0, fmt.Errorf("forward effect validation count overflows")
	}
	return value * multiplier, nil
}
