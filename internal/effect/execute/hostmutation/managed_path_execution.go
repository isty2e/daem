package hostmutation

import (
	"context"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/executionevent"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/operationplan"
)

func (host *Operation) ApplyManagedPathPhase(
	ctx context.Context,
	schedule ManagedPathSchedule,
	phase ManagedPathPhase,
	payloads payload.PayloadSet,
	progress ActionProgress,
	eventIndexOffset int,
	events executionevent.Emitter,
	gate applyprogram.VisibilityGate,
	execution *applyprogram.Execution,
	afterMutation func(context.Context, effectpreparation.ManagedPathEffect, ManagedPathPhase) error,
) error {
	for operation := range schedule.Operations(phase) {
		effect := operation.Effect()
		facts := executionevent.ManagedPathFacts(eventIndexOffset+operation.EffectIndex(), effect)
		if phase == ManagedPathPublishPhase || !operation.Relocates() {
			events.Emit(executionevent.ActionStarted, executionevent.StageAction, facts.Snapshot(), nil)
		}
		applyStep := func() error {
			state, err := host.ApplyManagedPathStep(ctx, operation, payloads)
			progress.record(operation.MutationIndex(), state)
			return err
		}
		var err error
		mutatesHost := effect.Kind() != effectpreparation.ManagedPathEffectRecord
		prefix := operation.ScheduleReference()
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
			events.Emit(executionevent.ActionFailed, executionevent.StageAction, facts.Snapshot(), err)
			return err
		}
		if !operation.Relocates() || phase == ManagedPathRetirePhase {
			events.Emit(executionevent.ActionDone, executionevent.StageAction, facts.Snapshot(), nil)
		}
	}
	return nil
}
