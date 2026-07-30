//go:build darwin || linux

package commit

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"golang.org/x/sys/unix"
)

// SnapshotDirectory returns a stable no-follow inventory of immediate children.
// It does not traverse children or interpret their names.
func SnapshotDirectory(
	ctx context.Context,
	path string,
) (mutationfs.DirectorySnapshot, error) {
	return snapshotDirectoryWithFaults(ctx, path, faultPlan{})
}

func snapshotDirectoryWithFaults(
	ctx context.Context,
	path string,
	faults faultPlan,
) (mutationfs.DirectorySnapshot, error) {
	if err := validateCommitPath(path); err != nil {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseValidate, path, err)
	}
	if err := faults.check(ctx, phaseValidate); err != nil {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseValidate, path, err)
	}
	anchor, err := openCommitParent(path, nil, false)
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

	names, err := readDirectoryNames(directoryFD, path)
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

	currentNames, err := readDirectoryNames(directoryFD, path)
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
