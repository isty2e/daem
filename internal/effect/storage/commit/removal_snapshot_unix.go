//go:build darwin || linux

package commit

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

type removalTreeSnapshotEntry struct {
	name     string
	identity removalEntryIdentity
	mode     fs.FileMode
	size     int64
	children []removalTreeSnapshotEntry
}

type removalEntryIdentity struct {
	kind     entryKind
	platform platformIdentity
}

type removalDirectoryBinding struct {
	parentFD    int
	directoryFD int
	path        string
	entry       *removalTreeSnapshotEntry
}

type removalTreeEffectState struct {
	changed bool
}

type removalEffectAuthority struct {
	mount             rootedpath.DirectoryMountBoundary
	validateNamespace func() error
}

func newRemovalEffectAuthority(
	parentFD int,
	validateNamespace func() error,
) (removalEffectAuthority, error) {
	if validateNamespace == nil {
		return removalEffectAuthority{}, fmt.Errorf("removal namespace validator is required")
	}
	mount, err := rootedpath.CaptureDirectoryMountBoundary(uintptr(parentFD))
	if err != nil {
		return removalEffectAuthority{}, err
	}
	return removalEffectAuthority{
		mount:             mount,
		validateNamespace: validateNamespace,
	}, nil
}

func (authority removalEffectAuthority) validateDirectoryHandle(directoryFD int) error {
	return authority.mount.ValidateDirectoryHandle(uintptr(directoryFD))
}

func (authority removalEffectAuthority) validateEntryAt(parentFD int, name string) error {
	return authority.mount.ValidateEntryAt(uintptr(parentFD), name)
}

func newRemovalEntryIdentity(identity EntryIdentity) removalEntryIdentity {
	return removalEntryIdentity{
		kind:     identity.kind,
		platform: identity.platform,
	}
}

func (identity removalEntryIdentity) entryAt(path string) EntryIdentity {
	return EntryIdentity{
		path:     path,
		kind:     identity.kind,
		platform: identity.platform,
	}
}

func (identity removalEntryIdentity) sameEntry(other EntryIdentity) bool {
	return identity.kind != entryKindInvalid && other.valid() &&
		identity.kind == other.kind && identity.platform.matches(other.platform)
}

func (identity removalEntryIdentity) sameObject(other EntryIdentity) bool {
	return identity.kind != entryKindInvalid && other.valid() &&
		identity.kind == other.kind && identity.platform.sameObject(other.platform)
}

func captureRemovalTreeSnapshot(
	ctx context.Context,
	parentFD int,
	name string,
	path string,
	expected EntryIdentity,
	limits mutationfs.TreeTraversalLimits,
	validateNamespace func() error,
	faults faultPlan,
) (removalTreeSnapshotEntry, removalEffectAuthority, bool, error) {
	state := &removalTreeEffectState{}
	budget, err := newTreeTraversalBudget(limits)
	if err != nil {
		return removalTreeSnapshotEntry{}, removalEffectAuthority{}, false, err
	}
	authority, err := newRemovalEffectAuthority(parentFD, validateNamespace)
	if err != nil {
		return removalTreeSnapshotEntry{}, removalEffectAuthority{}, false, err
	}
	observed, stat, err := observeAt(parentFD, name, path)
	if err != nil {
		return removalTreeSnapshotEntry{}, removalEffectAuthority{}, false, err
	}
	if !expected.sameEntry(observed) {
		return removalTreeSnapshotEntry{}, removalEffectAuthority{}, false, fmt.Errorf("entry identity changed before cleanup at %q", path)
	}
	if err := validateOwnedStat(path, &stat); err != nil {
		return removalTreeSnapshotEntry{}, removalEffectAuthority{}, false, err
	}
	if err := authority.validateEntryAt(parentFD, name); err != nil {
		return removalTreeSnapshotEntry{}, removalEffectAuthority{}, false, err
	}

	root := removalTreeSnapshotEntry{
		name:     name,
		identity: newRemovalEntryIdentity(observed),
		mode:     fs.FileMode(stat.Mode).Perm(),
	}
	switch observed.kind {
	case entryKindRegular:
		if err := budget.admitBytes(stat.Size); err != nil {
			return removalTreeSnapshotEntry{}, removalEffectAuthority{}, false, err
		}
		root.size = stat.Size
	case entryKindDirectory:
		fd, err := openRemovalSnapshotDirectory(
			ctx,
			parentFD,
			path,
			&root,
			authority,
			nil,
			faults,
			state,
		)
		if err != nil {
			return removalTreeSnapshotEntry{}, removalEffectAuthority{}, state.changed, err
		}
		rootBinding := removalDirectoryBinding{
			parentFD:    parentFD,
			directoryFD: fd,
			path:        path,
			entry:       &root,
		}
		err = captureRemovalDirectorySnapshot(
			ctx,
			fd,
			path,
			0,
			&root,
			authority,
			[]removalDirectoryBinding{rootBinding},
			budget,
			faults,
			state,
		)
		closeErr := unix.Close(fd)
		if err != nil {
			return removalTreeSnapshotEntry{}, removalEffectAuthority{}, state.changed, err
		}
		if closeErr != nil {
			return removalTreeSnapshotEntry{}, removalEffectAuthority{}, state.changed, closeErr
		}
	case entryKindSymlink:
		// A no-follow unlink removes only the link itself.
	default:
		return removalTreeSnapshotEntry{}, removalEffectAuthority{}, state.changed, unsupported(
			fmt.Sprintf("cannot safely remove special entry %q", path),
			nil,
		)
	}
	if err := verifyRemovalSnapshotEntryAt(parentFD, path, root, authority); err != nil {
		return removalTreeSnapshotEntry{}, removalEffectAuthority{}, state.changed, err
	}
	return root, authority, state.changed, nil
}

func captureRemovalDirectorySnapshot(
	ctx context.Context,
	directoryFD int,
	directoryPath string,
	depth int,
	entry *removalTreeSnapshotEntry,
	authority removalEffectAuthority,
	bindings []removalDirectoryBinding,
	budget *treeTraversalBudget,
	faults faultPlan,
	state *removalTreeEffectState,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := authority.validateDirectoryHandle(directoryFD); err != nil {
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
		if err := authority.validateEntryAt(directoryFD, name); err != nil {
			return err
		}
		child := removalTreeSnapshotEntry{
			name:     name,
			identity: newRemovalEntryIdentity(identity),
			mode:     fs.FileMode(stat.Mode).Perm(),
		}
		switch identity.kind {
		case entryKindRegular:
			if err := budget.admitBytes(stat.Size); err != nil {
				return err
			}
			child.size = stat.Size
		case entryKindDirectory:
			if err := budget.admitDepth(depth + 1); err != nil {
				return err
			}
			fd, err := openRemovalSnapshotDirectory(
				ctx,
				directoryFD,
				childPath,
				&child,
				authority,
				bindings,
				faults,
				state,
			)
			if err != nil {
				return err
			}
			childBindings := append(bindings, removalDirectoryBinding{
				parentFD:    directoryFD,
				directoryFD: fd,
				path:        childPath,
				entry:       &child,
			})
			err = captureRemovalDirectorySnapshot(
				ctx,
				fd,
				childPath,
				depth+1,
				&child,
				authority,
				childBindings,
				budget,
				faults,
				state,
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
	return verifyRemovalDirectorySnapshot(ctx, directoryFD, directoryPath, *entry, authority)
}

func openRemovalSnapshotDirectory(
	ctx context.Context,
	parentFD int,
	path string,
	entry *removalTreeSnapshotEntry,
	authority removalEffectAuthority,
	bindings []removalDirectoryBinding,
	faults faultPlan,
	state *removalTreeEffectState,
) (int, error) {
	if entry.identity.kind != entryKindDirectory {
		return -1, fmt.Errorf("removal directory snapshot requires a directory at %q", path)
	}
	if entry.mode&0o500 != 0o500 {
		if err := repairRemovalSnapshotDirectory(
			ctx,
			parentFD,
			path,
			entry,
			authority,
			bindings,
			faults,
			state,
		); err != nil {
			return -1, err
		}
	}
	fd, err := openExpectedAt(parentFD, entry.name, path, entry.identity.entryAt(path))
	if err != nil {
		return -1, err
	}
	if err := authority.validateDirectoryHandle(fd); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

func repairRemovalSnapshotDirectory(
	ctx context.Context,
	parentFD int,
	path string,
	entry *removalTreeSnapshotEntry,
	authority removalEffectAuthority,
	bindings []removalDirectoryBinding,
	faults faultPlan,
	state *removalTreeEffectState,
) error {
	if state == nil {
		return fmt.Errorf("removal tree effect state is required")
	}
	if err := faults.check(ctx, phaseApplyMode); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	if err := authority.verify(bindings); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	if err := verifyRemovalSnapshotEntryAt(parentFD, path, *entry, authority); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	directoryFD, err := openRestrictiveRemovalDirectory(parentFD, entry.name)
	if err != nil {
		return atPhase(phaseApplyMode, fmt.Errorf("retain restrictive cleanup directory %q: %w", path, err))
	}
	if directoryFD >= 0 {
		defer unix.Close(directoryFD)
		if err := verifyRemovalEntryBinding(parentFD, path, directoryFD, *entry, authority); err != nil {
			return atPhase(phaseApplyMode, err)
		}
	}

	mode := uint32(entry.mode.Perm()) | 0o700
	if err := chmodRestrictiveRemovalDirectory(parentFD, entry.name, directoryFD, mode); err != nil {
		return atPhase(phaseApplyMode, fmt.Errorf("make retired directory removable at %q: %w", path, err))
	}
	state.changed = true
	current, stat, err := observeAnyAt(parentFD, entry.name, path)
	if err != nil {
		return atPhase(phaseApplyMode, err)
	}
	if !entry.identity.sameObject(current) || current.kind != entryKindDirectory {
		return atPhase(phaseApplyMode, fmt.Errorf("retired directory identity changed while preparing cleanup at %q", path))
	}
	if directoryFD >= 0 {
		var opened unix.Stat_t
		if err := unix.Fstat(directoryFD, &opened); err != nil {
			return atPhase(phaseApplyMode, err)
		}
		openedIdentity := identityFromStat(path, &opened)
		if !current.sameEntry(openedIdentity) {
			return atPhase(phaseApplyMode, fmt.Errorf("retired directory handle changed while preparing cleanup at %q", path))
		}
	}
	if err := validateOwnedStat(path, &stat); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	if err := authority.validateEntryAt(parentFD, entry.name); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	if fs.FileMode(stat.Mode).Perm() != fs.FileMode(mode).Perm() {
		return atPhase(
			phaseApplyMode,
			unsupported(fmt.Sprintf("retired directory %q did not retain private cleanup mode", path), nil),
		)
	}
	entry.identity = newRemovalEntryIdentity(current)
	entry.mode = fs.FileMode(stat.Mode).Perm()
	if err := authority.verify(bindings); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	return nil
}

func removeRemovalTreeSnapshot(
	ctx context.Context,
	parentFD int,
	path string,
	entry *removalTreeSnapshotEntry,
	authority removalEffectAuthority,
	limits mutationfs.TreeTraversalLimits,
	faults faultPlan,
) (bool, error) {
	if err := faults.check(ctx, phaseRevalidateCleanup); err != nil {
		return false, atPhase(phaseRevalidateCleanup, err)
	}
	if err := verifyRemovalTreeSnapshot(
		ctx,
		parentFD,
		path,
		*entry,
		authority,
		limits,
	); err != nil {
		return false, atPhase(phaseRevalidateCleanup, err)
	}
	if err := authority.verify(nil); err != nil {
		return false, atPhase(phaseRevalidateCleanup, err)
	}
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
		authority,
		nil,
		0,
		budget,
		faults,
		state,
	)
	return state.changed, err
}

func verifyRemovalTreeSnapshot(
	ctx context.Context,
	parentFD int,
	path string,
	entry removalTreeSnapshotEntry,
	authority removalEffectAuthority,
	limits mutationfs.TreeTraversalLimits,
) error {
	budget, err := newTreeTraversalBudget(limits)
	if err != nil {
		return err
	}
	switch entry.identity.kind {
	case entryKindRegular:
		if err := budget.admitBytes(entry.size); err != nil {
			return err
		}
		return verifyRemovalSnapshotEntryAt(parentFD, path, entry, authority)
	case entryKindSymlink:
		return verifyRemovalSnapshotEntryAt(parentFD, path, entry, authority)
	case entryKindDirectory:
		fd, err := openExpectedAt(parentFD, entry.name, path, entry.identity.entryAt(path))
		if err != nil {
			return err
		}
		defer unix.Close(fd)
		if err := authority.validateDirectoryHandle(fd); err != nil {
			return err
		}
		return verifyRemovalDirectoryTreeSnapshot(
			ctx,
			fd,
			path,
			entry,
			authority,
			0,
			budget,
		)
	default:
		return unsupported(fmt.Sprintf("cannot safely remove special entry %q", path), nil)
	}
}

func verifyRemovalDirectoryTreeSnapshot(
	ctx context.Context,
	directoryFD int,
	directoryPath string,
	entry removalTreeSnapshotEntry,
	authority removalEffectAuthority,
	depth int,
	budget *treeTraversalBudget,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := authority.validateDirectoryHandle(directoryFD); err != nil {
		return err
	}
	if err := budget.admitDepth(depth); err != nil {
		return err
	}
	if err := verifyOpenedRemovalSnapshotEntry(directoryFD, directoryPath, entry); err != nil {
		return err
	}
	if err := budget.admitEntries(len(entry.children)); err != nil {
		return err
	}
	if err := verifyRemovalDirectorySnapshot(ctx, directoryFD, directoryPath, entry, authority); err != nil {
		return err
	}
	for _, child := range entry.children {
		childPath := filepath.Join(directoryPath, child.name)
		switch child.identity.kind {
		case entryKindRegular:
			if err := budget.admitBytes(child.size); err != nil {
				return err
			}
			if err := verifyRemovalSnapshotEntryAt(directoryFD, childPath, child, authority); err != nil {
				return err
			}
		case entryKindDirectory:
			fd, err := openExpectedAt(directoryFD, child.name, childPath, child.identity.entryAt(childPath))
			if err != nil {
				return err
			}
			err = verifyRemovalDirectoryTreeSnapshot(
				ctx,
				fd,
				childPath,
				child,
				authority,
				depth+1,
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
			if err := verifyRemovalSnapshotEntryAt(directoryFD, childPath, child, authority); err != nil {
				return err
			}
		default:
			return unsupported(fmt.Sprintf("cannot safely remove special entry %q", childPath), nil)
		}
	}
	return verifyRemovalDirectorySnapshot(ctx, directoryFD, directoryPath, entry, authority)
}

func removeRemovalSnapshotEntry(
	ctx context.Context,
	parentFD int,
	path string,
	entry *removalTreeSnapshotEntry,
	authority removalEffectAuthority,
	ancestors []removalDirectoryBinding,
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
			authority,
			ancestors,
			budget,
			faults,
			state,
		)
	case entryKindSymlink:
		return removeRemovalSnapshotSymlink(
			ctx,
			parentFD,
			path,
			*entry,
			authority,
			ancestors,
			faults,
			state,
		)
	case entryKindDirectory:
		return removeRemovalSnapshotDirectory(
			ctx,
			parentFD,
			path,
			entry,
			authority,
			ancestors,
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
	authority removalEffectAuthority,
	ancestors []removalDirectoryBinding,
	budget *treeTraversalBudget,
	faults faultPlan,
	state *removalTreeEffectState,
) error {
	if err := budget.admitBytes(entry.size); err != nil {
		return err
	}
	if err := faults.check(ctx, phaseCleanupEntry); err != nil {
		return err
	}
	if err := authority.verify(ancestors); err != nil {
		return err
	}
	if err := verifyRemovalSnapshotEntryAt(parentFD, path, entry, authority); err != nil {
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
	authority removalEffectAuthority,
	ancestors []removalDirectoryBinding,
	faults faultPlan,
	state *removalTreeEffectState,
) error {
	if err := faults.check(ctx, phaseCleanupEntry); err != nil {
		return err
	}
	if err := authority.verify(ancestors); err != nil {
		return err
	}
	if err := verifyRemovalSnapshotEntryAt(parentFD, path, entry, authority); err != nil {
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
	authority removalEffectAuthority,
	ancestors []removalDirectoryBinding,
	depth int,
	budget *treeTraversalBudget,
	faults faultPlan,
	state *removalTreeEffectState,
) error {
	if err := budget.admitDepth(depth); err != nil {
		return err
	}
	fd, err := openExpectedAt(parentFD, entry.name, path, entry.identity.entryAt(path))
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := authority.validateDirectoryHandle(fd); err != nil {
		return err
	}
	bindings := append(ancestors, removalDirectoryBinding{
		parentFD:    parentFD,
		directoryFD: fd,
		path:        path,
		entry:       entry,
	})
	if err := prepareOpenedRemovalDirectory(
		ctx,
		bindings,
		authority,
		faults,
		state,
	); err != nil {
		return err
	}
	if err := authority.verify(bindings); err != nil {
		return err
	}
	if err := budget.admitEntries(len(entry.children)); err != nil {
		return err
	}
	if err := verifyRemovalDirectorySnapshot(ctx, fd, path, *entry, authority); err != nil {
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
			authority,
			bindings,
			depth+1,
			budget,
			faults,
			state,
		); err != nil {
			return err
		}
		if err := refreshRemovalDirectoryBinding(parentFD, path, fd, entry, authority); err != nil {
			return err
		}
		if err := authority.verify(bindings); err != nil {
			return err
		}
	}
	if err := faults.check(ctx, phaseCleanupEntry); err != nil {
		return err
	}
	if err := authority.verify(bindings); err != nil {
		return err
	}
	names, err := readDirectoryNames(ctx, fd, path, 0)
	if err != nil {
		return err
	}
	if len(names) != 0 {
		return fmt.Errorf("directory entries changed before cleanup at %q", path)
	}
	if err := authority.verify(bindings); err != nil {
		return err
	}
	if err := unix.Unlinkat(parentFD, entry.name, unix.AT_REMOVEDIR); err != nil {
		return err
	}
	state.changed = true
	return nil
}

func prepareOpenedRemovalDirectory(
	ctx context.Context,
	bindings []removalDirectoryBinding,
	authority removalEffectAuthority,
	faults faultPlan,
	state *removalTreeEffectState,
) error {
	if len(bindings) == 0 {
		return fmt.Errorf("removal directory binding is required")
	}
	binding := bindings[len(bindings)-1]
	if binding.entry == nil {
		return fmt.Errorf("removal directory binding is uninitialized")
	}
	if binding.entry.mode&0o700 == 0o700 {
		return authority.verify(bindings)
	}
	if state == nil {
		return fmt.Errorf("removal tree effect state is required")
	}
	if err := faults.check(ctx, phaseApplyMode); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	if err := authority.verify(bindings); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	mode := uint32(binding.entry.mode.Perm()) | 0o700
	if err := unix.Fchmod(binding.directoryFD, mode); err != nil {
		return atPhase(phaseApplyMode, fmt.Errorf("make retired directory removable at %q: %w", binding.path, err))
	}
	state.changed = true
	var stat unix.Stat_t
	if err := unix.Fstat(binding.directoryFD, &stat); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	current := identityFromStat(binding.path, &stat)
	if !binding.entry.identity.sameObject(current) || current.kind != entryKindDirectory {
		return atPhase(phaseApplyMode, fmt.Errorf("retired directory identity changed while preparing cleanup at %q", binding.path))
	}
	if err := validateOwnedStat(binding.path, &stat); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	if fs.FileMode(stat.Mode).Perm() != fs.FileMode(mode).Perm() {
		return atPhase(
			phaseApplyMode,
			unsupported(fmt.Sprintf("retired directory %q did not retain private cleanup mode", binding.path), nil),
		)
	}
	binding.entry.identity = newRemovalEntryIdentity(current)
	binding.entry.mode = fs.FileMode(stat.Mode).Perm()
	if err := authority.verify(bindings); err != nil {
		return atPhase(phaseApplyMode, err)
	}
	return nil
}

func verifyRemovalDirectorySnapshot(
	ctx context.Context,
	directoryFD int,
	directoryPath string,
	entry removalTreeSnapshotEntry,
	authority removalEffectAuthority,
) error {
	names, err := readDirectoryNames(ctx, directoryFD, directoryPath, len(entry.children))
	if err != nil {
		return err
	}
	if len(names) != len(entry.children) {
		return fmt.Errorf("directory entries changed before cleanup at %q", directoryPath)
	}
	for index, name := range names {
		if name != entry.children[index].name {
			return fmt.Errorf("directory entries changed before cleanup at %q", directoryPath)
		}
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
		if err := authority.validateEntryAt(directoryFD, name); err != nil {
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
	authority removalEffectAuthority,
) error {
	if err := verifyRemovalSnapshotEntryAt(parentFD, path, entry, authority); err != nil {
		return err
	}
	if err := authority.validateDirectoryHandle(fd); err != nil {
		return err
	}
	return verifyOpenedRemovalSnapshotEntry(fd, path, entry)
}

func (authority removalEffectAuthority) verify(bindings []removalDirectoryBinding) error {
	if authority.validateNamespace == nil {
		return fmt.Errorf("removal namespace validator is required")
	}
	if err := authority.validateNamespace(); err != nil {
		return err
	}
	for _, binding := range bindings {
		if binding.entry == nil {
			return fmt.Errorf("removal directory binding is uninitialized")
		}
		if err := verifyRemovalEntryBinding(
			binding.parentFD,
			binding.path,
			binding.directoryFD,
			*binding.entry,
			authority,
		); err != nil {
			return err
		}
	}
	return authority.validateNamespace()
}

func verifyRemovalSnapshotEntryAt(
	parentFD int,
	path string,
	entry removalTreeSnapshotEntry,
	authority removalEffectAuthority,
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
	if err := validateOwnedStat(path, &stat); err != nil {
		return err
	}
	return authority.validateEntryAt(parentFD, entry.name)
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
	authority removalEffectAuthority,
) error {
	if err := authority.validateDirectoryHandle(fd); err != nil {
		return err
	}
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
	if err := authority.validateEntryAt(parentFD, entry.name); err != nil {
		return err
	}
	entry.identity = newRemovalEntryIdentity(current)
	return nil
}
