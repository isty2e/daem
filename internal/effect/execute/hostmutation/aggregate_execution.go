package hostmutation

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/executionevent"
	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func (host *Operation) ApplyAggregateEffects(
	ctx context.Context,
	effects []effectpreparation.AggregateEffect,
	codecs aggregate.CodecCatalog,
	eventIndexOffset int,
	events executionevent.Emitter,
	gate applyprogram.VisibilityGate,
	execution *applyprogram.Execution,
	afterMutation func(context.Context, effectpreparation.AggregateEffect) error,
) (ActionProgress, error) {
	journaledProjectionCount := 0
	for _, effect := range effects {
		journaledProjectionCount += effect.JournaledProjectionCount()
	}
	progress := NewActionProgress(journaledProjectionCount)
	journalProjectionIndex := 0
	eventIndex := eventIndexOffset
	for effectIndex, effect := range effects {
		subjects := effect.SubjectEffects()
		for _, subject := range subjects {
			facts := executionevent.AggregateFacts(eventIndex, subject.Kind(), subject.Subject(), subject.Contract())
			events.Emit(executionevent.ActionStarted, executionevent.StageAction, facts.Snapshot(), nil)
			eventIndex++
		}

		prefix := AggregateScheduleReference(effectIndex)
		var destination mutationnamespace.Destination
		observePreconditions := func() error {
			resolved, err := host.destinations.Resolve(effect.Scope(), effect.Destination())
			if err != nil {
				return err
			}
			destination = resolved
			return host.VerifyAggregatePreconditions(ctx, effect.OperationPreconditions())
		}
		hostProgress := EffectNotStarted
		var err error
		if effect.Kind() == effectpreparation.AggregateEffectRecord {
			err = execution.Observe(
				prefix+"/record",
				applyprogram.FailureGuardedRecovery,
				func() error {
					if observeErr := observePreconditions(); observeErr != nil {
						return observeErr
					}
					return host.VerifyAggregateBefore(ctx, destination, effect, codecs)
				},
			)
		} else {
			err = execution.Observe(
				prefix+"/preconditions",
				applyprogram.FailureGuardedRecovery,
				observePreconditions,
			)
			operationGate := execution.VisibilityGate(
				gate,
				prefix,
				operationplan.EffectStepPersistence,
				applyprogram.FailureGuardedRecovery,
			)
			if err == nil {
				err = operationGate.ValidateBefore(ctx)
			}
			if err == nil {
				err = operationGate.ApplyEffect(func() error {
					mutationErr := host.CommitAggregate(ctx, destination, effect, codecs)
					hostProgress = EffectExpectedAfter
					if mutationErr != nil {
						hostProgress = ProgressAfterMutationError(mutationErr)
					}
					return mutationErr
				})
			}
			if err == nil {
				err = operationGate.AcceptAfter(ctx)
			}
			if err == nil && afterMutation != nil {
				err = afterMutation(ctx, effect)
			}
		}
		for _, projection := range effect.ProjectionEffects() {
			if !projection.IsJournaled() {
				continue
			}
			if projection.MutatesHost() {
				progress.record(journalProjectionIndex, hostProgress)
			}
			journalProjectionIndex++
		}
		for index, subject := range subjects {
			facts := executionevent.AggregateFacts(eventIndex-len(subjects)+index, subject.Kind(), subject.Subject(), subject.Contract())
			if err != nil {
				events.Emit(executionevent.ActionFailed, executionevent.StageAction, facts.Snapshot(), err)
			} else {
				events.Emit(executionevent.ActionDone, executionevent.StageAction, facts.Snapshot(), nil)
			}
		}
		if err != nil {
			return progress, fmt.Errorf("apply aggregate destination %q: %w", effect.Destination(), err)
		}
	}
	return progress, nil
}

func AggregateScheduleReference(index int) string {
	return fmt.Sprintf("apply/aggregate/%d", index)
}
