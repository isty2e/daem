//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"

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
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(phaseValidate, path, fmt.Errorf("directory snapshot maximum entries must be positive"))
	}
	return snapshotDirectoryWithNames(ctx, path, maximumEntries, capability, faults, readDirectoryNames)
}

// SnapshotRootedDirectoryBatch returns a stable bounded prefix, not an exhaustive
// inventory. more means coverage is incomplete. A zero bound observes only root
// metadata, without enumerating children, and always reports incomplete coverage.
// The complete snapshot API retains its positive-bound and overflow refusals.
func SnapshotRootedDirectoryBatch(ctx context.Context, capability rootedpath.CommitCapability, maximumEntries int) (mutationfs.DirectorySnapshot, bool, error) {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return mutationfs.DirectorySnapshot{}, false, err
	}
	more, observed := false, false
	read := func(ctx context.Context, fd int, path string, limit int) ([]string, error) {
		if limit == 0 {
			more = true
			return nil, nil
		}
		names, extra, err := readDirectoryNamePrefix(ctx, fd, path, limit)
		if err != nil {
			return nil, err
		}
		if observed && more != extra {
			return nil, fmt.Errorf("directory batch coverage changed while reading")
		}
		more, observed = extra, true
		return names, nil
	}
	snapshot, err := snapshotDirectoryWithNames(ctx, path, maximumEntries, capability, faultPlan{}, read)
	return snapshot, more, err
}

func readDirectoryNamePrefix(ctx context.Context, fd int, path string, limit int) ([]string, bool, error) {
	if limit < 0 || limit+1 <= 0 {
		return nil, false, fmt.Errorf("directory batch bound cannot be represented")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	duplicate, err := unix.Dup(fd)
	if err != nil {
		return nil, false, err
	}
	if _, err := unix.Seek(duplicate, 0, 0); err != nil {
		_ = unix.Close(duplicate)
		return nil, false, err
	}
	directory := os.NewFile(uintptr(duplicate), path)
	if directory == nil {
		_ = unix.Close(duplicate)
		return nil, false, fmt.Errorf("wrap directory batch descriptor")
	}
	names, more, readErr := readDirectoryPrefix(ctx, directory.Readdirnames, limit)
	closeErr := directory.Close()
	if readErr != nil {
		return nil, false, readErr
	}
	if closeErr != nil {
		return nil, false, closeErr
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	return names, more, nil
}

func readDirectoryPrefix(ctx context.Context, read func(int) ([]string, error), limit int) ([]string, bool, error) {
	if ctx == nil || limit < 0 || limit+1 <= 0 {
		return nil, false, fmt.Errorf("directory prefix requires context and a representable bound")
	}
	names := make([]string, 0, min(limit, 256))
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		batch, err := read(min(256, limit+1-len(names)))
		names = append(names, batch...)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, false, err
		}
		if len(names) > limit {
			names = names[:limit]
			sort.Strings(names)
			return names, true, nil
		}
		if errors.Is(err, io.EOF) {
			sort.Strings(names)
			return names, false, nil
		}
		if len(batch) == 0 {
			return nil, false, fmt.Errorf("directory prefix enumeration made no progress")
		}
	}
}

func snapshotDirectoryWithNames(
	ctx context.Context,
	path string,
	maximumEntries int,
	capability rootedpath.CommitCapability,
	faults faultPlan,
	readNames func(context.Context, int, string, int) ([]string, error),
) (mutationfs.DirectorySnapshot, error) {
	if maximumEntries < 0 {
		return mutationfs.DirectorySnapshot{}, failureBeforeVisibility(
			phaseValidate,
			path,
			fmt.Errorf("directory batch maximum entries must not be negative"),
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

	names, err := readNames(ctx, directoryFD, path, maximumEntries)
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

	currentNames, err := readNames(ctx, directoryFD, path, maximumEntries)
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
