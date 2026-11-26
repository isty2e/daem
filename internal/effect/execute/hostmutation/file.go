package hostmutation

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func (operation *Operation) CommitFileAgainst(ctx context.Context, destination mutationnamespace.Destination, content []byte, fileMode os.FileMode, expectedExists bool, expected mutationfs.EntryIdentity) (mutationfs.EntryIdentity, error) {
	if !destination.IsRooted() {
		return nil, errors.New("mutation destination is invalid")
	}
	if expectedExists != (expected != nil) {
		return nil, errors.New("file publication requires the identity of an existing destination")
	}
	capability, err := operation.destinations.Acquire(destination)
	if err != nil {
		return nil, err
	}
	return operation.filesystem.PublishRootedFile(ctx, capability, content, fileMode, expected)
}

func (operation *Operation) MutateFile(
	ctx context.Context,
	destination mutationnamespace.Destination,
	fileMode os.FileMode,
	allowMissing bool,
	maximumBytes int64,
	mutation func(existing []byte, mode os.FileMode, identity mutationfs.EntryIdentity) (content []byte, keepFile bool, err error),
) (mutationfs.EntryIdentity, error) {
	if !destination.IsRooted() {
		return nil, errors.New("mutation destination is invalid")
	}
	capability, err := operation.destinations.Acquire(destination)
	if err != nil {
		return nil, err
	}
	existing, mode, expected, err := operation.filesystem.ReadRootedRegularFileUpTo(ctx, capability, maximumBytes)
	exists := err == nil
	if err != nil && !(allowMissing && errors.Is(err, os.ErrNotExist)) {
		_ = capability.Close()
		return nil, err
	}
	if exists && expected == nil {
		return nil, errors.Join(errors.New("existing file has no entry identity"), capability.Close())
	}
	if !exists {
		expected = nil
	}
	content, keepFile, err := mutation(existing, mode, expected)
	if err != nil {
		_ = capability.Close()
		return nil, err
	}
	if !keepFile {
		if !exists {
			_ = capability.Close()
			return nil, nil
		}
		_, err := operation.removals.RemoveRootedEntry(ctx, destination, capability, expected, artifact.InstalledFileHash(existing))
		return nil, err
	}
	if exists {
		fileMode = mode.Perm()
	}
	return operation.filesystem.PublishRootedFile(ctx, capability, content, fileMode, expected)
}

func (operation *Operation) readFile(ctx context.Context, destination mutationnamespace.Destination, maximumBytes int64) ([]byte, os.FileMode, error) {
	if !destination.IsRooted() {
		return nil, 0, errors.New("mutation destination is invalid")
	}
	capability, err := operation.destinations.Acquire(destination)
	if err != nil {
		return nil, 0, err
	}
	defer capability.Close()
	content, mode, _, err := operation.filesystem.ReadRootedRegularFileUpTo(ctx, capability, maximumBytes)
	return content, mode, err
}

func (operation *Operation) entryExists(ctx context.Context, destination mutationnamespace.Destination) (bool, error) {
	if !destination.IsRooted() {
		return false, errors.New("mutation destination is invalid")
	}
	capability, err := operation.destinations.Acquire(destination)
	if err != nil {
		return false, err
	}
	defer capability.Close()
	if _, err := operation.filesystem.CaptureRootedEntryIdentity(ctx, capability); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (operation *Operation) RemoveAgainst(ctx context.Context, destination mutationnamespace.Destination, expected mutationfs.EntryIdentity, fileHash artifact.ContentHash) error {
	if !destination.IsRooted() {
		return errors.New("mutation destination is invalid")
	}
	capability, err := operation.destinations.Acquire(destination)
	if err != nil {
		return err
	}
	_, err = operation.removals.RemoveRootedEntry(ctx, destination, capability, expected, fileHash)
	return err
}

func (path PreparedPath) Remove(ctx context.Context) error {
	precondition := path.state
	if precondition == nil || !precondition.existed {
		return fmt.Errorf("managed removal requires a matching existing precondition")
	}
	destination := precondition.destination
	if !destination.IsRooted() {
		return errors.New("mutation destination is invalid")
	}
	capability, err := precondition.takeRootedCapability()
	if err != nil {
		return err
	}
	_, err = precondition.operation.removals.RemoveRootedEntry(ctx, destination, capability, precondition.identity, precondition.hash)
	return err
}

func (path PreparedPath) WriteFile(ctx context.Context, content []byte, fileMode os.FileMode) error {
	precondition := path.state
	if precondition == nil {
		return fmt.Errorf("managed file precondition is unavailable")
	}
	if !precondition.destination.IsRooted() {
		return errors.New("mutation destination is invalid")
	}
	if precondition.existed && precondition.hash.IsInstalled() {
		identity, err := precondition.filesystem.CaptureRootedEntryIdentity(ctx, precondition.capability)
		if err != nil {
			return err
		}
		if !precondition.identity.Equal(identity) {
			live, mode, current, err := precondition.filesystem.ReadRootedRegularFile(ctx, precondition.capability)
			if err != nil {
				return err
			}
			if !mutationfs.SameEntryObject(precondition.identity, current) || !precondition.hash.MatchesFile(live) {
				return fmt.Errorf("managed file %q changed while preparing publication", precondition.destination.Logical())
			}
			precondition.identity, precondition.fileMode = current, mode.Perm()
		}
		fileMode = precondition.fileMode
	}
	capability, err := precondition.takeRootedCapability()
	if err != nil {
		return err
	}
	if !precondition.existed {
		return precondition.filesystem.CreateRootedFile(ctx, capability, content, fileMode)
	}
	return precondition.filesystem.ReplaceRootedFile(ctx, capability, content, fileMode, precondition.identity)
}
