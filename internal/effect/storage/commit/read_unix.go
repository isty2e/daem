//go:build darwin || linux

package commit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

const maximumSymlinkTargetBytes = 1 << 20

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

// ReadRootedSymlinkTarget returns one identity-stable, no-follow symbolic-link
// target without consuming the supplied capability.
func ReadRootedSymlinkTarget(
	ctx context.Context,
	capability rootedpath.CommitCapability,
) (string, EntryIdentity, error) {
	if err := ctx.Err(); err != nil {
		return "", EntryIdentity{}, err
	}
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return "", EntryIdentity{}, err
	}
	if err := validateRootedPath(path); err != nil {
		return "", EntryIdentity{}, failureBeforeVisibility(phaseValidate, path, err)
	}
	anchor, err := openCommitParent(path, capability, false)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return "", EntryIdentity{}, failureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	before, _, err := anchor.observe(anchor.base, path)
	if err != nil {
		return "", EntryIdentity{}, failureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	if before.kind != entryKindSymlink {
		return "", EntryIdentity{}, failureBeforeVisibility(
			phaseValidate,
			path,
			fmt.Errorf("entry is not a symbolic link"),
		)
	}
	target, err := readRootedSymlinkAt(ctx, anchor.parentFD(), anchor.base)
	if err != nil {
		return "", EntryIdentity{}, failureBeforeVisibility(phaseReadPayload, path, err)
	}
	if err := ctx.Err(); err != nil {
		return "", EntryIdentity{}, err
	}
	after, _, err := anchor.observe(anchor.base, path)
	if err != nil {
		return "", EntryIdentity{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	if !before.sameEntry(after) {
		return "", EntryIdentity{}, failureBeforeVisibility(
			phaseRevalidateEntry,
			path,
			fmt.Errorf("symbolic link changed while reading target"),
		)
	}
	return target, before, nil
}

func readRootedSymlinkAt(ctx context.Context, parentFD int, name string) (string, error) {
	for size := 256; size <= maximumSymlinkTargetBytes; size *= 2 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		buffer := make([]byte, size)
		count, err := unix.Readlinkat(parentFD, name, buffer)
		if err != nil {
			return "", err
		}
		if count < len(buffer) {
			return string(buffer[:count]), nil
		}
	}
	return "", errors.New("symbolic link target exceeds 1 MiB")
}

func readRegularFileSnapshotWithFaults(
	ctx context.Context,
	path string,
	capability rootedpath.CommitCapability,
	maximumBytes int64,
	faults faultPlan,
) ([]byte, fs.FileMode, EntryIdentity, error) {
	var validationErr error
	if capability == nil {
		validationErr = validateCommitPath(path)
	} else {
		validationErr = validateRootedPath(path)
	}
	if validationErr != nil {
		return nil, 0, EntryIdentity{}, newFailure(failureUncommitted, phaseValidate, path, validationErr)
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
