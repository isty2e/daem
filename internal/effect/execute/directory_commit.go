package execute

import (
	"context"
	"errors"
	"fmt"
	"os"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/filesystem/artifactstage"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

func commitManagedDirectoryDestination(
	ctx context.Context,
	authority *mutationAuthority,
	identity artifact.ExactIdentity,
	view access.View,
	destination mutationDestination,
	precondition *managedPathPrecondition,
) error {
	if precondition == nil || precondition.destination.logical != destination.logical {
		return fmt.Errorf("managed directory precondition does not match destination %q", destination.logical)
	}
	if !destination.isRooted() {
		return errors.New("mutation destination is invalid")
	}
	return commitRootedDirectoryWithPrecondition(ctx, authority, destination, func(writer mutationfs.RootedTreeWriter) error {
		sink, err := artifactstage.New(writer)
		if err != nil {
			return err
		}
		return view.CopyVerified(ctx, identity, sink)
	}, precondition)
}

func commitRootedDirectoryWithPrecondition(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationDestination,
	populate func(mutationfs.RootedTreeWriter) error,
	precondition *managedPathPrecondition,
) (resultErr error) {
	publicationCapability, err := authority.acquire(destination)
	if err != nil {
		return err
	}
	prepared, err := authority.filesystem.PrepareRootedTree(ctx, publicationCapability, populate)
	if err != nil {
		return err
	}
	defer func() {
		if abortErr := prepared.Abort(context.Background()); abortErr != nil {
			resultErr = markHostEffectIndeterminate(
				errors.Join(resultErr, fmt.Errorf("abort prepared rooted tree: %w", abortErr)),
			)
		}
	}()

	destinationRetired := false
	var removalCapability rootedpath.CommitCapability
	if precondition != nil {
		removalCapability, err = precondition.takeRootedCapability()
	} else {
		removalCapability, err = authority.acquire(destination)
	}
	if err != nil {
		return err
	}
	var expected mutationfs.EntryIdentity
	if precondition != nil {
		expected = precondition.identity
		if !precondition.existed {
			_ = removalCapability.Close()
			removalCapability = nil
		}
	} else {
		expected, err = authority.filesystem.CaptureRootedEntryIdentity(ctx, removalCapability)
	}
	if precondition != nil && !precondition.existed {
		// The captured absence is enforced by the prepared publication's
		// no-clobber commit; there is no existing destination to retire.
	} else if removalCapability != nil && (precondition != nil || err == nil) {
		if _, err := authority.removeJournaledRootedEntry(ctx, destination, removalCapability, expected); err != nil {
			return err
		}
		destinationRetired = true
	} else if precondition == nil && errors.Is(err, os.ErrNotExist) {
		_ = removalCapability.Close()
	} else {
		if removalCapability != nil {
			_ = removalCapability.Close()
		}
		return err
	}

	err = prepared.Commit(ctx)
	if err != nil && destinationRetired {
		return markHostEffectIndeterminate(err)
	}
	return err
}

type recoveryDirectorySource interface {
	copyDirectory(context.Context, mutationfs.RootedTreeWriter) error
}

func commitRecoveryDirectoryDestinationAgainst(
	ctx context.Context,
	authority *mutationAuthority,
	backup recoveryDirectorySource,
	destination mutationDestination,
	expectedExists bool,
	expected mutationfs.EntryIdentity,
) error {
	if !destination.isRooted() {
		return errors.New("mutation destination is invalid")
	}
	capability, err := authority.acquire(destination)
	if err != nil {
		return err
	}
	precondition := &managedPathPrecondition{
		destination: destination,
		existed:     expectedExists,
		identity:    expected,
		capability:  capability,
		filesystem:  authority.filesystem,
	}
	defer precondition.close()
	return commitRootedDirectoryWithPrecondition(
		ctx,
		authority,
		destination,
		func(writer mutationfs.RootedTreeWriter) error {
			return backup.copyDirectory(ctx, writer)
		},
		precondition,
	)
}
