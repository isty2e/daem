package execute

import (
	"context"
	"errors"
	"fmt"
	"os"

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
	destination mutationDestination,
	content []byte,
	fileMode os.FileMode,
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
	if expectedExists {
		return authority.filesystem.ReplaceRootedFile(ctx, capability, content, fileMode, expected)
	}
	return authority.filesystem.CreateRootedFile(ctx, capability, content, fileMode)
}

func mutateFileDestinationWithOutcome(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationDestination,
	fileMode os.FileMode,
	allowMissing bool,
	mutation fileContentMutation,
) fileMutationOutcome {
	if !destination.isRooted() {
		return fileMutationOutcome{err: errors.New("mutation destination is invalid")}
	}
	capability, err := authority.acquire(destination)
	if err != nil {
		return fileMutationOutcome{err: err}
	}
	existing, mode, expected, err := authority.filesystem.ReadRootedRegularFile(ctx, capability)
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
		return attemptedFileMutation(authority.filesystem.RemoveRootedEntry(ctx, capability, expected))
	}
	if !exists {
		return attemptedFileMutation(authority.filesystem.CreateRootedFile(ctx, capability, content, fileMode))
	}
	return attemptedFileMutation(authority.filesystem.ReplaceRootedFile(ctx, capability, content, fileMode, expected))
}

func readRegularFileDestination(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationDestination,
) ([]byte, os.FileMode, error) {
	if !destination.isRooted() {
		return nil, 0, errors.New("mutation destination is invalid")
	}
	capability, err := authority.acquire(destination)
	if err != nil {
		return nil, 0, err
	}
	defer capability.Close()
	content, mode, _, err := authority.filesystem.ReadRootedRegularFile(ctx, capability)
	return content, mode, err
}

func destinationEntryExists(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationDestination,
) (bool, error) {
	if !destination.isRooted() {
		return false, errors.New("mutation destination is invalid")
	}
	capability, err := authority.acquire(destination)
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
	destination mutationDestination,
	expected mutationfs.EntryIdentity,
) error {
	if !destination.isRooted() {
		return errors.New("mutation destination is invalid")
	}
	capability, err := authority.acquire(destination)
	if err != nil {
		return err
	}
	return authority.filesystem.RemoveRootedEntry(ctx, capability, expected)
}

func removeManagedPathDestination(
	ctx context.Context,
	destination mutationDestination,
	precondition *managedPathPrecondition,
) error {
	if precondition == nil || !precondition.existed || precondition.destination.logical != destination.logical {
		return fmt.Errorf("managed removal requires a matching existing precondition for %q", destination.logical)
	}
	if !destination.isRooted() {
		return errors.New("mutation destination is invalid")
	}
	capability, err := precondition.takeRootedCapability()
	if err != nil {
		return err
	}
	return precondition.filesystem.RemoveRootedEntry(ctx, capability, precondition.identity)
}

func commitManagedFileDestination(
	ctx context.Context,
	destination mutationDestination,
	content []byte,
	fileMode os.FileMode,
	precondition *managedPathPrecondition,
) error {
	if precondition == nil || precondition.destination.logical != destination.logical {
		return fmt.Errorf("managed file precondition does not match destination %q", destination.logical)
	}
	if !destination.isRooted() {
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
