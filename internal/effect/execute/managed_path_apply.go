package execute

import (
	"context"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/hostmutation"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/operationplan"
)

func applyManagedPathPhaseWithEvents(
	ctx context.Context,
	schedule hostmutation.ManagedPathSchedule,
	phase hostmutation.ManagedPathPhase,
	payloads payload.PayloadSet,
	authority *mutationAuthority,
	progress hostActionProgress,
	eventIndexOffset int,
	events applyEventEmitter,
	gate applyprogram.VisibilityGate,
	execution *applyprogram.Execution,
	afterMutation func(context.Context, effectpreparation.ManagedPathEffect, hostmutation.ManagedPathPhase) error,
) error {
	for operation := range schedule.Operations(phase) {
		effect := operation.Effect()
		facts := managedPathEventFacts(eventIndexOffset+operation.EffectIndex(), effect)
		if phase == hostmutation.ManagedPathPublishPhase || !operation.Relocates() {
			events.emit(EventActionStarted, EventStageAction, cloneActionEventFacts(facts), nil)
		}
		applyStep := func() error {
			state, err := authority.hostMutation().ApplyManagedPathStep(ctx, operation, payloads)
			progress.record(operation.MutationIndex(), state)
			return err
		}
		var err error
		mutatesHost := effect.Kind() != effectpreparation.ManagedPathEffectRecord
		prefix := applyManagedPathScheduleReference(phase, operation.EffectIndex())
		if !mutatesHost {
			err = execution.Observe(prefix+"/record", applyprogram.FailureGuardedRecovery, applyStep)
		} else {
			operationGate := execution.VisibilityGate(gate, prefix, operationplan.EffectStepPersistence, applyprogram.FailureGuardedRecovery)
			if err = operationGate.ValidateBefore(ctx); err == nil {
				err = operationGate.ApplyEffect(applyStep)
			}
			if err == nil {
				err = operationGate.AcceptAfter(ctx)
			}
		}
		if err == nil && mutatesHost && afterMutation != nil {
			err = afterMutation(ctx, effect, phase)
		}
		if err != nil {
			events.emit(EventActionFailed, EventStageAction, cloneActionEventFacts(facts), err)
			return err
		}
		if !operation.Relocates() || phase == hostmutation.ManagedPathRetirePhase {
			events.emit(EventActionDone, EventStageAction, cloneActionEventFacts(facts), nil)
		}
	}
	return nil
}
