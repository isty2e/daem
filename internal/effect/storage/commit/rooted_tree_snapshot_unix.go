//go:build darwin || linux

package commit

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"golang.org/x/sys/unix"
)

type preparedTreeContentDigest [sha256.Size]byte

type preparedTreeEntryExpectation struct {
	relativePath string
	kind         entryKind
	mode         fs.FileMode
	size         int64
	content      preparedTreeContentDigest
}

func (expectation preparedTreeEntryExpectation) validate(root bool) error {
	if root {
		if expectation.relativePath != "" {
			return fmt.Errorf("prepared tree root must not carry a relative path")
		}
	} else {
		components := strings.Split(expectation.relativePath, "/")
		path, err := mutationfs.NewTreeRelativePath(components...)
		if err != nil || path.Path() != expectation.relativePath {
			return fmt.Errorf("prepared tree relative path %q is not canonical", expectation.relativePath)
		}
	}
	if err := validateFileMode(expectation.mode); err != nil {
		return err
	}
	if root && expectation.kind != entryKindDirectory {
		return fmt.Errorf("prepared tree root must be a directory")
	}
	switch expectation.kind {
	case entryKindDirectory:
		if expectation.size != 0 || expectation.content != (preparedTreeContentDigest{}) {
			return fmt.Errorf("prepared tree directory %q carries file content facts", expectation.relativePath)
		}
	case entryKindRegular:
		if root {
			return fmt.Errorf("prepared tree root cannot be a regular file")
		}
		if expectation.size < 0 {
			return fmt.Errorf("prepared tree file %q has negative size", expectation.relativePath)
		}
	default:
		return fmt.Errorf("prepared tree entry %q has unsupported kind", expectation.relativePath)
	}
	return nil
}

type preparedTreeStatFacts struct {
	mode       fs.FileMode
	uid        uint32
	gid        uint32
	size       int64
	modifiedAt unix.Timespec
	links      uint64
}

func preparedTreeFactsFromStat(stat *unix.Stat_t) preparedTreeStatFacts {
	return preparedTreeStatFacts{
		mode:       fs.FileMode(stat.Mode).Perm(),
		uid:        stat.Uid,
		gid:        stat.Gid,
		size:       stat.Size,
		modifiedAt: stat.Mtim,
		links:      uint64(stat.Nlink),
	}
}

func (facts preparedTreeStatFacts) equal(other preparedTreeStatFacts) bool {
	return facts == other
}

type preparedTreeSnapshotEntry struct {
	expectation preparedTreeEntryExpectation
	identity    EntryIdentity
	facts       preparedTreeStatFacts
	children    []preparedTreeSnapshotEntry
}

type preparedTreeSnapshot struct {
	root preparedTreeSnapshotEntry
}

func (snapshot preparedTreeSnapshot) validate() error {
	_, err := validatePreparedTreeSnapshotEntry(snapshot.root, true, "")
	return err
}

func validatePreparedTreeSnapshotEntry(
	entry preparedTreeSnapshotEntry,
	root bool,
	parentPath string,
) (int, error) {
	if err := entry.expectation.validate(root); err != nil {
		return 0, err
	}
	if !entry.identity.valid() || entry.identity.kind != entry.expectation.kind {
		return 0, fmt.Errorf("prepared tree snapshot entry %q has no identity", entry.expectation.relativePath)
	}
	if entry.expectation.kind == entryKindRegular {
		if len(entry.children) != 0 {
			return 0, fmt.Errorf("prepared tree file %q contains children", entry.expectation.relativePath)
		}
		if entry.facts.size != entry.expectation.size {
			return 0, fmt.Errorf("prepared tree file %q has inconsistent size facts", entry.expectation.relativePath)
		}
		return 1, nil
	}
	count := 0
	previousName := ""
	for _, child := range entry.children {
		name := filepath.Base(child.expectation.relativePath)
		if name == "." || name == "" || (previousName != "" && name <= previousName) {
			return 0, fmt.Errorf("prepared tree directory %q has noncanonical children", entry.expectation.relativePath)
		}
		wantPath := name
		if !root {
			wantPath = parentPath + "/" + name
		}
		if child.expectation.relativePath != wantPath {
			return 0, fmt.Errorf("prepared tree child %q is outside its parent", child.expectation.relativePath)
		}
		childCount, err := validatePreparedTreeSnapshotEntry(child, false, wantPath)
		if err != nil {
			return 0, err
		}
		count += childCount
		previousName = name
	}
	return count + 1, nil
}

func capturePreparedTreeSnapshotLocked(
	ctx context.Context,
	prepared *PreparedRootedTree,
) (preparedTreeSnapshot, error) {
	if err := prepared.verifyStageObjectLocked(); err != nil {
		return preparedTreeSnapshot{}, err
	}
	if err := prepared.anchor.capability.ValidateDirectoryHandle(uintptr(prepared.stageFD)); err != nil {
		return preparedTreeSnapshot{}, err
	}
	budget, err := newTreeTraversalBudget(prepared.limits)
	if err != nil {
		return preparedTreeSnapshot{}, err
	}
	rootExpectation := preparedTreeEntryExpectation{
		kind: entryKindDirectory,
		mode: prepared.rootMode.Perm(),
	}
	root, count, err := capturePreparedTreeDirectory(
		ctx,
		prepared.stageFD,
		prepared.stagePath,
		"",
		0,
		rootExpectation,
		prepared.plannedEntries,
		prepared.anchor.capability.ValidateDirectoryHandle,
		budget,
	)
	if err != nil {
		return preparedTreeSnapshot{}, err
	}
	if count != len(prepared.plannedEntries) {
		return preparedTreeSnapshot{}, fmt.Errorf(
			"prepared tree snapshot has %d entries, want %d",
			count,
			len(prepared.plannedEntries),
		)
	}
	snapshot := preparedTreeSnapshot{root: root}
	if err := snapshot.validate(); err != nil {
		return preparedTreeSnapshot{}, err
	}
	return snapshot, nil
}

func capturePreparedTreeDirectory(
	ctx context.Context,
	directoryFD int,
	directoryPath string,
	relativePath string,
	depth int,
	expectation preparedTreeEntryExpectation,
	planned map[string]preparedTreeEntryExpectation,
	validateMount func(uintptr) error,
	budget *treeTraversalBudget,
) (preparedTreeSnapshotEntry, int, error) {
	if err := ctx.Err(); err != nil {
		return preparedTreeSnapshotEntry{}, 0, err
	}
	if err := budget.admitDepth(depth); err != nil {
		return preparedTreeSnapshotEntry{}, 0, err
	}
	root, err := captureOpenedPreparedTreeEntry(
		ctx,
		directoryFD,
		directoryPath,
		expectation,
		entryKindDirectory,
		preparedTreePrivateDirectoryMode,
		budget,
	)
	if err != nil {
		return preparedTreeSnapshotEntry{}, 0, err
	}
	names, err := readDirectoryNames(ctx, directoryFD, directoryPath, budget.remainingEntries())
	if err != nil {
		return preparedTreeSnapshotEntry{}, 0, err
	}
	if err := budget.admitEntries(len(names)); err != nil {
		return preparedTreeSnapshotEntry{}, 0, err
	}
	root.children = make([]preparedTreeSnapshotEntry, 0, len(names))
	entryCount := 0
	for _, name := range names {
		childRelativePath := name
		if relativePath != "" {
			childRelativePath = relativePath + "/" + name
		}
		childExpectation, exists := planned[childRelativePath]
		if !exists {
			return preparedTreeSnapshotEntry{}, 0, fmt.Errorf(
				"prepared tree contains undeclared entry %q",
				childRelativePath,
			)
		}
		childPath := filepath.Join(directoryPath, name)
		identity, stat, err := observeAnyAt(directoryFD, name, childPath)
		if err != nil {
			return preparedTreeSnapshotEntry{}, 0, err
		}
		if identity.kind != childExpectation.kind {
			return preparedTreeSnapshotEntry{}, 0, fmt.Errorf(
				"prepared tree entry %q kind changed",
				childRelativePath,
			)
		}
		if err := validateOwnedStat(childPath, &stat); err != nil {
			return preparedTreeSnapshotEntry{}, 0, err
		}
		childFD, err := openExpectedAt(directoryFD, name, childPath, identity)
		if err != nil {
			return preparedTreeSnapshotEntry{}, 0, err
		}
		if err := validateMount(uintptr(childFD)); err != nil {
			_ = unix.Close(childFD)
			return preparedTreeSnapshotEntry{}, 0, err
		}

		var child preparedTreeSnapshotEntry
		childCount := 0
		switch childExpectation.kind {
		case entryKindDirectory:
			child, childCount, err = capturePreparedTreeDirectory(
				ctx,
				childFD,
				childPath,
				childRelativePath,
				depth+1,
				childExpectation,
				planned,
				validateMount,
				budget,
			)
		case entryKindRegular:
			child, err = captureOpenedPreparedTreeEntry(
				ctx,
				childFD,
				childPath,
				childExpectation,
				entryKindRegular,
				preparedTreePrivateFileMode,
				budget,
			)
		default:
			err = unsupported(fmt.Sprintf("prepared tree contains unsupported entry %q", childPath), nil)
		}
		closeErr := unix.Close(childFD)
		if err != nil {
			return preparedTreeSnapshotEntry{}, 0, err
		}
		if closeErr != nil {
			return preparedTreeSnapshotEntry{}, 0, closeErr
		}
		root.children = append(root.children, child)
		entryCount += 1 + childCount
	}
	if err := verifyPreparedTreeDirectoryBinding(
		ctx,
		directoryFD,
		directoryPath,
		root,
	); err != nil {
		return preparedTreeSnapshotEntry{}, 0, err
	}
	return root, entryCount, nil
}

func captureOpenedPreparedTreeEntry(
	ctx context.Context,
	fd int,
	path string,
	expectation preparedTreeEntryExpectation,
	kind entryKind,
	currentMode fs.FileMode,
	budget *treeTraversalBudget,
) (preparedTreeSnapshotEntry, error) {
	if err := expectation.validate(expectation.relativePath == ""); err != nil {
		return preparedTreeSnapshotEntry{}, err
	}
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return preparedTreeSnapshotEntry{}, err
	}
	identity := identityFromStat(path, &before)
	if identity.kind != kind || identity.kind != expectation.kind {
		return preparedTreeSnapshotEntry{}, fmt.Errorf("prepared tree entry %q kind changed", path)
	}
	if err := validatePreparedTreeStat(path, &before, kind); err != nil {
		return preparedTreeSnapshotEntry{}, err
	}
	facts := preparedTreeFactsFromStat(&before)
	if facts.mode != currentMode.Perm() {
		return preparedTreeSnapshotEntry{}, fmt.Errorf(
			"prepared tree entry %q mode = %04o, want %04o",
			path,
			facts.mode,
			currentMode.Perm(),
		)
	}
	if expectation.kind == entryKindRegular {
		if facts.size != expectation.size {
			return preparedTreeSnapshotEntry{}, fmt.Errorf(
				"prepared tree file %q size = %d, want %d",
				path,
				facts.size,
				expectation.size,
			)
		}
		if err := budget.admitBytes(facts.size); err != nil {
			return preparedTreeSnapshotEntry{}, err
		}
		digest, err := digestPreparedTreeFile(ctx, fd, facts.size)
		if err != nil {
			return preparedTreeSnapshotEntry{}, err
		}
		if digest != expectation.content {
			return preparedTreeSnapshotEntry{}, fmt.Errorf("prepared tree file %q content changed", path)
		}
	}
	if err := requirePreparedTreeMetadataAbsent(fd, path, &before); err != nil {
		return preparedTreeSnapshotEntry{}, err
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return preparedTreeSnapshotEntry{}, err
	}
	if !identity.sameEntry(identityFromStat(path, &after)) ||
		!facts.equal(preparedTreeFactsFromStat(&after)) {
		return preparedTreeSnapshotEntry{}, fmt.Errorf("prepared tree entry %q changed while capturing", path)
	}
	return preparedTreeSnapshotEntry{
		expectation: expectation,
		identity:    identity,
		facts:       facts,
	}, nil
}

func verifyPreparedTreeSnapshotLocked(
	ctx context.Context,
	prepared *PreparedRootedTree,
) error {
	if err := prepared.snapshot.validate(); err != nil {
		return err
	}
	if err := prepared.anchor.capability.ValidateDirectoryHandle(uintptr(prepared.stageFD)); err != nil {
		return err
	}
	budget, err := newTreeTraversalBudget(prepared.limits)
	if err != nil {
		return err
	}
	return verifyPreparedTreeSnapshotDirectory(
		ctx,
		prepared.stageFD,
		prepared.stagePath,
		prepared.snapshot.root,
		0,
		prepared.anchor.capability.ValidateDirectoryHandle,
		budget,
	)
}

func verifyPreparedTreeSnapshotDirectory(
	ctx context.Context,
	directoryFD int,
	directoryPath string,
	expected preparedTreeSnapshotEntry,
	depth int,
	validateMount func(uintptr) error,
	budget *treeTraversalBudget,
) error {
	if err := budget.admitDepth(depth); err != nil {
		return err
	}
	if err := verifyOpenedPreparedTreeAuthority(ctx, directoryFD, directoryPath, expected, budget); err != nil {
		return err
	}
	names, err := readDirectoryNames(ctx, directoryFD, directoryPath, len(expected.children))
	if err != nil {
		return err
	}
	if err := budget.admitEntries(len(names)); err != nil {
		return err
	}
	expectedNames := preparedTreeSnapshotChildNames(expected)
	if !slices.Equal(names, expectedNames) {
		return fmt.Errorf("prepared tree directory entries changed at %q", directoryPath)
	}
	for index, child := range expected.children {
		name := names[index]
		childPath := filepath.Join(directoryPath, name)
		identity, _, err := observeAnyAt(directoryFD, name, childPath)
		if err != nil {
			return err
		}
		if !child.identity.sameEntry(identity) {
			return fmt.Errorf("prepared tree entry %q identity changed", childPath)
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
			err = verifyPreparedTreeSnapshotDirectory(
				ctx,
				childFD,
				childPath,
				child,
				depth+1,
				validateMount,
				budget,
			)
		} else {
			err = verifyOpenedPreparedTreeAuthority(ctx, childFD, childPath, child, budget)
		}
		closeErr := unix.Close(childFD)
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return verifyPreparedTreeDirectoryBinding(ctx, directoryFD, directoryPath, expected)
}

func verifyOpenedPreparedTreeEntry(
	ctx context.Context,
	fd int,
	path string,
	expected preparedTreeSnapshotEntry,
	budget *treeTraversalBudget,
) error {
	return verifyOpenedPreparedTreeEntryWithContent(ctx, fd, path, expected, budget, true)
}

func verifyOpenedPreparedTreeAuthority(
	ctx context.Context,
	fd int,
	path string,
	expected preparedTreeSnapshotEntry,
	budget *treeTraversalBudget,
) error {
	return verifyOpenedPreparedTreeEntryWithContent(ctx, fd, path, expected, budget, false)
}

func verifyOpenedPreparedTreeEntryWithContent(
	ctx context.Context,
	fd int,
	path string,
	expected preparedTreeSnapshotEntry,
	budget *treeTraversalBudget,
	verifyContent bool,
) error {
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return err
	}
	if !expected.identity.sameEntry(identityFromStat(path, &before)) ||
		!expected.facts.equal(preparedTreeFactsFromStat(&before)) {
		return fmt.Errorf("prepared tree entry %q changed", path)
	}
	if err := validatePreparedTreeStat(path, &before, expected.expectation.kind); err != nil {
		return err
	}
	if expected.expectation.kind == entryKindRegular && verifyContent {
		if err := budget.admitBytes(expected.facts.size); err != nil {
			return err
		}
		digest, err := digestPreparedTreeFile(ctx, fd, expected.facts.size)
		if err != nil {
			return err
		}
		if digest != expected.expectation.content {
			return fmt.Errorf("prepared tree file %q content changed", path)
		}
	}
	if err := requirePreparedTreeMetadataAbsent(fd, path, &before); err != nil {
		return err
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return err
	}
	if !expected.identity.sameEntry(identityFromStat(path, &after)) ||
		!expected.facts.equal(preparedTreeFactsFromStat(&after)) {
		return fmt.Errorf("prepared tree entry %q changed while validating", path)
	}
	return nil
}

func verifyPreparedTreeDirectoryBinding(
	ctx context.Context,
	directoryFD int,
	directoryPath string,
	expected preparedTreeSnapshotEntry,
) error {
	names, err := readDirectoryNames(ctx, directoryFD, directoryPath, len(expected.children))
	if err != nil {
		return err
	}
	if !slices.Equal(names, preparedTreeSnapshotChildNames(expected)) {
		return fmt.Errorf("prepared tree directory entries changed at %q", directoryPath)
	}
	for index, child := range expected.children {
		childPath := filepath.Join(directoryPath, names[index])
		identity, _, err := observeAnyAt(directoryFD, names[index], childPath)
		if err != nil {
			return err
		}
		if !child.identity.sameEntry(identity) {
			return fmt.Errorf("prepared tree entry %q identity changed", childPath)
		}
	}
	return nil
}

func preparedTreeSnapshotChildNames(entry preparedTreeSnapshotEntry) []string {
	names := make([]string, 0, len(entry.children))
	for _, child := range entry.children {
		names = append(names, filepath.Base(child.expectation.relativePath))
	}
	return names
}

func digestPreparedTreeFile(
	ctx context.Context,
	fd int,
	expectedSize int64,
) (preparedTreeContentDigest, error) {
	if expectedSize < 0 {
		return preparedTreeContentDigest{}, fmt.Errorf("prepared tree file size must not be negative")
	}
	if _, err := unix.Seek(fd, 0, 0); err != nil {
		return preparedTreeContentDigest{}, err
	}
	reader := &rootedSnapshotFileReader{
		ctx:       ctx,
		fd:        fd,
		remaining: expectedSize + 1,
	}
	digest := sha256.New()
	read, err := io.Copy(digest, reader)
	if err != nil {
		return preparedTreeContentDigest{}, err
	}
	if read != expectedSize {
		return preparedTreeContentDigest{}, fmt.Errorf(
			"prepared tree file size changed while reading: read %d, want %d",
			read,
			expectedSize,
		)
	}
	var result preparedTreeContentDigest
	copy(result[:], digest.Sum(nil))
	return result, nil
}

func requirePreparedTreeMetadataAbsent(fd int, path string, stat *unix.Stat_t) error {
	metadata, err := capturePreservedMetadata(fd, stat)
	if err != nil {
		return err
	}
	for name := range metadata.xattrs {
		if !isAllowedPreparedTreeXattr(name) {
			return unsupported(
				fmt.Sprintf("prepared tree entry %q contains unsupported extended attribute %q", path, name),
				nil,
			)
		}
	}
	return nil
}

func validatePreparedTreeStat(path string, stat *unix.Stat_t, kind entryKind) error {
	if err := validateOwnedStat(path, stat); err != nil {
		return err
	}
	const unsupportedModeBits = unix.S_ISUID | unix.S_ISGID | unix.S_ISVTX
	if stat.Mode&unsupportedModeBits != 0 {
		return unsupported(
			fmt.Sprintf("prepared tree entry %q carries unsupported mode bits %#o", path, stat.Mode&unsupportedModeBits),
			nil,
		)
	}
	if kind == entryKindRegular && stat.Nlink != 1 {
		return unsupported(
			fmt.Sprintf("prepared tree file %q has %d hard links", path, stat.Nlink),
			nil,
		)
	}
	return nil
}
