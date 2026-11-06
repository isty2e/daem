package execute

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/hostmutation"
	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
)

func applyAggregateEffectsWithEvents(
	ctx context.Context,
	effects []effectpreparation.AggregateEffect,
	authority *mutationAuthority,
	codecs aggregate.CodecCatalog,
	eventIndexOffset int,
	events applyEventEmitter,
	gate applyprogram.VisibilityGate,
	execution *applyprogram.Execution,
	afterMutation func(context.Context, effectpreparation.AggregateEffect) error,
) (hostActionProgress, error) {
	journaledProjectionCount := 0
	for _, effect := range effects {
		journaledProjectionCount += effect.JournaledProjectionCount()
	}
	progress := newHostActionProgress(journaledProjectionCount)
	journalProjectionIndex := 0
	eventIndex := eventIndexOffset
	for effectIndex, effect := range effects {
		subjects := effect.SubjectEffects()
		for _, subject := range subjects {
			facts := aggregateEventFacts(eventIndex, subject.Kind(), subject.Subject(), subject.Contract())
			events.emit(EventActionStarted, EventStageAction, cloneActionEventFacts(facts), nil)
			eventIndex++
		}

		prefix := applyAggregateScheduleReference(effectIndex)
		var destination mutationnamespace.Destination
		observePreconditions := func() error {
			resolved, err := authority.destinations.Resolve(effect.Scope(), effect.Destination())
			if err != nil {
				return err
			}
			destination = resolved
			return authority.hostMutation().VerifyAggregatePreconditions(ctx, effect.OperationPreconditions())
		}
		hostProgress := hostmutation.EffectNotStarted
		var err error
		if effect.Kind() == effectpreparation.AggregateEffectRecord {
			err = execution.Observe(
				prefix+"/record",
				applyprogram.FailureGuardedRecovery,
				func() error {
					if observeErr := observePreconditions(); observeErr != nil {
						return observeErr
					}
					return authority.hostMutation().VerifyAggregateBefore(ctx, destination, effect, codecs)
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
					mutationErr := authority.hostMutation().CommitAggregate(ctx, destination, effect, codecs)
					hostProgress = hostmutation.EffectExpectedAfter
					if mutationErr != nil {
						hostProgress = hostmutation.ProgressAfterMutationError(mutationErr)
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
			facts := aggregateEventFacts(eventIndex-len(subjects)+index, subject.Kind(), subject.Subject(), subject.Contract())
			if err != nil {
				events.emit(EventActionFailed, EventStageAction, cloneActionEventFacts(facts), err)
			} else {
				events.emit(EventActionDone, EventStageAction, cloneActionEventFacts(facts), nil)
			}
		}
		if err != nil {
			return progress, fmt.Errorf("apply aggregate destination %q: %w", effect.Destination(), err)
		}
	}
	return progress, nil
}

func aggregateEventFacts(index int, kind effectpreparation.AggregateEffectKind, subject topology.SubjectID, contract aggregate.ProjectionContract) ActionEventFacts {
	return ActionEventFacts{
		Index: index, AggregateKind: kind, Subject: subject,
		Target: contract.Address().Document().Target(), Scope: contract.Address().Document().Scope(),
		Destination: contract.Address().Document().AggregateRoot(),
	}
}
