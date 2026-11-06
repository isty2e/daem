package execute

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/hostmutation"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"

	"github.com/isty2e/daem/internal/assurance/durable"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/realization"
)

type managedPathPhase uint8

const (
	managedPathPublishPhase managedPathPhase = iota + 1
	managedPathRetirePhase
)

type managedPathPhaseOperation struct {
	effectIndex   int
	mutationIndex int
	relocation    bool
}

type managedPathExecutionSchedule struct {
	effects       []effectpreparation.ManagedPathEffect
	publish       []managedPathPhaseOperation
	retire        []managedPathPhaseOperation
	mutationCount int
}

func newManagedPathExecutionSchedule(effects []effectpreparation.ManagedPathEffect) (managedPathExecutionSchedule, error) {
	mutations, err := effectpreparation.ManagedPathJournalMutations(effects)
	if err != nil {
		return managedPathExecutionSchedule{}, err
	}
	schedule := managedPathExecutionSchedule{effects: append([]effectpreparation.ManagedPathEffect(nil), effects...), mutationCount: len(mutations)}
	mutationIndex := 0
	for effectIndex, effect := range effects {
		operation := managedPathPhaseOperation{effectIndex: effectIndex, mutationIndex: mutationIndex}
		if effect.Kind() == effectpreparation.ManagedPathEffectReplace {
			previous, _ := effect.PreviousState()
			if previous.Scope() != effect.Scope() || previous.Destination() != effect.Destination() {
				operation.relocation = true
				schedule.publish = append(schedule.publish, operation)
				schedule.retire = append(schedule.retire, managedPathPhaseOperation{
					effectIndex: effectIndex, mutationIndex: mutationIndex + 1, relocation: true,
				})
				mutationIndex += 2
				continue
			}
		}
		if effect.Kind() == effectpreparation.ManagedPathEffectRemove {
			schedule.retire = append(schedule.retire, operation)
		} else {
			schedule.publish = append(schedule.publish, operation)
		}
		mutationIndex++
	}
	if mutationIndex != len(mutations) {
		return managedPathExecutionSchedule{}, fmt.Errorf("managed path effect expansion produced %d operations, want %d", mutationIndex, len(mutations))
	}
	return schedule, nil
}

func applyManagedPathPhaseWithEvents(
	ctx context.Context,
	schedule managedPathExecutionSchedule,
	phase managedPathPhase,
	payloads payload.PayloadSet,
	authority *mutationAuthority,
	progress hostActionProgress,
	eventIndexOffset int,
	events applyEventEmitter,
	gate applyprogram.VisibilityGate,
	execution *applyprogram.Execution,
	afterMutation func(context.Context, effectpreparation.ManagedPathEffect, managedPathPhase) error,
) error {
	operations := schedule.publish
	if phase == managedPathRetirePhase {
		operations = schedule.retire
	}
	for _, operation := range operations {
		effect := schedule.effects[operation.effectIndex]
		facts := managedPathEventFacts(eventIndexOffset+operation.effectIndex, effect)
		if phase == managedPathPublishPhase || !operation.relocation {
			events.emit(EventActionStarted, EventStageAction, cloneActionEventFacts(facts), nil)
		}
		var err error
		mutatesHost := effect.Kind() != effectpreparation.ManagedPathEffectRecord
		prefix := applyManagedPathScheduleReference(phase, operation.effectIndex)
		if effect.Kind() == effectpreparation.ManagedPathEffectRecord {
			err = execution.Observe(
				prefix+"/record",
				applyprogram.FailureGuardedRecovery,
				func() error {
					destination, resolveErr := authority.destinations.Resolve(effect.Scope(), effect.Destination())
					if resolveErr != nil {
						return resolveErr
					}
					if verifyErr := authority.hostMutation().VerifyPath(
						ctx, destination, true, effect.DesiredHash(), effect.ContentKind(), effect.LiveFileMode(),
					); verifyErr != nil {
						return verifyErr
					}
					progress.record(operation.mutationIndex, hostEffectExpectedAfter)
					return nil
				},
			)
		} else {
			operationGate := execution.VisibilityGate(
				gate,
				prefix,
				operationplan.EffectStepPersistence,
				applyprogram.FailureGuardedRecovery,
			)
			if err = operationGate.ValidateBefore(ctx); err == nil {
				err = operationGate.ApplyEffect(func() error {
					switch {
					case operation.relocation && phase == managedPathPublishPhase:
						return applyManagedPathRelocationPublish(
							ctx, effect, payloads, authority, progress, operation.mutationIndex,
						)
					case operation.relocation && phase == managedPathRetirePhase:
						previous, _ := effect.PreviousState()
						return applyManagedPathRelocationRetire(
							ctx, previous, authority, progress, operation.mutationIndex,
						)
					default:
						return applyManagedPathSingleMutation(
							ctx, effect, payloads, authority, progress, operation.mutationIndex,
						)
					}
				})
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
		if !operation.relocation || phase == managedPathRetirePhase {
			events.emit(EventActionDone, EventStageAction, cloneActionEventFacts(facts), nil)
		}
	}
	return nil
}

func applyManagedPathSingleMutation(
	ctx context.Context,
	effect effectpreparation.ManagedPathEffect,
	payloads payload.PayloadSet,
	authority *mutationAuthority,
	progress hostActionProgress,
	mutationIndex int,
) error {
	destination, err := authority.destinations.Resolve(effect.Scope(), effect.Destination())
	if err != nil {
		return err
	}
	expectedExists := effect.Kind() != effectpreparation.ManagedPathEffectCreate
	expectedHash := effect.LiveHash()
	precondition, err := authority.hostMutation().CapturePath(
		ctx,
		destination,
		expectedExists,
		expectedHash,
		effect.ContentKind(),
		nil,
	)
	if err != nil {
		return err
	}
	defer precondition.Close()

	if effect.Kind() == effectpreparation.ManagedPathEffectRemove {
		if err := precondition.Remove(ctx); err != nil {
			progress.record(mutationIndex, progressAfterMutationError(err))
			return fmt.Errorf("remove managed destination %q: %w", effect.Destination(), err)
		}
		if err := authority.hostMutation().VerifyPath(ctx, destination, false, "", effect.ContentKind(), 0); err != nil {
			progress.record(mutationIndex, hostEffectIndeterminate)
			return mutationfs.MarkEffectIndeterminate(fmt.Errorf("verify removed managed destination %q: %w", effect.Destination(), err))
		}
		progress.record(mutationIndex, hostEffectExpectedAfter)
		return nil
	}

	prepared, err := effectpreparation.ManagedPathPayload(effect, payloads)
	if err != nil {
		return err
	}
	if err := commitManagedPathDestination(ctx, effect, prepared, precondition); err != nil {
		progress.record(mutationIndex, progressAfterMutationError(err))
		return fmt.Errorf("write destination %q: %w", effect.Destination(), err)
	}
	if err := authority.hostMutation().VerifyPath(
		ctx,
		destination,
		true,
		effect.DesiredHash(),
		effect.ContentKind(),
		effect.DesiredFileMode(),
	); err != nil {
		progress.record(mutationIndex, hostEffectIndeterminate)
		return mutationfs.MarkEffectIndeterminate(fmt.Errorf("verify managed destination %q: %w", effect.Destination(), err))
	}
	progress.record(mutationIndex, hostEffectExpectedAfter)
	return nil
}

func applyManagedPathRelocationPublish(
	ctx context.Context,
	effect effectpreparation.ManagedPathEffect,
	payloads payload.PayloadSet,
	authority *mutationAuthority,
	progress hostActionProgress,
	mutationIndex int,
) error {
	newDestination, err := authority.destinations.Resolve(effect.Scope(), effect.Destination())
	if err != nil {
		return err
	}
	newPrecondition, err := authority.hostMutation().CapturePath(
		ctx, newDestination, false, "", effect.ContentKind(), nil,
	)
	if err != nil {
		return err
	}
	defer newPrecondition.Close()
	prepared, err := effectpreparation.ManagedPathPayload(effect, payloads)
	if err != nil {
		return err
	}
	if err := commitManagedPathDestination(ctx, effect, prepared, newPrecondition); err != nil {
		progress.record(mutationIndex, progressAfterMutationError(err))
		return fmt.Errorf("write relocated managed destination %q: %w", effect.Destination(), err)
	}
	if err := authority.hostMutation().VerifyPath(
		ctx,
		newDestination,
		true,
		effect.DesiredHash(),
		effect.ContentKind(),
		effect.DesiredFileMode(),
	); err != nil {
		progress.record(mutationIndex, hostEffectIndeterminate)
		return mutationfs.MarkEffectIndeterminate(fmt.Errorf("verify relocated managed destination %q: %w", effect.Destination(), err))
	}
	progress.record(mutationIndex, hostEffectExpectedAfter)
	return nil
}

func applyManagedPathRelocationRetire(
	ctx context.Context,
	previous durable.ManagedPathState,
	authority *mutationAuthority,
	progress hostActionProgress,
	mutationIndex int,
) error {
	oldDestination, err := authority.destinations.Resolve(previous.Scope(), previous.Destination())
	if err != nil {
		return err
	}
	oldPrecondition, err := authority.hostMutation().CapturePath(
		ctx, oldDestination, true, previous.ContentHash(), previous.ContentKind(), nil,
	)
	if err != nil {
		return err
	}
	defer oldPrecondition.Close()
	if err := oldPrecondition.Remove(ctx); err != nil {
		progress.record(mutationIndex, progressAfterMutationError(err))
		return fmt.Errorf("remove previous managed destination %q: %w", previous.Destination(), err)
	}
	if err := authority.hostMutation().VerifyPath(
		ctx, oldDestination, false, "", previous.ContentKind(), 0,
	); err != nil {
		progress.record(mutationIndex, hostEffectIndeterminate)
		return mutationfs.MarkEffectIndeterminate(fmt.Errorf("verify previous managed destination removal %q: %w", previous.Destination(), err))
	}
	progress.record(mutationIndex, hostEffectExpectedAfter)
	return nil
}

func commitManagedPathDestination(
	ctx context.Context,
	effect effectpreparation.ManagedPathEffect,
	prepared payload.Payload,
	precondition hostmutation.PreparedPath,
) error {
	switch effect.ContentKind() {
	case realization.PathProjectionFile:
		file, ok := prepared.File()
		if !ok {
			return fmt.Errorf("managed file effect for %q has a non-file payload", effect.Destination())
		}
		return precondition.WriteFile(
			ctx,
			file.Bytes(),
			effect.DesiredFileMode(),
		)
	case realization.PathProjectionDirectory:
		directory, ok := prepared.Directory()
		if !ok {
			return fmt.Errorf("managed directory effect for %q has a non-directory payload", effect.Destination())
		}
		return precondition.WriteDirectory(
			ctx,
			directory.Identity(),
			directory.View(),
		)
	default:
		return fmt.Errorf("managed path content kind %q is unsupported", effect.ContentKind())
	}
}
