//go:build darwin || linux

package commit

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"

	"golang.org/x/sys/unix"
)

func (writer *rootedTreeWriterUnix) SetRootMode(mode fs.FileMode) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if !writer.active || writer.prepared == nil || writer.prepared.state != preparedRootedTreeReady {
		return fmt.Errorf("rooted tree writer is no longer active")
	}
	if err := writer.ctx.Err(); err != nil {
		return err
	}
	if err := validateFileMode(mode); err != nil {
		return err
	}
	if writer.prepared.rootModeSet {
		return fmt.Errorf("rooted tree root mode is already set")
	}
	writer.prepared.rootMode = mode.Perm()
	writer.prepared.rootModeSet = true
	return nil
}

func (prepared *PreparedRootedTree) applyTreeModesLocked(ctx context.Context) error {
	prepared.modesMayRestrictCleanup = true
	budget, err := newTreeTraversalBudget(prepared.limits)
	if err != nil {
		return err
	}
	if err := applyPreparedTreeDirectoryMode(
		ctx,
		prepared.stageFD,
		prepared.stagePath,
		&prepared.snapshot.root,
		0,
		prepared.anchor.capability.ValidateDirectoryHandle,
		budget,
	); err != nil {
		return err
	}
	prepared.expected = prepared.snapshot.root.identity
	return nil
}

func applyPreparedTreeDirectoryMode(
	ctx context.Context,
	directoryFD int,
	directoryPath string,
	expected *preparedTreeSnapshotEntry,
	depth int,
	validateMount func(uintptr) error,
	budget *treeTraversalBudget,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := budget.admitDepth(depth); err != nil {
		return err
	}
	if err := verifyOpenedPreparedTreeAuthority(ctx, directoryFD, directoryPath, *expected, budget); err != nil {
		return err
	}
	names, err := readDirectoryNames(ctx, directoryFD, directoryPath, len(expected.children))
	if err != nil {
		return err
	}
	if err := budget.admitEntries(len(names)); err != nil {
		return err
	}
	if !slices.Equal(names, preparedTreeSnapshotChildNames(*expected)) {
		return fmt.Errorf("prepared tree directory entries changed at %q", directoryPath)
	}
	for index := range expected.children {
		if err := ctx.Err(); err != nil {
			return err
		}
		child := &expected.children[index]
		name := names[index]
		childPath := filepath.Join(directoryPath, name)
		identity, stat, err := observeAnyAt(directoryFD, name, childPath)
		if err != nil {
			return err
		}
		if !child.identity.sameEntry(identity) ||
			!child.facts.equal(preparedTreeFactsFromStat(&stat)) {
			return fmt.Errorf("prepared tree entry %q changed", childPath)
		}
		childFD, err := openExpectedAt(directoryFD, name, childPath, child.identity)
		if err != nil {
			return err
		}
		if err := validateMount(uintptr(childFD)); err != nil {
			_ = unix.Close(childFD)
			return err
		}
		if child.expectation.kind == entryKindDirectory {
			err = applyPreparedTreeDirectoryMode(
				ctx,
				childFD,
				childPath,
				child,
				depth+1,
				validateMount,
				budget,
			)
		} else {
			err = applyPreparedTreeFileMode(ctx, childFD, childPath, child, budget)
		}
		closeErr := unix.Close(childFD)
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err := verifyPreparedTreeDirectoryBinding(ctx, directoryFD, directoryPath, *expected); err != nil {
		return err
	}
	if expected.facts.mode != expected.expectation.mode.Perm() {
		if err := unix.Fchmod(directoryFD, uint32(expected.expectation.mode.Perm())); err != nil {
			return err
		}
	}
	var after unix.Stat_t
	if err := unix.Fstat(directoryFD, &after); err != nil {
		return err
	}
	if err := validatePreparedTreeStat(directoryPath, &after, entryKindDirectory); err != nil {
		return err
	}
	afterIdentity := identityFromStat(directoryPath, &after)
	if !expected.identity.sameObject(afterIdentity) || afterIdentity.kind != entryKindDirectory {
		return fmt.Errorf("prepared tree directory %q identity changed during mode transition", directoryPath)
	}
	wantFacts := expected.facts
	wantFacts.mode = expected.expectation.mode.Perm()
	afterFacts := preparedTreeFactsFromStat(&after)
	if !wantFacts.equal(afterFacts) {
		return unsupported(fmt.Sprintf("prepared tree directory %q did not retain authorized facts", directoryPath), nil)
	}
	if err := syncDirectory(directoryFD); err != nil {
		return err
	}
	var synchronized unix.Stat_t
	if err := unix.Fstat(directoryFD, &synchronized); err != nil {
		return err
	}
	synchronizedIdentity := identityFromStat(directoryPath, &synchronized)
	if !afterIdentity.sameEntry(synchronizedIdentity) ||
		!afterFacts.equal(preparedTreeFactsFromStat(&synchronized)) {
		return fmt.Errorf("prepared tree directory %q changed while synchronizing", directoryPath)
	}
	expected.identity = synchronizedIdentity
	expected.facts = afterFacts
	return nil
}

func applyPreparedTreeFileMode(
	ctx context.Context,
	fileFD int,
	filePath string,
	expected *preparedTreeSnapshotEntry,
	budget *treeTraversalBudget,
) error {
	if err := verifyOpenedPreparedTreeEntry(ctx, fileFD, filePath, *expected, budget); err != nil {
		return err
	}
	if expected.facts.mode != expected.expectation.mode.Perm() {
		if err := unix.Fchmod(fileFD, uint32(expected.expectation.mode.Perm())); err != nil {
			return err
		}
	}
	var after unix.Stat_t
	if err := unix.Fstat(fileFD, &after); err != nil {
		return err
	}
	if err := validatePreparedTreeStat(filePath, &after, entryKindRegular); err != nil {
		return err
	}
	afterIdentity := identityFromStat(filePath, &after)
	if !expected.identity.sameObject(afterIdentity) || afterIdentity.kind != entryKindRegular {
		return fmt.Errorf("prepared tree file %q identity changed during mode transition", filePath)
	}
	wantFacts := expected.facts
	wantFacts.mode = expected.expectation.mode.Perm()
	afterFacts := preparedTreeFactsFromStat(&after)
	if !wantFacts.equal(afterFacts) {
		return unsupported(fmt.Sprintf("prepared tree file %q did not retain authorized facts", filePath), nil)
	}
	if err := syncPayload(fileFD); err != nil {
		return err
	}
	var synchronized unix.Stat_t
	if err := unix.Fstat(fileFD, &synchronized); err != nil {
		return err
	}
	synchronizedIdentity := identityFromStat(filePath, &synchronized)
	if !afterIdentity.sameEntry(synchronizedIdentity) ||
		!afterFacts.equal(preparedTreeFactsFromStat(&synchronized)) {
		return fmt.Errorf("prepared tree file %q changed while synchronizing", filePath)
	}
	expected.identity = synchronizedIdentity
	expected.facts = afterFacts
	return nil
}

func (prepared *PreparedRootedTree) normalizeStageModesForCleanupLocked(ctx context.Context) error {
	if !prepared.modesMayRestrictCleanup || prepared.snapshot.root.identity.kind == entryKindInvalid {
		return nil
	}
	budget, err := newTreeTraversalBudget(prepared.limits)
	if err != nil {
		return err
	}
	return normalizePreparedTreeDirectoryForCleanup(
		ctx,
		prepared.stageFD,
		prepared.stagePath,
		&prepared.snapshot.root,
		0,
		prepared.anchor.capability.ValidateDirectoryHandle,
		budget,
	)
}

func normalizePreparedTreeDirectoryForCleanup(
	ctx context.Context,
	directoryFD int,
	directoryPath string,
	expected *preparedTreeSnapshotEntry,
	depth int,
	validateMount func(uintptr) error,
	budget *treeTraversalBudget,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := budget.admitDepth(depth); err != nil {
		return err
	}
	if err := normalizeOpenedPreparedTreeModeForCleanup(
		directoryFD,
		directoryPath,
		expected,
		preparedTreePrivateDirectoryMode,
	); err != nil {
		return err
	}
	if err := validateMount(uintptr(directoryFD)); err != nil {
		return err
	}
	names, err := readDirectoryNames(ctx, directoryFD, directoryPath, len(expected.children))
	if err != nil {
		return err
	}
	if err := budget.admitEntries(len(names)); err != nil {
		return err
	}
	if !slices.Equal(names, preparedTreeSnapshotChildNames(*expected)) {
		return fmt.Errorf("prepared tree directory entries changed at %q", directoryPath)
	}
	for index, name := range names {
		child := &expected.children[index]
		childPath := filepath.Join(directoryPath, name)
		identity, stat, err := observeAnyAt(directoryFD, name, childPath)
		if err != nil {
			return err
		}
		if err := validatePreparedTreeCleanupTransition(childPath, child, identity, &stat); err != nil {
			return err
		}
		cleanupMode := preparedTreePrivateFileMode
		if child.expectation.kind == entryKindDirectory {
			cleanupMode = preparedTreePrivateDirectoryMode
		}
		if fs.FileMode(stat.Mode).Perm() != cleanupMode {
			if err := unix.Fchmodat(
				directoryFD,
				name,
				uint32(cleanupMode),
				unix.AT_SYMLINK_NOFOLLOW,
			); err != nil {
				return err
			}
			identity, stat, err = observeAnyAt(directoryFD, name, childPath)
			if err != nil {
				return err
			}
			if err := validatePreparedTreeCleanupTransition(childPath, child, identity, &stat); err != nil {
				return err
			}
		}
		child.identity = identity
		child.facts = preparedTreeFactsFromStat(&stat)
		childFD, err := openExpectedAt(directoryFD, name, childPath, identity)
		if err != nil {
			return err
		}
		if err := validateMount(uintptr(childFD)); err != nil {
			_ = unix.Close(childFD)
			return err
		}
		if child.expectation.kind == entryKindDirectory {
			err = normalizePreparedTreeDirectoryForCleanup(
				ctx,
				childFD,
				childPath,
				child,
				depth+1,
				validateMount,
				budget,
			)
		} else {
			err = verifyOpenedPreparedTreeEntry(ctx, childFD, childPath, *child, budget)
		}
		closeErr := unix.Close(childFD)
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return verifyPreparedTreeDirectoryBinding(ctx, directoryFD, directoryPath, *expected)
}

func normalizeOpenedPreparedTreeModeForCleanup(
	fd int,
	path string,
	expected *preparedTreeSnapshotEntry,
	cleanupMode fs.FileMode,
) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	identity := identityFromStat(path, &stat)
	if err := validatePreparedTreeCleanupTransition(path, expected, identity, &stat); err != nil {
		return err
	}
	if fs.FileMode(stat.Mode).Perm() != cleanupMode {
		if err := unix.Fchmod(fd, uint32(cleanupMode)); err != nil {
			return err
		}
		if err := unix.Fstat(fd, &stat); err != nil {
			return err
		}
		identity = identityFromStat(path, &stat)
		if err := validatePreparedTreeCleanupTransition(path, expected, identity, &stat); err != nil {
			return err
		}
	}
	metadata, err := capturePreparedTreeMetadataFacts(fd, path, &stat)
	if err != nil {
		return err
	}
	if !expected.metadata.equal(metadata) {
		return fmt.Errorf("prepared tree entry %q metadata changed before cleanup", path)
	}
	expected.identity = identity
	expected.facts = preparedTreeFactsFromStat(&stat)
	return nil
}

func validatePreparedTreeCleanupTransition(
	path string,
	expected *preparedTreeSnapshotEntry,
	identity EntryIdentity,
	stat *unix.Stat_t,
) error {
	if !expected.identity.sameObject(identity) || identity.kind != expected.expectation.kind {
		return fmt.Errorf("prepared tree entry %q identity changed before cleanup", path)
	}
	if err := validatePreparedTreeStat(path, stat, expected.expectation.kind); err != nil {
		return err
	}
	actual := preparedTreeFactsFromStat(stat)
	want := expected.facts
	want.mode = actual.mode
	if !want.equal(actual) {
		return fmt.Errorf("prepared tree entry %q facts changed before cleanup", path)
	}
	allowedMode := actual.mode == expected.facts.mode || actual.mode == expected.expectation.mode.Perm()
	if expected.expectation.kind == entryKindDirectory {
		allowedMode = allowedMode || actual.mode == preparedTreePrivateDirectoryMode
	} else {
		allowedMode = allowedMode || actual.mode == preparedTreePrivateFileMode
	}
	if !allowedMode {
		return fmt.Errorf("prepared tree entry %q mode changed before cleanup", path)
	}
	return nil
}

func verifyTreeEntryMode(fd int, path string, mode fs.FileMode) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if err := validateOwnedStat(path, &stat); err != nil {
		return err
	}
	if fs.FileMode(stat.Mode).Perm() != mode.Perm() {
		return unsupported(fmt.Sprintf("prepared tree directory %q did not retain requested mode", path), nil)
	}
	return nil
}
