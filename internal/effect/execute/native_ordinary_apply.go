package execute

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute/claimoperation"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/target"
)

type nativeOrdinaryApply struct {
	managedMutations   []journalrecord.ManagedPathMutation
	aggregateMutations []journalrecord.ManagedAggregateMutation
	demands            recovery.RemovalDemandSet
	managedSchedule    managedPathExecutionSchedule
	ownership          claimoperation.State
}

func nativeOrdinaryApplyFor(input NativeApplyInput, work nativeApplyWork, operationID string) (nativeOrdinaryApply, error) {
	managed, err := effectpreparation.ManagedPathJournalMutations(work.managed)
	if err != nil {
		return nativeOrdinaryApply{}, err
	}
	aggregates, err := effectpreparation.AggregateJournalMutations(work.ordinaryAggregates)
	if err != nil {
		return nativeOrdinaryApply{}, err
	}
	demands, err := removalDemandSetForExecution(work.managed, work.ordinaryAggregates, input.ManagedPathEvidence)
	if err != nil {
		return nativeOrdinaryApply{}, err
	}
	schedule, err := newManagedPathExecutionSchedule(work.managed)
	if err != nil {
		return nativeOrdinaryApply{}, err
	}
	managedOwnership, err := claimoperation.PlanManagedPaths(work.managed, input.Owner, input.Ownership, operationID)
	if err != nil {
		return nativeOrdinaryApply{}, err
	}
	aggregateOwnership, err := claimoperation.PlanAggregates(work.ordinaryAggregates, input.Owner, input.Ownership, operationID)
	if err != nil {
		return nativeOrdinaryApply{}, err
	}
	return nativeOrdinaryApply{
		managedMutations: managed, aggregateMutations: aggregates, demands: demands,
		managedSchedule: schedule, ownership: claimoperation.NewState(managedOwnership, aggregateOwnership),
	}, nil
}

func bindNativeOrdinaryApply(ctx context.Context, input NativeApplyInput, work nativeApplyWork, authority *mutationAuthority) error {
	for _, effect := range work.managed {
		if err := authority.bindPhysicalAuthority(effect.Scope(), effect.Destination(), effect.ConsumerTargets()); err != nil {
			return err
		}
		if previous, exists := effect.PreviousState(); exists && (previous.Scope() != effect.Scope() || previous.Destination() != effect.Destination()) {
			if err := authority.bindPhysicalAuthority(previous.Scope(), previous.Destination(), previous.ConsumerTargets()); err != nil {
				return err
			}
		}
		if effect.Kind() == effectpreparation.ManagedPathEffectCreate || effect.Kind() == effectpreparation.ManagedPathEffectReplace {
			if _, err := effectpreparation.ManagedPathPayload(effect, input.Payloads); err != nil {
				return fmt.Errorf("prepare joint managed payload: %w", err)
			}
		}
	}
	for _, effect := range work.ordinaryAggregates {
		if err := authority.bindPhysicalAuthority(effect.Scope(), effect.Destination(), []target.Target{effect.Target()}); err != nil {
			return err
		}
	}
	return ctx.Err()
}
