package hostmutation

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/filesystem/artifactstage"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

func (path PreparedPath) WriteDirectory(
	ctx context.Context,
	identity artifact.ExactIdentity,
	view access.View,
) error {
	precondition := path.state
	if precondition == nil {
		return fmt.Errorf("managed directory precondition is unavailable")
	}
	destination := precondition.destination
	if !destination.IsRooted() {
		return errors.New("mutation destination is invalid")
	}
	return precondition.operation.commitRootedDirectoryWithPrecondition(ctx, func(writer mutationfs.RootedTreeWriter) error {
		sink, err := artifactstage.New(writer)
		if err != nil {
			return err
		}
		return view.CopyVerified(ctx, identity, sink.ForInstallation())
	}, precondition)
}

func (operation *Operation) commitRootedDirectoryWithPrecondition(
	ctx context.Context,
	populate func(mutationfs.RootedTreeWriter) error,
	precondition *managedPathPrecondition,
) (resultErr error) {
	if precondition == nil || precondition.consumed || precondition.capability == nil {
		return fmt.Errorf("managed path rooted precondition is unavailable or consumed")
	}
	if precondition.existed && precondition.identity == nil {
		return fmt.Errorf("existing tree publication destination requires a captured identity")
	}
	destination := precondition.destination
	capability, err := operation.destinations.Acquire(destination)
	if err != nil {
		return err
	}
	prepared, err := operation.filesystem.PrepareRootedTree(ctx, capability, populate)
	if err != nil {
		return err
	}
	defer func() {
		if abortErr := prepared.Abort(context.Background()); abortErr != nil {
			resultErr = mutationfs.MarkEffectIndeterminate(
				errors.Join(resultErr, fmt.Errorf("abort prepared rooted tree: %w", abortErr)),
			)
		}
	}()

	destinationRetired := false
	removalCapability, err := precondition.takeRootedCapability()
	if err != nil {
		return err
	}
	if !precondition.existed {
		// The captured absence is enforced by the prepared publication's
		// no-clobber commit; there is no existing destination to retire.
		_ = removalCapability.Close()
	} else {
		if precondition.identity.Kind() == mutationfs.EntryKindDirectory {
			_, err = operation.removals.RetireTreeForReplacement(ctx, destination, removalCapability, precondition.identity, precondition.hash, prepared)
		} else {
			_, err = operation.removals.RemoveRootedEntry(ctx, destination, removalCapability, precondition.identity, precondition.hash)
		}
		if err != nil {
			return err
		}
		destinationRetired = true
	}

	err = prepared.Commit(ctx)
	if err != nil && destinationRetired {
		return mutationfs.MarkEffectIndeterminate(err)
	}
	return err
}

func (operation *Operation) restoreDirectoryAgainst(
	ctx context.Context,
	destination mutationnamespace.Destination,
	state recoveryWholePathState,
	expected mutationfs.EntryIdentity,
	populate func(mutationfs.RootedTreeWriter) error,
) error {
	if !destination.IsRooted() {
		return errors.New("mutation destination is invalid")
	}
	capability, err := operation.destinations.Acquire(destination)
	if err != nil {
		return err
	}
	precondition := &managedPathPrecondition{
		destination: destination,
		existed:     state.existed,
		hash:        artifact.ContentHash(state.contentHash),
		identity:    expected,
		capability:  capability,
		filesystem:  operation.filesystem,
	}
	defer precondition.close()
	return operation.commitRootedDirectoryWithPrecondition(
		ctx,
		populate,
		precondition,
	)
}
