package execute

import (
	"context"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
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
	effects       []ManagedPathEffect
	publish       []managedPathPhaseOperation
	retire        []managedPathPhaseOperation
	mutationCount int
}

func newManagedPathExecutionSchedule(effects []ManagedPathEffect) (managedPathExecutionSchedule, error) {
	mutations, err := managedPathJournalMutations(effects)
	if err != nil {
		return managedPathExecutionSchedule{}, err
	}
	schedule := managedPathExecutionSchedule{effects: append([]ManagedPathEffect(nil), effects...), mutationCount: len(mutations)}
	mutationIndex := 0
	for effectIndex, effect := range effects {
		operation := managedPathPhaseOperation{effectIndex: effectIndex, mutationIndex: mutationIndex}
		if effect.Kind() == ManagedPathEffectReplace {
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
		if effect.Kind() == ManagedPathEffectRemove {
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
	gate visibilityEffectGate,
	execution *applyEffectExecution,
	afterMutation func(context.Context, ManagedPathEffect, managedPathPhase) error,
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
		mutatesHost := effect.Kind() != ManagedPathEffectRecord
		prefix := applyManagedPathScheduleReference(phase, operation.effectIndex)
		if effect.Kind() == ManagedPathEffectRecord {
			err = execution.runObservation(prefix+"/record", func() error {
				destination, resolveErr := authority.resolveBoundDestination(effect.Scope(), effect.Destination())
				if resolveErr != nil {
					return resolveErr
				}
				if verifyErr := verifyManagedPathPostcondition(
					ctx, authority, destination, true, effect.DesiredHash(), effect.ContentKind(), effect.LiveFileMode(),
				); verifyErr != nil {
					return verifyErr
				}
				progress.record(operation.mutationIndex, hostEffectExpectedAfter)
				return nil
			})
		} else {
			operationGate := execution.visibilityGate(gate, prefix, operationplan.EffectStepPersistence)
			if err = operationGate.validateBefore(ctx); err == nil {
				err = operationGate.applyEffect(func() error {
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
				err = operationGate.acceptAfter(ctx)
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
	effect ManagedPathEffect,
	payloads payload.PayloadSet,
	authority *mutationAuthority,
	progress hostActionProgress,
	mutationIndex int,
) error {
	destination, err := authority.resolveBoundDestination(effect.Scope(), effect.Destination())
	if err != nil {
		return err
	}
	expectedExists := effect.Kind() != ManagedPathEffectCreate
	expectedHash := effect.LiveHash()
	precondition, err := captureManagedPathPrecondition(
		ctx,
		authority,
		destination,
		expectedExists,
		expectedHash,
		effect.ContentKind(),
		nil,
	)
	if err != nil {
		return err
	}
	defer precondition.close()

	if effect.Kind() == ManagedPathEffectRemove {
		if err := removeManagedPathDestination(ctx, authority, destination, &precondition); err != nil {
			progress.record(mutationIndex, progressAfterMutationError(err))
			return fmt.Errorf("remove managed destination %q: %w", effect.Destination(), err)
		}
		if err := verifyManagedPathPostcondition(ctx, authority, destination, false, "", effect.ContentKind(), 0); err != nil {
			progress.record(mutationIndex, hostEffectIndeterminate)
			return markHostEffectIndeterminate(fmt.Errorf("verify removed managed destination %q: %w", effect.Destination(), err))
		}
		progress.record(mutationIndex, hostEffectExpectedAfter)
		return nil
	}

	prepared, err := managedPathPayload(effect, payloads)
	if err != nil {
		return err
	}
	if err := commitManagedPathDestination(ctx, authority, effect, prepared, destination, &precondition); err != nil {
		progress.record(mutationIndex, progressAfterMutationError(err))
		return fmt.Errorf("write destination %q: %w", effect.Destination(), err)
	}
	if err := verifyManagedPathPostcondition(
		ctx,
		authority,
		destination,
		true,
		effect.DesiredHash(),
		effect.ContentKind(),
		effect.DesiredFileMode(),
	); err != nil {
		progress.record(mutationIndex, hostEffectIndeterminate)
		return markHostEffectIndeterminate(fmt.Errorf("verify managed destination %q: %w", effect.Destination(), err))
	}
	progress.record(mutationIndex, hostEffectExpectedAfter)
	return nil
}

func applyManagedPathRelocationPublish(
	ctx context.Context,
	effect ManagedPathEffect,
	payloads payload.PayloadSet,
	authority *mutationAuthority,
	progress hostActionProgress,
	mutationIndex int,
) error {
	newDestination, err := authority.resolveBoundDestination(effect.Scope(), effect.Destination())
	if err != nil {
		return err
	}
	newPrecondition, err := captureManagedPathPrecondition(
		ctx, authority, newDestination, false, "", effect.ContentKind(), nil,
	)
	if err != nil {
		return err
	}
	defer newPrecondition.close()
	prepared, err := managedPathPayload(effect, payloads)
	if err != nil {
		return err
	}
	if err := commitManagedPathDestination(ctx, authority, effect, prepared, newDestination, &newPrecondition); err != nil {
		progress.record(mutationIndex, progressAfterMutationError(err))
		return fmt.Errorf("write relocated managed destination %q: %w", effect.Destination(), err)
	}
	if err := verifyManagedPathPostcondition(
		ctx,
		authority,
		newDestination,
		true,
		effect.DesiredHash(),
		effect.ContentKind(),
		effect.DesiredFileMode(),
	); err != nil {
		progress.record(mutationIndex, hostEffectIndeterminate)
		return markHostEffectIndeterminate(fmt.Errorf("verify relocated managed destination %q: %w", effect.Destination(), err))
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
	oldDestination, err := authority.resolveBoundDestination(previous.Scope(), previous.Destination())
	if err != nil {
		return err
	}
	oldPrecondition, err := captureManagedPathPrecondition(
		ctx, authority, oldDestination, true, previous.ContentHash(), previous.ContentKind(), nil,
	)
	if err != nil {
		return err
	}
	defer oldPrecondition.close()
	if err := removeManagedPathDestination(ctx, authority, oldDestination, &oldPrecondition); err != nil {
		progress.record(mutationIndex, progressAfterMutationError(err))
		return fmt.Errorf("remove previous managed destination %q: %w", previous.Destination(), err)
	}
	if err := verifyManagedPathPostcondition(
		ctx, authority, oldDestination, false, "", previous.ContentKind(), 0,
	); err != nil {
		progress.record(mutationIndex, hostEffectIndeterminate)
		return markHostEffectIndeterminate(fmt.Errorf("verify previous managed destination removal %q: %w", previous.Destination(), err))
	}
	progress.record(mutationIndex, hostEffectExpectedAfter)
	return nil
}

func managedPathPayload(effect ManagedPathEffect, payloads payload.PayloadSet) (payload.Payload, error) {
	prepared, ok := payloads.LookupSubject(effect.Subject())
	if !ok {
		return payload.Payload{}, fmt.Errorf(
			"missing managed path payload for subject %s/%s %q",
			effect.Subject().Kind(), effect.Subject().Namespace(), effect.Subject().Key(),
		)
	}
	if err := prepared.VerifyHash(effect.DesiredHash(), effect.Destination()); err != nil {
		return payload.Payload{}, err
	}
	switch effect.ContentKind() {
	case realization.PathProjectionFile:
		file, isFile := prepared.File()
		if !isFile {
			return payload.Payload{}, fmt.Errorf("managed file effect for %q has a non-file payload", effect.Destination())
		}
		if file.Mode().Perm() != effect.DesiredFileMode().Perm() {
			return payload.Payload{}, fmt.Errorf(
				"managed file payload mode %04o does not match planned mode %04o for %q",
				file.Mode().Perm(),
				effect.DesiredFileMode().Perm(),
				effect.Destination(),
			)
		}
	case realization.PathProjectionDirectory:
		if _, isDirectory := prepared.Directory(); !isDirectory {
			return payload.Payload{}, fmt.Errorf("managed directory effect for %q has a non-directory payload", effect.Destination())
		}
	default:
		return payload.Payload{}, fmt.Errorf("managed path content kind %q is unsupported", effect.ContentKind())
	}
	return prepared, nil
}

func commitManagedPathDestination(
	ctx context.Context,
	authority *mutationAuthority,
	effect ManagedPathEffect,
	prepared payload.Payload,
	destination mutationDestination,
	precondition *managedPathPrecondition,
) error {
	switch effect.ContentKind() {
	case realization.PathProjectionFile:
		file, ok := prepared.File()
		if !ok {
			return fmt.Errorf("managed file effect for %q has a non-file payload", effect.Destination())
		}
		return commitManagedFileDestination(
			ctx,
			destination,
			file.Bytes(),
			effect.DesiredFileMode(),
			precondition,
		)
	case realization.PathProjectionDirectory:
		directory, ok := prepared.Directory()
		if !ok {
			return fmt.Errorf("managed directory effect for %q has a non-directory payload", effect.Destination())
		}
		return commitManagedDirectoryDestination(
			ctx,
			authority,
			directory.Identity(),
			directory.View(),
			destination,
			precondition,
		)
	default:
		return fmt.Errorf("managed path content kind %q is unsupported", effect.ContentKind())
	}
}

func verifyManagedPathPostcondition(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationDestination,
	expectedExists bool,
	expectedHash artifact.ContentHash,
	contentKind realization.PathProjectionContentKind,
	expectedFileMode os.FileMode,
) error {
	var exactFileMode *os.FileMode
	if expectedExists && contentKind == realization.PathProjectionFile {
		mode := expectedFileMode.Perm()
		exactFileMode = &mode
	}
	observed, err := captureManagedPathPrecondition(
		ctx,
		authority,
		destination,
		expectedExists,
		expectedHash,
		contentKind,
		exactFileMode,
	)
	if err != nil {
		return err
	}
	return observed.close()
}
