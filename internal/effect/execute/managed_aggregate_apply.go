package execute

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/topology"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func applyAggregateEffectsWithEvents(
	ctx context.Context,
	effects []effectpreparation.AggregateEffect,
	authority *mutationAuthority,
	codecs aggregate.CodecCatalog,
	eventIndexOffset int,
	events applyEventEmitter,
	gate visibilityEffectGate,
	execution *applyEffectExecution,
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
			return verifyAggregateOperationPreconditions(ctx, authority, effect.OperationPreconditions())
		}
		hostProgress := hostEffectNotStarted
		var err error
		if effect.Kind() == effectpreparation.AggregateEffectRecord {
			err = execution.runObservation(
				prefix+"/record",
				applyForwardFailureGuardedRecovery,
				func() error {
					if observeErr := observePreconditions(); observeErr != nil {
						return observeErr
					}
					return verifyAggregateBefore(ctx, authority, destination, effect, codecs)
				},
			)
		} else {
			err = execution.runObservation(
				prefix+"/preconditions",
				applyForwardFailureGuardedRecovery,
				observePreconditions,
			)
			operationGate := execution.visibilityGate(
				gate,
				prefix,
				operationplan.EffectStepPersistence,
				applyForwardFailureGuardedRecovery,
			)
			if err == nil {
				err = operationGate.validateBefore(ctx)
			}
			if err == nil {
				err = operationGate.applyEffect(func() error {
					outcome := commitAggregateEffect(ctx, authority, destination, effect, codecs)
					hostProgress = hostEffectExpectedAfter
					if outcome.err != nil {
						hostProgress = progressAfterMutationError(outcome.err)
					}
					return outcome.err
				})
			}
			if err == nil {
				err = operationGate.acceptAfter(ctx)
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

func verifyAggregateOperationPreconditions(
	ctx context.Context,
	authority *mutationAuthority,
	preconditions []aggregate.OperationPrecondition,
) error {
	for _, precondition := range preconditions {
		document := precondition.DocumentAddress()
		destination, err := authority.destinations.Resolve(
			document.Scope(),
			document.AggregateRoot(),
		)
		if err != nil {
			return err
		}
		exists, err := destinationEntryExists(ctx, authority, destination)
		if err != nil {
			return fmt.Errorf(
				"inspect aggregate operation precondition %q: %w",
				document.AggregateRoot(),
				err,
			)
		}
		satisfied := precondition.Kind() != aggregate.OperationPreconditionDocumentAbsent || !exists
		if !satisfied {
			return fmt.Errorf("%s", precondition.UnsatisfiedDetail())
		}
	}
	return nil
}

func commitAggregateEffect(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationnamespace.Destination,
	effect effectpreparation.AggregateEffect,
	codecs aggregate.CodecCatalog,
) fileMutationOutcome {
	if !destination.IsRooted() {
		return fileMutationOutcome{err: fmt.Errorf("aggregate mutation destination is invalid")}
	}
	capability, err := authority.destinations.Acquire(destination)
	if err != nil {
		return fileMutationOutcome{err: err}
	}
	codec, ok := codecs.Lookup(effect.CodecContractID())
	if !ok {
		_ = capability.Close()
		return fileMutationOutcome{err: fmt.Errorf("aggregate codec %q is not admitted", effect.CodecContractID())}
	}
	content, mode, identity, readErr := authority.filesystem.ReadRootedRegularFileUpTo(
		ctx,
		capability,
		codec.MaximumDocumentBytes(),
	)
	exists := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		_ = capability.Close()
		return fileMutationOutcome{err: readErr}
	}
	current := aggregate.AbsentDocument()
	if exists {
		current = aggregate.ExistingDocument(content)
	}
	candidate, err := validateAndRenderAggregate(effect, current, mode, codec)
	if err != nil {
		_ = capability.Close()
		return fileMutationOutcome{err: err}
	}
	outcome := commitRootedAggregateCandidate(
		ctx,
		authority,
		destination,
		capability,
		identity,
		exists,
		candidate,
	)
	if outcome.err != nil {
		return outcome
	}
	if err := verifyAggregatePostcondition(ctx, authority, destination, effect, candidate, codecs); err != nil {
		return fileMutationOutcome{err: markHostEffectIndeterminate(err), commitAttempted: true}
	}
	return outcome
}

func commitRootedAggregateCandidate(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationnamespace.Destination,
	capability rootedpath.CommitCapability,
	identity mutationfs.EntryIdentity,
	exists bool,
	candidate aggregate.Document,
) fileMutationOutcome {
	switch {
	case !candidate.Exists() && !exists:
		if err := capability.Close(); err != nil {
			return fileMutationOutcome{err: err}
		}
		return fileMutationOutcome{}
	case !candidate.Exists():
		_, err := authority.removeJournaledRootedEntry(ctx, destination, capability, identity)
		return attemptedFileMutation(err)
	case !exists:
		return attemptedFileMutation(authority.filesystem.CreateRootedFile(
			ctx,
			capability,
			candidate.Content(),
			aggregate.DocumentFileMode,
		))
	default:
		return attemptedFileMutation(authority.filesystem.ReplaceRootedFile(
			ctx,
			capability,
			candidate.Content(),
			aggregate.DocumentFileMode,
			identity,
		))
	}
}

func validateAndRenderAggregate(
	effect effectpreparation.AggregateEffect,
	current aggregate.Document,
	mode os.FileMode,
	codec aggregate.Codec,
) (aggregate.Document, error) {
	if err := validateAggregateBefore(effect, current, mode, codec); err != nil {
		return aggregate.Document{}, err
	}
	rendered, failure := codec.Render(current, effect.CodecPlan())
	if failure != nil {
		return aggregate.Document{}, failure
	}
	if !rendered.Document().Equal(effect.Rendered().Document()) || !rendered.Expected().Equal(effect.Rendered().Expected()) {
		return aggregate.Document{}, fmt.Errorf("aggregate codec candidate changed after planning")
	}
	return rendered.Document(), nil
}

func validateAggregateBefore(
	effect effectpreparation.AggregateEffect,
	current aggregate.Document,
	mode os.FileMode,
	codec aggregate.Codec,
) error {
	if !current.Equal(effect.BeforeDocument()) {
		return fmt.Errorf("aggregate document changed after planning")
	}
	if current.Exists() && mode.Perm() != effect.Evidence().FileMode().Perm() {
		return fmt.Errorf("aggregate document mode changed after planning")
	}
	selection, err := effect.BeforeSnapshot().Selection()
	if err != nil {
		return err
	}
	snapshot, failure := codec.Read(current, selection)
	if failure != nil {
		return failure
	}
	if !snapshot.Equal(effect.BeforeSnapshot()) {
		return fmt.Errorf("aggregate selected projection changed after planning")
	}
	return nil
}

func verifyAggregateBefore(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationnamespace.Destination,
	effect effectpreparation.AggregateEffect,
	codecs aggregate.CodecCatalog,
) error {
	codec, ok := codecs.Lookup(effect.CodecContractID())
	if !ok {
		return fmt.Errorf("aggregate codec %q is not admitted", effect.CodecContractID())
	}
	current, mode, err := readAggregateDocumentDestination(
		ctx,
		authority,
		destination,
		codec.MaximumDocumentBytes(),
	)
	if err != nil {
		return err
	}
	return validateAggregateBefore(effect, current, mode, codec)
}

func verifyAggregatePostcondition(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationnamespace.Destination,
	effect effectpreparation.AggregateEffect,
	expectedDocument aggregate.Document,
	codecs aggregate.CodecCatalog,
) error {
	codec, ok := codecs.Lookup(effect.CodecContractID())
	if !ok {
		return fmt.Errorf("aggregate codec %q is not admitted", effect.CodecContractID())
	}
	current, mode, err := readAggregateDocumentDestination(
		ctx,
		authority,
		destination,
		codec.MaximumDocumentBytes(),
	)
	if err != nil {
		return fmt.Errorf("reread aggregate postcondition: %w", err)
	}
	if !current.Equal(expectedDocument) {
		return fmt.Errorf("aggregate document postcondition differs from rendered candidate")
	}
	if current.Exists() && mode.Perm() != aggregate.DocumentFileMode {
		return fmt.Errorf("aggregate document mode = %04o, want %04o", mode.Perm(), aggregate.DocumentFileMode)
	}
	selection, err := effect.Rendered().Expected().Selection()
	if err != nil {
		return err
	}
	snapshot, failure := codec.Read(current, selection)
	if failure != nil {
		return failure
	}
	if !snapshot.Equal(effect.Rendered().Expected()) {
		return fmt.Errorf("aggregate selected projection postcondition failed")
	}
	return nil
}

func readAggregateDocumentDestination(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationnamespace.Destination,
	maximumBytes int64,
) (aggregate.Document, os.FileMode, error) {
	content, mode, err := readRegularFileDestination(ctx, authority, destination, maximumBytes)
	if errors.Is(err, os.ErrNotExist) {
		return aggregate.AbsentDocument(), 0, nil
	}
	if err != nil {
		return aggregate.Document{}, 0, err
	}
	return aggregate.ExistingDocument(content), mode, nil
}

func aggregateEventFacts(index int, kind effectpreparation.AggregateEffectKind, subject topology.SubjectID, contract aggregate.ProjectionContract) ActionEventFacts {
	return ActionEventFacts{
		Index: index, AggregateKind: kind, Subject: subject,
		Target: contract.Address().Document().Target(), Scope: contract.Address().Document().Scope(),
		Destination: contract.Address().Document().AggregateRoot(),
	}
}
