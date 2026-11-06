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
		return view.CopyVerified(ctx, identity, sink)
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
	destination := precondition.destination
	publicationCapability, err := operation.destinations.Acquire(destination)
	if err != nil {
		return err
	}
	prepared, err := operation.filesystem.PrepareRootedTree(ctx, publicationCapability, populate)
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
		if _, err := operation.removals.RemoveRootedEntry(ctx, destination, removalCapability, precondition.identity); err != nil {
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

func (operation *Operation) CommitDirectoryAgainst(
	ctx context.Context,
	destination mutationnamespace.Destination,
	expectedExists bool,
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
		existed:     expectedExists,
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
