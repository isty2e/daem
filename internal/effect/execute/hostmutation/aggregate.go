package hostmutation

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func (operation *Operation) VerifyAggregatePreconditions(
	ctx context.Context,
	preconditions []aggregate.OperationPrecondition,
) error {
	for _, precondition := range preconditions {
		document := precondition.DocumentAddress()
		destination, err := operation.destinations.Resolve(
			document.Scope(),
			document.AggregateRoot(),
		)
		if err != nil {
			return err
		}
		exists, err := operation.entryExists(ctx, destination)
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

func (operation *Operation) CommitAggregate(
	ctx context.Context,
	destination mutationnamespace.Destination,
	effect effectpreparation.AggregateEffect,
	codecs aggregate.CodecCatalog,
) error {
	if !destination.IsRooted() {
		return fmt.Errorf("aggregate mutation destination is invalid")
	}
	capability, err := operation.destinations.Acquire(destination)
	if err != nil {
		return err
	}
	codec, ok := codecs.Lookup(effect.CodecContractID())
	if !ok {
		_ = capability.Close()
		return fmt.Errorf("aggregate codec %q is not admitted", effect.CodecContractID())
	}
	content, mode, identity, readErr := operation.filesystem.ReadRootedRegularFileUpTo(
		ctx,
		capability,
		codec.MaximumDocumentBytes(),
	)
	exists := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		_ = capability.Close()
		return readErr
	}
	current := aggregate.AbsentDocument()
	if exists {
		current = aggregate.ExistingDocument(content)
	}
	candidate, err := validateAndRenderAggregate(effect, current, mode, codec)
	if err != nil {
		_ = capability.Close()
		return err
	}
	err = operation.commitRootedAggregateCandidate(
		ctx,
		destination,
		capability,
		identity,
		exists,
		candidate,
	)
	if err != nil {
		return err
	}
	if err := operation.verifyAggregatePostcondition(ctx, destination, effect, candidate, codecs); err != nil {
		return mutationfs.MarkEffectIndeterminate(err)
	}
	return nil
}

func (operation *Operation) commitRootedAggregateCandidate(
	ctx context.Context,
	destination mutationnamespace.Destination,
	capability rootedpath.CommitCapability,
	identity mutationfs.EntryIdentity,
	exists bool,
	candidate aggregate.Document,
) error {
	switch {
	case !candidate.Exists() && !exists:
		if err := capability.Close(); err != nil {
			return err
		}
		return nil
	case !candidate.Exists():
		_, err := operation.removals.RemoveRootedEntry(ctx, destination, capability, identity)
		return err
	case !exists:
		return operation.filesystem.CreateRootedFile(
			ctx,
			capability,
			candidate.Content(),
			aggregate.DocumentFileMode,
		)
	default:
		return operation.filesystem.ReplaceRootedFile(
			ctx,
			capability,
			candidate.Content(),
			aggregate.DocumentFileMode,
			identity,
		)
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

func (operation *Operation) VerifyAggregateBefore(
	ctx context.Context,
	destination mutationnamespace.Destination,
	effect effectpreparation.AggregateEffect,
	codecs aggregate.CodecCatalog,
) error {
	codec, ok := codecs.Lookup(effect.CodecContractID())
	if !ok {
		return fmt.Errorf("aggregate codec %q is not admitted", effect.CodecContractID())
	}
	current, mode, err := operation.ReadDocument(
		ctx,
		destination,
		codec.MaximumDocumentBytes(),
	)
	if err != nil {
		return err
	}
	return validateAggregateBefore(effect, current, mode, codec)
}

func (operation *Operation) verifyAggregatePostcondition(
	ctx context.Context,
	destination mutationnamespace.Destination,
	effect effectpreparation.AggregateEffect,
	expectedDocument aggregate.Document,
	codecs aggregate.CodecCatalog,
) error {
	codec, ok := codecs.Lookup(effect.CodecContractID())
	if !ok {
		return fmt.Errorf("aggregate codec %q is not admitted", effect.CodecContractID())
	}
	current, mode, err := operation.ReadDocument(
		ctx,
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

func (operation *Operation) ReadDocument(
	ctx context.Context,
	destination mutationnamespace.Destination,
	maximumBytes int64,
) (aggregate.Document, os.FileMode, error) {
	content, mode, err := operation.readFile(ctx, destination, maximumBytes)
	if errors.Is(err, os.ErrNotExist) {
		return aggregate.AbsentDocument(), 0, nil
	}
	if err != nil {
		return aggregate.Document{}, 0, err
	}
	return aggregate.ExistingDocument(content), mode, nil
}
