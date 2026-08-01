package execute

import (
	"context"
	"errors"
	"fmt"
	"os"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func applyAggregateEffectsWithEvents(
	ctx context.Context,
	effects []AggregateEffect,
	authority *mutationAuthority,
	codecs aggregate.CodecCatalog,
	eventIndexOffset int,
	events applyEventEmitter,
	gate visibilityEffectGate,
	afterMutation func(context.Context, AggregateEffect) error,
) (hostActionProgress, error) {
	journaledProjectionCount := 0
	for _, effect := range effects {
		journaledProjectionCount += effect.journaledProjectionCount()
	}
	progress := newHostActionProgress(journaledProjectionCount)
	journalProjectionIndex := 0
	eventIndex := eventIndexOffset
	for _, effect := range effects {
		subjects := effect.SubjectEffects()
		for _, subject := range subjects {
			facts := aggregateEventFacts(eventIndex, subject)
			events.emit(EventActionStarted, EventStageAction, cloneActionEventFacts(facts), nil)
			eventIndex++
		}

		destination, err := authority.resolveBoundDestination(effect.Scope(), effect.Destination())
		if err == nil {
			err = verifyAggregateOperationPreconditions(ctx, authority, effect)
		}
		hostProgress := hostEffectNotStarted
		if err == nil {
			if effect.Kind() == AggregateEffectRecord {
				err = verifyAggregateBefore(ctx, authority, destination, effect, codecs)
			} else {
				err = gate.validateBefore(ctx)
				if err == nil {
					outcome := commitAggregateEffect(ctx, authority, destination, effect, codecs)
					err = outcome.err
					hostProgress = hostEffectExpectedAfter
					if err != nil {
						hostProgress = progressAfterMutationError(err)
					}
				}
				if err == nil {
					err = gate.acceptAfter(ctx)
				}
				if err == nil && afterMutation != nil {
					err = afterMutation(ctx, effect)
				}
			}
		}
		for _, projection := range effect.projections {
			if !projection.isJournaled() {
				continue
			}
			if projection.MutatesHost() {
				progress.record(journalProjectionIndex, hostProgress)
			}
			journalProjectionIndex++
		}
		for index, subject := range subjects {
			facts := aggregateEventFacts(eventIndex-len(subjects)+index, subject)
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
	effect AggregateEffect,
) error {
	for _, precondition := range effect.OperationPreconditions() {
		document := precondition.DocumentAddress()
		destination, err := authority.resolveBoundDestination(
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
	destination mutationDestination,
	effect AggregateEffect,
	codecs aggregate.CodecCatalog,
) fileMutationOutcome {
	if !destination.isRooted() {
		return fileMutationOutcome{err: fmt.Errorf("aggregate mutation destination is invalid")}
	}
	capability, err := authority.acquire(destination)
	if err != nil {
		return fileMutationOutcome{err: err}
	}
	content, mode, identity, readErr := authority.filesystem.ReadRootedRegularFile(ctx, capability)
	exists := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		_ = capability.Close()
		return fileMutationOutcome{err: readErr}
	}
	current := aggregate.AbsentDocument()
	if exists {
		current = aggregate.ExistingDocument(content)
	}
	candidate, err := validateAndRenderAggregate(effect, current, mode, codecs)
	if err != nil {
		_ = capability.Close()
		return fileMutationOutcome{err: err}
	}
	outcome := commitRootedAggregateCandidate(
		ctx,
		authority.filesystem,
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
	filesystem mutationfs.RootedCommitter,
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
		return attemptedFileMutation(filesystem.RemoveRootedEntry(ctx, capability, identity))
	case !exists:
		return attemptedFileMutation(filesystem.CreateRootedFile(
			ctx,
			capability,
			candidate.Content(),
			aggregate.DocumentFileMode,
		))
	default:
		return attemptedFileMutation(filesystem.ReplaceRootedFile(
			ctx,
			capability,
			candidate.Content(),
			aggregate.DocumentFileMode,
			identity,
		))
	}
}

func validateAndRenderAggregate(
	effect AggregateEffect,
	current aggregate.Document,
	mode os.FileMode,
	codecs aggregate.CodecCatalog,
) (aggregate.Document, error) {
	codec, err := validateAggregateBefore(effect, current, mode, codecs)
	if err != nil {
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
	effect AggregateEffect,
	current aggregate.Document,
	mode os.FileMode,
	codecs aggregate.CodecCatalog,
) (aggregate.Codec, error) {
	if !current.Equal(effect.BeforeDocument()) {
		return nil, fmt.Errorf("aggregate document changed after planning")
	}
	if current.Exists() && mode.Perm() != effect.Evidence().FileMode().Perm() {
		return nil, fmt.Errorf("aggregate document mode changed after planning")
	}
	codec, ok := codecs.Lookup(effect.CodecContractID())
	if !ok {
		return nil, fmt.Errorf("aggregate codec %q is not admitted", effect.CodecContractID())
	}
	selection, err := effect.BeforeSnapshot().Selection()
	if err != nil {
		return nil, err
	}
	snapshot, failure := codec.Read(current, selection)
	if failure != nil {
		return nil, failure
	}
	if !snapshot.Equal(effect.BeforeSnapshot()) {
		return nil, fmt.Errorf("aggregate selected projection changed after planning")
	}
	return codec, nil
}

func verifyAggregateBefore(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationDestination,
	effect AggregateEffect,
	codecs aggregate.CodecCatalog,
) error {
	current, mode, err := readAggregateDocumentDestination(ctx, authority, destination)
	if err != nil {
		return err
	}
	_, err = validateAggregateBefore(effect, current, mode, codecs)
	return err
}

func verifyAggregatePostcondition(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationDestination,
	effect AggregateEffect,
	expectedDocument aggregate.Document,
	codecs aggregate.CodecCatalog,
) error {
	current, mode, err := readAggregateDocumentDestination(ctx, authority, destination)
	if err != nil {
		return fmt.Errorf("reread aggregate postcondition: %w", err)
	}
	if !current.Equal(expectedDocument) {
		return fmt.Errorf("aggregate document postcondition differs from rendered candidate")
	}
	if current.Exists() && mode.Perm() != aggregate.DocumentFileMode {
		return fmt.Errorf("aggregate document mode = %04o, want %04o", mode.Perm(), aggregate.DocumentFileMode)
	}
	codec, ok := codecs.Lookup(effect.CodecContractID())
	if !ok {
		return fmt.Errorf("aggregate codec %q is not admitted", effect.CodecContractID())
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
	destination mutationDestination,
) (aggregate.Document, os.FileMode, error) {
	content, mode, err := readRegularFileDestination(ctx, authority, destination)
	if errors.Is(err, os.ErrNotExist) {
		return aggregate.AbsentDocument(), 0, nil
	}
	if err != nil {
		return aggregate.Document{}, 0, err
	}
	return aggregate.ExistingDocument(content), mode, nil
}

func aggregateEventFacts(index int, subject AggregateSubjectEffect) ActionEventFacts {
	contract := subject.Contract()
	return ActionEventFacts{
		Index: index, AggregateKind: subject.Kind(), Subject: subject.Subject(),
		Target: contract.Address().Document().Target(), Scope: contract.Address().Document().Scope(),
		Destination: contract.Address().Document().AggregateRoot(),
	}
}
