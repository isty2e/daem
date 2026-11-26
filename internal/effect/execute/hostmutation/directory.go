package hostmutation

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/filesystem/artifactstage"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
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
		return view.CopyVerified(ctx, identity, sink.ForInstallation(precondition.treeModes))
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

	changed, err := precondition.refreshTree(ctx)
	if err != nil {
		return err
	}
	if changed {
		if err := prepared.PreserveMatchingModes(ctx, precondition.treeModes); err != nil {
			return err
		}
	}

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
		if _, err := operation.removals.RemoveRootedEntry(ctx, destination, removalCapability, precondition.identity, ""); err != nil {
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
	work workmeasure.ArtifactWork,
	budget *physicalwork.Budget,
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
		identity:    expected,
		capability:  capability,
		filesystem:  operation.filesystem,
	}
	defer precondition.close()
	if state.existed && state.kind == recovery.PathKindDirectory {
		if err := admitRecoveryObservation(budget, state.kind, work); err != nil {
			return err
		}
		precondition.treeLimits, err = recoveryTraversalLimits(work)
		if err != nil {
			return err
		}
		precondition.hash = artifact.ContentHash(state.contentHash)
		precondition.treeModes = make(map[string]fs.FileMode)
	}
	return operation.commitRootedDirectoryWithPrecondition(
		ctx,
		populate,
		precondition,
	)
}
