//go:build darwin || linux

package commit

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

// SnapshotDirectory returns a stable no-follow inventory of immediate children.
// It does not traverse children or interpret their names.
func SnapshotDirectory(
	ctx context.Context,
	path string,
	maximumEntries int,
) (mutationfs.DirectorySnapshot, error) {
	return snapshotDirectoryWithFaults(ctx, path, maximumEntries, faultPlan{})
}

// SnapshotRootedDirectoryEntries returns a stable immediate-child inventory
// through exact retained-root authority. Unlike the unrooted path API, the
// selected final name may belong to a reserved storage namespace.
func SnapshotRootedDirectoryEntries(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	maximumEntries int,
) (mutationfs.DirectorySnapshot, error) {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return mutationfs.DirectorySnapshot{}, err
	}
	return snapshotDirectoryWithCapability(
		ctx,
		path,
		maximumEntries,
		capability,
		faultPlan{},
	)
}

func snapshotDirectoryWithFaults(
	ctx context.Context,
	path string,
	maximumEntries int,
	faults faultPlan,
) (mutationfs.DirectorySnapshot, error) {
	return snapshotDirectoryWithCapability(
		ctx,
		path,
		maximumEntries,
		nil,
		faults,
	)
}

func snapshotDirectoryWithCapability(
	ctx context.Context,
	path string,
	maximumEntries int,
	capability rootedpath.CommitCapability,
	faults faultPlan,
) (mutationfs.DirectorySnapshot, error) {
	if maximumEntries <= 0 {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(
			phaseValidate,
			path,
			fmt.Errorf("directory snapshot maximum entries must be positive"),
		)
	}
	if capability == nil {
		if err := validateCommitPath(path); err != nil {
			return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseValidate, path, err)
		}
	} else if err := validateRootedCapability(path, capability); err != nil {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseValidate, path, err)
	}
	if err := faults.check(ctx, phaseValidate); err != nil {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseValidate, path, err)
	}
	anchor, err := openReadParent(path, capability)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	expectedRoot, rootStat, err := anchor.observe(anchor.base, path)
	if err != nil {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	if expectedRoot.kind != entryKindDirectory {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(
			phaseValidate,
			path,
			fmt.Errorf("entry is not a directory"),
		)
	}
	if err := validateOwnedStat(path, &rootStat); err != nil {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseValidate, path, err)
	}
	directoryFD, _, err := anchor.openExpected(anchor.base, path, expectedRoot)
	if err != nil {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	defer unix.Close(directoryFD)
	if capability != nil {
		if err := capability.ValidateDirectoryHandle(uintptr(directoryFD)); err != nil {
			return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(
				phaseValidate,
				path,
				err,
			)
		}
	}

	names, err := readDirectoryNames(ctx, directoryFD, path, maximumEntries)
	if err != nil {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseReadPayload, path, err)
	}
	entries := make([]mutationfs.DirectoryEntrySnapshot, 0, len(names))
	expectedEntries := make(map[string]EntryIdentity, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseReadPayload, path, err)
		}
		entryPath := filepath.Join(path, name)
		identity, stat, err := observeAnyAt(directoryFD, name, entryPath)
		if err != nil {
			return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseReadPayload, entryPath, err)
		}
		snapshot, err := mutationfs.NewDirectoryEntrySnapshot(
			name,
			identity,
			fs.FileMode(stat.Mode).Perm(),
			int(stat.Uid) == unix.Geteuid(),
			stat.Size,
		)
		if err != nil {
			return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseReadPayload, entryPath, err)
		}
		entries = append(entries, snapshot)
		expectedEntries[name] = identity
	}
	if err := faults.check(ctx, phaseReadPayload); err != nil {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseReadPayload, path, err)
	}

	currentNames, err := readDirectoryNames(ctx, directoryFD, path, maximumEntries)
	if err != nil {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	if !slices.Equal(names, currentNames) {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(
			phaseRevalidateEntry,
			path,
			fmt.Errorf("directory entries changed while snapshotting"),
		)
	}
	if err := faults.check(ctx, phaseRevalidateEntry); err != nil {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	for _, name := range names {
		entryPath := filepath.Join(path, name)
		current, _, err := observeAnyAt(directoryFD, name, entryPath)
		if err != nil {
			return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseRevalidateEntry, entryPath, err)
		}
		if !expectedEntries[name].sameEntry(current) {
			return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(
				phaseRevalidateEntry,
				entryPath,
				fmt.Errorf("directory entry identity changed while snapshotting"),
			)
		}
	}
	if err := verifySnapshotEntry(
		anchor.parentFD(),
		anchor.base,
		directoryFD,
		path,
		expectedRoot,
	); err != nil {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	if err := anchor.verifyChain(); err != nil {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}

	snapshot, err := mutationfs.NewDirectorySnapshot(
		expectedRoot,
		fs.FileMode(rootStat.Mode).Perm(),
		true,
		entries,
	)
	if err != nil {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseValidate, path, err)
	}
	return snapshot, nil
}
