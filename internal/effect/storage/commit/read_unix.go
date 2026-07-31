//go:build darwin || linux

package commit

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

// ReadRegularFile returns one identity-stable, no-follow regular-file snapshot.
func ReadRegularFile(ctx context.Context, path string) ([]byte, fs.FileMode, error) {
	snapshot, err := ReadRegularFileSnapshot(ctx, path)
	if err != nil {
		return nil, 0, err
	}
	return snapshot.Content(), snapshot.Mode(), nil
}

// ReadRegularFileSnapshot returns content and mode from the same
// identity-stable, no-follow read.
func ReadRegularFileSnapshot(ctx context.Context, path string) (mutationfs.RegularFileSnapshot, error) {
	content, mode, identity, err := readRegularFileSnapshotWithFaults(ctx, path, nil, 0, faultPlan{})
	if err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	return newRegularFileSnapshot(content, mode, identity)
}

// ReadRegularFileSnapshotUpTo returns an identity-stable, no-follow snapshot
// while refusing to retain more than maximumBytes of payload content.
func ReadRegularFileSnapshotUpTo(
	ctx context.Context,
	path string,
	maximumBytes int64,
) (mutationfs.RegularFileSnapshot, error) {
	if maximumBytes <= 0 {
		return mutationfs.RegularFileSnapshot{}, fmt.Errorf("regular file snapshot maximum bytes must be positive")
	}
	content, mode, identity, err := readRegularFileSnapshotWithFaults(ctx, path, nil, maximumBytes, faultPlan{})
	if err != nil {
		return mutationfs.RegularFileSnapshot{}, err
	}
	return newRegularFileSnapshot(content, mode, identity)
}

// ReadRootedRegularFile returns content, mode, and identity from one rooted
// no-follow snapshot. It does not consume capability; the caller must pass the
// same capability to the following replacement or close it on every exit.
func ReadRootedRegularFile(
	ctx context.Context,
	capability rootedpath.CommitCapability,
) ([]byte, fs.FileMode, EntryIdentity, error) {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return nil, 0, EntryIdentity{}, err
	}
	return readRegularFileSnapshotWithFaults(ctx, path, capability, 0, faultPlan{})
}

// ReadRootedRegularFileUpTo returns one rooted no-follow snapshot while
// refusing to retain more than maximumBytes of payload content. It does not
// consume capability.
func ReadRootedRegularFileUpTo(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	maximumBytes int64,
) ([]byte, fs.FileMode, EntryIdentity, error) {
	if maximumBytes <= 0 {
		return nil, 0, EntryIdentity{}, fmt.Errorf("rooted regular file maximum bytes must be positive")
	}
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return nil, 0, EntryIdentity{}, err
	}
	return readRegularFileSnapshotWithFaults(ctx, path, capability, maximumBytes, faultPlan{})
}

func readRegularFileSnapshotWithFaults(
	ctx context.Context,
	path string,
	capability rootedpath.CommitCapability,
	maximumBytes int64,
	faults faultPlan,
) ([]byte, fs.FileMode, EntryIdentity, error) {
	if err := validateCommitPath(path); err != nil {
		return nil, 0, EntryIdentity{}, newFailure(failureUncommitted, phaseValidate, path, err)
	}
	if err := faults.check(ctx, phaseValidate); err != nil {
		return nil, 0, EntryIdentity{}, newFailure(failureUncommitted, phaseValidate, path, err)
	}
	anchor, err := openCommitParent(path, capability, false)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return nil, 0, EntryIdentity{}, failureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	expected, stat, err := anchor.observe(anchor.base, path)
	if err != nil {
		return nil, 0, EntryIdentity{}, failureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	if capability != nil && expected.kind == entryKindSymlink {
		return nil, 0, EntryIdentity{}, failureBeforeVisibility(
			phaseCaptureIdentity,
			path,
			rootedFinalSymlinkFailure(path),
		)
	}
	if expected.kind != entryKindRegular {
		return nil, 0, EntryIdentity{}, failureBeforeVisibility(phaseValidate, path, fmt.Errorf("entry is not a regular file"))
	}
	if err := validateOwnedStat(path, &stat); err != nil {
		return nil, 0, EntryIdentity{}, failureBeforeVisibility(phaseValidate, path, err)
	}
	if maximumBytes > 0 && stat.Size > maximumBytes {
		return nil, 0, EntryIdentity{}, failureBeforeVisibility(
			phaseReadPayload,
			path,
			fmt.Errorf("regular file exceeds %d bytes", maximumBytes),
		)
	}
	fd, _, err := anchor.openExpected(anchor.base, path, expected)
	if err != nil {
		return nil, 0, EntryIdentity{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	defer unix.Close(fd)
	if capability != nil {
		if err := capability.ValidateDirectoryHandle(uintptr(fd)); err != nil {
			return nil, 0, EntryIdentity{}, failureBeforeVisibility(phaseValidate, path, err)
		}
	}

	var content bytes.Buffer
	buffer := make([]byte, 32*1024)
	for {
		if err := faults.check(ctx, phaseReadPayload); err != nil {
			return nil, 0, EntryIdentity{}, failureBeforeVisibility(phaseReadPayload, path, err)
		}
		count, readErr := unix.Read(fd, buffer)
		if count > 0 {
			if maximumBytes > 0 && int64(content.Len())+int64(count) > maximumBytes {
				return nil, 0, EntryIdentity{}, failureBeforeVisibility(
					phaseReadPayload,
					path,
					fmt.Errorf("regular file exceeds %d bytes", maximumBytes),
				)
			}
			_, _ = content.Write(buffer[:count])
		}
		if readErr == unix.EINTR {
			continue
		}
		if readErr != nil {
			return nil, 0, EntryIdentity{}, failureBeforeVisibility(phaseReadPayload, path, readErr)
		}
		if count == 0 {
			break
		}
	}
	if err := faults.check(ctx, phaseRevalidateEntry); err != nil {
		return nil, 0, EntryIdentity{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	observed, _, err := anchor.requireExpected(anchor.base, path, expected)
	if err != nil || !expected.sameEntry(observed) {
		if err == nil {
			err = fmt.Errorf("entry identity changed while reading")
		}
		return nil, 0, EntryIdentity{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	if err := anchor.verifyChain(); err != nil {
		return nil, 0, EntryIdentity{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	var final unix.Stat_t
	if err := unix.Fstat(fd, &final); err != nil {
		return nil, 0, EntryIdentity{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	if err := validateOwnedStat(path, &final); err != nil {
		return nil, 0, EntryIdentity{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	return content.Bytes(), fs.FileMode(final.Mode).Perm(), expected, nil
}

func newRegularFileSnapshot(
	content []byte,
	mode fs.FileMode,
	identity EntryIdentity,
) (mutationfs.RegularFileSnapshot, error) {
	return mutationfs.NewRegularFileSnapshot(content, mode, identity)
}
