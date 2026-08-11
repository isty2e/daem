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

type removalTreeSnapshotEntry struct {
	name     string
	identity EntryIdentity
	mode     fs.FileMode
	size     int64
	children []removalTreeSnapshotEntry
}

type removalTreeEffectState struct {
	changed bool
}

func captureRemovalTreeSnapshot(
	ctx context.Context,
	parentFD int,
	name string,
	path string,
	expected EntryIdentity,
	capability rootedpath.CommitCapability,
	limits mutationfs.TreeTraversalLimits,
) (removalTreeSnapshotEntry, func(uintptr) error, error) {
	budget, err := newTreeTraversalBudget(limits)
	if err != nil {
		return removalTreeSnapshotEntry{}, nil, err
	}
	observed, stat, err := observeAt(parentFD, name, path)
	if err != nil {
		return removalTreeSnapshotEntry{}, nil, err
	}
	if !expected.sameEntry(observed) {
		return removalTreeSnapshotEntry{}, nil, fmt.Errorf("entry identity changed before cleanup at %q", path)
	}
	if err := validateOwnedStat(path, &stat); err != nil {
		return removalTreeSnapshotEntry{}, nil, err
	}

	root := removalTreeSnapshotEntry{
		name:     name,
		identity: observed,
		mode:     fs.FileMode(stat.Mode).Perm(),
	}
	var validateMount func(uintptr) error
	switch observed.kind {
	case entryKindRegular:
		if err := budget.admitBytes(stat.Size); err != nil {
			return removalTreeSnapshotEntry{}, nil, err
		}
		root.size = stat.Size
		fd, err := openExpectedAt(parentFD, name, path, observed)
		if err != nil {
			return removalTreeSnapshotEntry{}, nil, err
		}
		validateMount, err = removalMountValidator(capability, fd)
		if err == nil {
			err = validateMount(uintptr(fd))
		}
		closeErr := unix.Close(fd)
		if err != nil {
			return removalTreeSnapshotEntry{}, nil, err
		}
		if closeErr != nil {
			return removalTreeSnapshotEntry{}, nil, closeErr
		}
	case entryKindDirectory:
		fd, err := openExpectedAt(parentFD, name, path, observed)
		if err != nil {
			return removalTreeSnapshotEntry{}, nil, err
		}
		validateMount, err = removalMountValidator(capability, fd)
		if err == nil {
			err = captureRemovalDirectorySnapshot(ctx, fd, path, 0, &root, validateMount, budget)
		}
		closeErr := unix.Close(fd)
		if err != nil {
			return removalTreeSnapshotEntry{}, nil, err
		}
		if closeErr != nil {
			return removalTreeSnapshotEntry{}, nil, closeErr
		}
	case entryKindSymlink:
		// A no-follow unlink removes only the link itself.
	default:
		return removalTreeSnapshotEntry{}, nil, unsupported(
			fmt.Sprintf("cannot safely remove special entry %q", path),
			nil,
		)
	}
	if err := verifyRemovalSnapshotEntryAt(parentFD, path, root); err != nil {
		return removalTreeSnapshotEntry{}, nil, err
	}
	return root, validateMount, nil
}

func captureRemovalDirectorySnapshot(
	ctx context.Context,
	directoryFD int,
	directoryPath string,
	depth int,
	entry *removalTreeSnapshotEntry,
	validateMount func(uintptr) error,
	budget *treeTraversalBudget,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateMount(uintptr(directoryFD)); err != nil {
		return err
	}
	if err := budget.admitDepth(depth); err != nil {
		return err
	}
	if err := verifyOpenedRemovalSnapshotEntry(directoryFD, directoryPath, *entry); err != nil {
		return err
	}
	names, err := readDirectoryNames(ctx, directoryFD, directoryPath, budget.remainingEntries())
	if err != nil {
		return err
	}
	if err := budget.admitEntries(len(names)); err != nil {
		return err
	}
	entry.children = make([]removalTreeSnapshotEntry, 0, len(names))
	for _, name := range names {
		childPath := filepath.Join(directoryPath, name)
		identity, stat, err := observeAnyAt(directoryFD, name, childPath)
		if err != nil {
			return err
		}
		if err := validateOwnedStat(childPath, &stat); err != nil {
			return err
		}
		child := removalTreeSnapshotEntry{
			name:     name,
			identity: identity,
			mode:     fs.FileMode(stat.Mode).Perm(),
		}
		switch identity.kind {
		case entryKindRegular:
			if err := budget.admitBytes(stat.Size); err != nil {
				return err
			}
			child.size = stat.Size
			fd, err := openExpectedAt(directoryFD, name, childPath, identity)
			if err != nil {
				return err
			}
			if err := validateMount(uintptr(fd)); err != nil {
				_ = unix.Close(fd)
				return err
			}
			closeErr := unix.Close(fd)
			if closeErr != nil {
				return closeErr
			}
		case entryKindDirectory:
			if err := budget.admitDepth(depth + 1); err != nil {
				return err
			}
			fd, err := openExpectedAt(directoryFD, name, childPath, identity)
			if err != nil {
				return err
			}
			err = captureRemovalDirectorySnapshot(
				ctx,
				fd,
				childPath,
				depth+1,
				&child,
				validateMount,
				budget,
			)
			closeErr := unix.Close(fd)
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		case entryKindSymlink:
			// Symlink targets are never followed during cleanup.
		default:
			return unsupported(
				fmt.Sprintf("cannot safely remove special entry %q", childPath),
				nil,
			)
		}
		entry.children = append(entry.children, child)
	}
	if err := verifyOpenedRemovalSnapshotEntry(directoryFD, directoryPath, *entry); err != nil {
		return err
	}
	return verifyRemovalDirectorySnapshot(ctx, directoryFD, directoryPath, *entry)
}

func removeRemovalTreeSnapshot(
	ctx context.Context,
	parentFD int,
	path string,
	entry *removalTreeSnapshotEntry,
	validateMount func(uintptr) error,
	limits mutationfs.TreeTraversalLimits,
	faults faultPlan,
) (bool, error) {
	budget, err := newTreeTraversalBudget(limits)
	if err != nil {
		return false, err
	}
	state := &removalTreeEffectState{}
	err = removeRemovalSnapshotEntry(
		ctx,
		parentFD,
		path,
		entry,
		validateMount,
		0,
		budget,
		faults,
		state,
	)
	return state.changed, err
}

func removeRemovalSnapshotEntry(
	ctx context.Context,
	parentFD int,
	path string,
	entry *removalTreeSnapshotEntry,
	validateMount func(uintptr) error,
	depth int,
	budget *treeTraversalBudget,
	faults faultPlan,
	state *removalTreeEffectState,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch entry.identity.kind {
	case entryKindRegular:
		return removeRemovalSnapshotFile(
			ctx,
			parentFD,
			path,
			*entry,
			validateMount,
			budget,
			faults,
			state,
		)
	case entryKindSymlink:
		return removeRemovalSnapshotSymlink(ctx, parentFD, path, *entry, faults, state)
	case entryKindDirectory:
		return removeRemovalSnapshotDirectory(
			ctx,
			parentFD,
			path,
			entry,
			validateMount,
			depth,
			budget,
			faults,
			state,
		)
	default:
		return unsupported(fmt.Sprintf("cannot safely remove special entry %q", path), nil)
	}
}

func removeRemovalSnapshotFile(
	ctx context.Context,
	parentFD int,
	path string,
	entry removalTreeSnapshotEntry,
	validateMount func(uintptr) error,
	budget *treeTraversalBudget,
	faults faultPlan,
	state *removalTreeEffectState,
) error {
	if err := budget.admitBytes(entry.size); err != nil {
		return err
	}
	fd, err := openExpectedAt(parentFD, entry.name, path, entry.identity)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if validateMount == nil {
		return fmt.Errorf("removal mount validator is required")
	}
	if err := validateMount(uintptr(fd)); err != nil {
		return err
	}
	if err := faults.check(ctx, phaseCleanupEntry); err != nil {
		return err
	}
	if err := verifyRemovalEntryBinding(parentFD, path, fd, entry); err != nil {
		return err
	}
	if err := unix.Unlinkat(parentFD, entry.name, 0); err != nil {
		return err
	}
	state.changed = true
	return nil
}

func removeRemovalSnapshotSymlink(
	ctx context.Context,
	parentFD int,
	path string,
	entry removalTreeSnapshotEntry,
	faults faultPlan,
	state *removalTreeEffectState,
) error {
	if err := faults.check(ctx, phaseCleanupEntry); err != nil {
		return err
	}
	if err := verifyRemovalSnapshotEntryAt(parentFD, path, entry); err != nil {
		return err
	}
	if err := unix.Unlinkat(parentFD, entry.name, 0); err != nil {
		return err
	}
	state.changed = true
	return nil
}

func removeRemovalSnapshotDirectory(
	ctx context.Context,
	parentFD int,
	path string,
	entry *removalTreeSnapshotEntry,
	validateMount func(uintptr) error,
	depth int,
	budget *treeTraversalBudget,
	faults faultPlan,
	state *removalTreeEffectState,
) error {
	if err := budget.admitDepth(depth); err != nil {
		return err
	}
	fd, err := openExpectedAt(parentFD, entry.name, path, entry.identity)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if validateMount == nil {
		return fmt.Errorf("removal mount validator is required")
	}
	if err := validateMount(uintptr(fd)); err != nil {
		return err
	}
	if err := prepareOpenedRemovalDirectory(
		ctx,
		parentFD,
		path,
		fd,
		entry,
		faults,
		state,
	); err != nil {
		return err
	}
	if err := faults.check(ctx, phaseRevalidateCleanup); err != nil {
		return err
	}
	if err := verifyRemovalEntryBinding(parentFD, path, fd, *entry); err != nil {
		return err
	}
	if err := budget.admitEntries(len(entry.children)); err != nil {
		return err
	}
	if err := verifyRemovalDirectorySnapshot(ctx, fd, path, *entry); err != nil {
		return err
	}
	for index := range entry.children {
		child := &entry.children[index]
		childPath := filepath.Join(path, child.name)
		if err := removeRemovalSnapshotEntry(
			ctx,
			fd,
			childPath,
			child,
			validateMount,
			depth+1,
			budget,
			faults,
			state,
		); err != nil {
			return err
		}
	}
	if err := faults.check(ctx, phaseCleanupEntry); err != nil {
		return err
	}
	if err := refreshRemovalDirectoryBinding(parentFD, path, fd, entry); err != nil {
		return err
	}
	names, err := readDirectoryNames(ctx, fd, path, 0)
	if err != nil {
		return err
	}
	if len(names) != 0 {
		return fmt.Errorf("directory entries changed before cleanup at %q", path)
	}
	if err := unix.Unlinkat(parentFD, entry.name, unix.AT_REMOVEDIR); err != nil {
		return err
	}
	state.changed = true
	return nil
}

func prepareOpenedRemovalDirectory(
	ctx context.Context,
	parentFD int,
	path string,
	fd int,
	entry *removalTreeSnapshotEntry,
	faults faultPlan,
	state *removalTreeEffectState,
) error {
	if entry.mode&0o700 == 0o700 {
		return verifyRemovalEntryBinding(parentFD, path, fd, *entry)
	}
	if err := verifyRemovalEntryBinding(parentFD, path, fd, *entry); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	if err := faults.check(ctx, phaseApplyMode); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	mode := uint32(entry.mode.Perm()) | 0o700
	if err := unix.Fchmod(fd, mode); err != nil {
		return atPhase(phaseApplyMode, fmt.Errorf("make retired directory removable at %q: %w", path, err))
	}
	state.changed = true
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	current := identityFromStat(path, &stat)
	if !entry.identity.sameObject(current) || current.kind != entryKindDirectory {
		return atPhase(phaseApplyMode, fmt.Errorf("retired directory identity changed while preparing cleanup at %q", path))
	}
	if err := validateOwnedStat(path, &stat); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	if fs.FileMode(stat.Mode).Perm() != fs.FileMode(mode).Perm() {
		return atPhase(
			phaseApplyMode,
			unsupported(fmt.Sprintf("retired directory %q did not retain private cleanup mode", path), nil),
		)
	}
	entry.identity = current
	entry.mode = fs.FileMode(stat.Mode).Perm()
	if err := verifyRemovalEntryBinding(parentFD, path, fd, *entry); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	return nil
}

func verifyRemovalDirectorySnapshot(
	ctx context.Context,
	directoryFD int,
	directoryPath string,
	entry removalTreeSnapshotEntry,
) error {
	names, err := readDirectoryNames(ctx, directoryFD, directoryPath, len(entry.children))
	if err != nil {
		return err
	}
	expectedNames := make([]string, 0, len(entry.children))
	for _, child := range entry.children {
		expectedNames = append(expectedNames, child.name)
	}
	if !slices.Equal(names, expectedNames) {
		return fmt.Errorf("directory entries changed before cleanup at %q", directoryPath)
	}
	for index, name := range names {
		childPath := filepath.Join(directoryPath, name)
		current, stat, err := observeAnyAt(directoryFD, name, childPath)
		if err != nil {
			return err
		}
		expected := entry.children[index]
		if !expected.identity.sameEntry(current) ||
			fs.FileMode(stat.Mode).Perm() != expected.mode ||
			(current.kind == entryKindRegular && stat.Size != expected.size) {
			return fmt.Errorf("entry identity changed before cleanup at %q", childPath)
		}
		if err := validateOwnedStat(childPath, &stat); err != nil {
			return err
		}
	}
	return nil
}

func verifyRemovalEntryBinding(
	parentFD int,
	path string,
	fd int,
	entry removalTreeSnapshotEntry,
) error {
	if err := verifyRemovalSnapshotEntryAt(parentFD, path, entry); err != nil {
		return err
	}
	return verifyOpenedRemovalSnapshotEntry(fd, path, entry)
}

func verifyRemovalSnapshotEntryAt(
	parentFD int,
	path string,
	entry removalTreeSnapshotEntry,
) error {
	current, stat, err := observeAnyAt(parentFD, entry.name, path)
	if err != nil {
		return err
	}
	if !entry.identity.sameEntry(current) ||
		fs.FileMode(stat.Mode).Perm() != entry.mode ||
		(current.kind == entryKindRegular && stat.Size != entry.size) {
		return fmt.Errorf("entry identity changed before cleanup at %q", path)
	}
	return validateOwnedStat(path, &stat)
}

func verifyOpenedRemovalSnapshotEntry(
	fd int,
	path string,
	entry removalTreeSnapshotEntry,
) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	current := identityFromStat(path, &stat)
	if !entry.identity.sameEntry(current) || current.kind != entry.identity.kind ||
		fs.FileMode(stat.Mode).Perm() != entry.mode ||
		(current.kind == entryKindRegular && stat.Size != entry.size) {
		return fmt.Errorf("entry identity changed before cleanup at %q", path)
	}
	return validateOwnedStat(path, &stat)
}

func refreshRemovalDirectoryBinding(
	parentFD int,
	path string,
	fd int,
	entry *removalTreeSnapshotEntry,
) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	current := identityFromStat(path, &stat)
	if !entry.identity.sameObject(current) || current.kind != entryKindDirectory ||
		fs.FileMode(stat.Mode).Perm() != entry.mode {
		return fmt.Errorf("directory identity changed before cleanup at %q", path)
	}
	if err := validateOwnedStat(path, &stat); err != nil {
		return err
	}
	observed, observedStat, err := observeAnyAt(parentFD, entry.name, path)
	if err != nil {
		return err
	}
	if !current.sameEntry(observed) || fs.FileMode(observedStat.Mode).Perm() != entry.mode {
		return fmt.Errorf("directory identity changed before cleanup at %q", path)
	}
	entry.identity = current
	return nil
}
