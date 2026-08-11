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
	if err := verifyOpenedPreparedTreeEntry(ctx, directoryFD, directoryPath, *expected, budget); err != nil {
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
	if err := validateOwnedStat(directoryPath, &after); err != nil {
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
	if err := validateOwnedStat(filePath, &after); err != nil {
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
