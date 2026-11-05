package execute

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

type fileContentMutation func(existing []byte, mode os.FileMode, exists bool) (content []byte, keepFile bool, err error)

type fileMutationOutcome struct {
	err             error
	commitAttempted bool
}

func attemptedFileMutation(err error) fileMutationOutcome {
	return fileMutationOutcome{err: err, commitAttempted: true}
}

func commitFileDestinationAgainst(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationnamespace.Destination,
	content []byte,
	fileMode os.FileMode,
	expectedExists bool,
	expected mutationfs.EntryIdentity,
) error {
	if !destination.IsRooted() {
		return errors.New("mutation destination is invalid")
	}
	capability, err := authority.destinations.Acquire(destination)
	if err != nil {
		return err
	}
	if expectedExists {
		return authority.filesystem.ReplaceRootedFile(ctx, capability, content, fileMode, expected)
	}
	return authority.filesystem.CreateRootedFile(ctx, capability, content, fileMode)
}

func mutateFileDestinationWithOutcome(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationnamespace.Destination,
	fileMode os.FileMode,
	allowMissing bool,
	maximumBytes int64,
	mutation fileContentMutation,
) fileMutationOutcome {
	if !destination.IsRooted() {
		return fileMutationOutcome{err: errors.New("mutation destination is invalid")}
	}
	capability, err := authority.destinations.Acquire(destination)
	if err != nil {
		return fileMutationOutcome{err: err}
	}
	existing, mode, expected, err := authority.filesystem.ReadRootedRegularFileUpTo(ctx, capability, maximumBytes)
	exists := err == nil
	if err != nil && !(allowMissing && errors.Is(err, os.ErrNotExist)) {
		_ = capability.Close()
		return fileMutationOutcome{err: err}
	}
	content, keepFile, err := mutation(existing, mode, exists)
	if err != nil {
		_ = capability.Close()
		return fileMutationOutcome{err: err}
	}
	if !keepFile {
		if !exists {
			_ = capability.Close()
			return fileMutationOutcome{}
		}
		_, err := authority.removeJournaledRootedEntry(ctx, destination, capability, expected)
		return attemptedFileMutation(err)
	}
	if !exists {
		return attemptedFileMutation(authority.filesystem.CreateRootedFile(ctx, capability, content, fileMode))
	}
	return attemptedFileMutation(authority.filesystem.ReplaceRootedFile(ctx, capability, content, fileMode, expected))
}

func readRegularFileDestination(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationnamespace.Destination,
	maximumBytes int64,
) ([]byte, os.FileMode, error) {
	if !destination.IsRooted() {
		return nil, 0, errors.New("mutation destination is invalid")
	}
	capability, err := authority.destinations.Acquire(destination)
	if err != nil {
		return nil, 0, err
	}
	defer capability.Close()
	content, mode, _, err := authority.filesystem.ReadRootedRegularFileUpTo(ctx, capability, maximumBytes)
	return content, mode, err
}

func destinationEntryExists(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationnamespace.Destination,
) (bool, error) {
	if !destination.IsRooted() {
		return false, errors.New("mutation destination is invalid")
	}
	capability, err := authority.destinations.Acquire(destination)
	if err != nil {
		return false, err
	}
	defer capability.Close()
	if _, err := authority.filesystem.CaptureRootedEntryIdentity(ctx, capability); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func removeDestinationAgainst(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationnamespace.Destination,
	expected mutationfs.EntryIdentity,
) error {
	if !destination.IsRooted() {
		return errors.New("mutation destination is invalid")
	}
	capability, err := authority.destinations.Acquire(destination)
	if err != nil {
		return err
	}
	_, err = authority.removeJournaledRootedEntry(ctx, destination, capability, expected)
	return err
}

func removeManagedPathDestination(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationnamespace.Destination,
	precondition *managedPathPrecondition,
) error {
	if precondition == nil || !precondition.existed || precondition.destination.Logical() != destination.Logical() {
		return fmt.Errorf("managed removal requires a matching existing precondition for %q", destination.Logical())
	}
	if !destination.IsRooted() {
		return errors.New("mutation destination is invalid")
	}
	capability, err := precondition.takeRootedCapability()
	if err != nil {
		return err
	}
	_, err = authority.removeJournaledRootedEntry(ctx, destination, capability, precondition.identity)
	return err
}

func commitManagedFileDestination(
	ctx context.Context,
	destination mutationnamespace.Destination,
	content []byte,
	fileMode os.FileMode,
	precondition *managedPathPrecondition,
) error {
	if precondition == nil || precondition.destination.Logical() != destination.Logical() {
		return fmt.Errorf("managed file precondition does not match destination %q", destination.Logical())
	}
	if !destination.IsRooted() {
		return errors.New("mutation destination is invalid")
	}
	capability, err := precondition.takeRootedCapability()
	if err != nil {
		return err
	}
	if !precondition.existed {
		return precondition.filesystem.CreateRootedFile(ctx, capability, content, fileMode)
	}
	return precondition.filesystem.ReplaceRootedFile(
		ctx,
		capability,
		content,
		fileMode,
		precondition.identity,
	)
}
