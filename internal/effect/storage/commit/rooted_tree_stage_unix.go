//go:build darwin || linux

package commit

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"golang.org/x/sys/unix"
)

const (
	preparedTreePrivateDirectoryMode fs.FileMode = 0o700
	preparedTreePrivateFileMode      fs.FileMode = 0o600
)

func createPreparedRootedTree(
	path string,
	anchor *anchoredParent,
	limits mutationfs.TreeTraversalLimits,
) (*PreparedRootedTree, error) {
	stageName, err := unusedSiblingName(anchor.parentFD(), temporaryPrefix)
	if err != nil {
		return nil, err
	}
	if err := unix.Mkdirat(anchor.parentFD(), stageName, uint32(preparedTreePrivateDirectoryMode)); err != nil {
		return nil, err
	}
	stagePath := filepath.Join(filepath.Dir(path), stageName)
	prepared := &PreparedRootedTree{
		state:          preparedRootedTreeReady,
		destination:    path,
		anchor:         anchor,
		stageName:      stageName,
		stagePath:      stagePath,
		stageFD:        -1,
		limits:         limits,
		rootMode:       preparedTreePrivateDirectoryMode,
		plannedEntries: make(map[string]preparedTreeEntryExpectation),
	}
	identity, stat, err := anchor.observe(stageName, stagePath)
	if err != nil {
		return prepared, err
	}
	prepared.stageObject = identity
	if identity.kind != entryKindDirectory {
		return prepared, fmt.Errorf("rooted tree stage %q is not a directory", stagePath)
	}
	if err := validateOwnedStat(stagePath, &stat); err != nil {
		return prepared, err
	}
	stageFD, _, err := anchor.openExpected(stageName, stagePath, identity)
	if err != nil {
		return prepared, err
	}
	prepared.stageFD = stageFD
	if err := anchor.capability.ValidateDirectoryHandle(uintptr(stageFD)); err != nil {
		return prepared, err
	}
	rootMetadata, err := capturePreparedTreeMetadataFacts(stageFD, stagePath, &stat)
	if err != nil {
		return prepared, err
	}
	prepared.rootCreationMetadata = rootMetadata.creationMetadata()
	return prepared, nil
}

func (prepared *PreparedRootedTree) captureSnapshotLocked(ctx context.Context) error {
	snapshot, err := capturePreparedTreeSnapshotLocked(ctx, prepared)
	if err != nil {
		return err
	}
	prepared.snapshot = snapshot
	prepared.expected = snapshot.root.identity
	prepared.plannedEntries = nil
	return nil
}

func (prepared *PreparedRootedTree) verifyExpectedLocked() error {
	if err := prepared.verifyStageObjectLocked(); err != nil {
		return err
	}
	_, _, err := prepared.anchor.requireExpected(prepared.stageName, prepared.stagePath, prepared.expected)
	return err
}

func (prepared *PreparedRootedTree) requireDestinationAbsentLocked() error {
	identity, _, err := prepared.anchor.observe(prepared.anchor.base, prepared.destination)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	if identity.kind == entryKindSymlink {
		return rootedFinalSymlinkFailure(prepared.destination)
	}
	return fs.ErrExist
}

type rootedTreeWriterUnix struct {
	mu       sync.Mutex
	ctx      context.Context
	prepared *PreparedRootedTree
	budget   *treeTraversalBudget
	active   bool
}

func (writer *rootedTreeWriterUnix) deactivate() {
	writer.mu.Lock()
	writer.active = false
	writer.mu.Unlock()
}

func (writer *rootedTreeWriterUnix) CreateDirectory(path mutationfs.TreeRelativePath, mode fs.FileMode) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if err := writer.validate(path, mode); err != nil {
		return err
	}
	if err := writer.budget.admitWrittenEntry(path.Depth()); err != nil {
		return fmt.Errorf("prepared tree directory %q: %w", path.Path(), err)
	}
	parent, err := writer.openParent(path)
	if err != nil {
		return err
	}
	defer parent.close()
	if err := unix.Mkdirat(parent.fd, parent.base, uint32(preparedTreePrivateDirectoryMode)); err != nil {
		return err
	}
	identity, stat, err := observeAt(parent.fd, parent.base, parent.path)
	if err != nil {
		return err
	}
	if identity.kind != entryKindDirectory {
		return fmt.Errorf("prepared tree entry %q is not a directory", parent.path)
	}
	if err := validateOwnedStat(parent.path, &stat); err != nil {
		return err
	}
	fd, err := openExpectedAt(parent.fd, parent.base, parent.path, identity)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	creationMetadata, err := capturePreparedTreeMetadataFacts(fd, parent.path, &stat)
	if err != nil {
		return err
	}
	if err := verifyTreeEntryMode(fd, parent.path, preparedTreePrivateDirectoryMode); err != nil {
		return err
	}
	if err := writer.prepared.anchor.capability.ValidateDirectoryHandle(uintptr(fd)); err != nil {
		return err
	}
	if err := writer.verifyStage(); err != nil {
		return err
	}
	return writer.recordPlannedEntry(preparedTreeEntryExpectation{
		relativePath:     path.Path(),
		kind:             entryKindDirectory,
		mode:             mode.Perm(),
		creationMetadata: creationMetadata.creationMetadata(),
	})
}

func (writer *rootedTreeWriterUnix) WriteFile(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
	content io.Reader,
) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if content == nil {
		return fmt.Errorf("prepared tree file content is required")
	}
	if err := writer.validate(path, mode); err != nil {
		return err
	}
	if err := writer.budget.admitWrittenEntry(path.Depth() - 1); err != nil {
		return fmt.Errorf("prepared tree file %q: %w", path.Path(), err)
	}
	parent, err := writer.openParent(path)
	if err != nil {
		return err
	}
	defer parent.close()
	fd, err := unix.Openat(
		parent.fd,
		parent.base,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		uint32(preparedTreePrivateFileMode),
	)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = unix.Close(fd)
		}
	}()
	if err := unix.Fchmod(fd, uint32(preparedTreePrivateFileMode)); err != nil {
		return err
	}
	var creationStat unix.Stat_t
	if err := unix.Fstat(fd, &creationStat); err != nil {
		return err
	}
	creationMetadata, err := capturePreparedTreeMetadataFacts(fd, parent.path, &creationStat)
	if err != nil {
		return err
	}
	reader := contextReader{ctx: writer.ctx, reader: content}
	digest := sha256.New()
	remainingBytes := writer.budget.remainingBytes()
	written, err := io.CopyN(io.MultiWriter(fdWriter{fd: fd}, digest), reader, remainingBytes)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if written == remainingBytes {
		hasExtra, probeErr := readerHasContent(reader)
		if probeErr != nil {
			return probeErr
		}
		if hasExtra {
			return fmt.Errorf(
				"prepared tree file %q: tree exceeds %d regular-file bytes",
				path.Path(),
				writer.budget.limits.MaximumBytes(),
			)
		}
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if err := validateOwnedStat(parent.path, &stat); err != nil {
		return err
	}
	if err := writer.budget.admitBytes(stat.Size); err != nil {
		return fmt.Errorf("prepared tree file %q: %w", path.Path(), err)
	}
	if fs.FileMode(stat.Mode).Perm() != preparedTreePrivateFileMode {
		return unsupported(fmt.Sprintf("prepared tree file %q did not retain private staging mode", parent.path), nil)
	}
	if err := unix.Close(fd); err != nil {
		return err
	}
	closed = true
	if err := writer.verifyStage(); err != nil {
		return err
	}
	var contentDigest preparedTreeContentDigest
	copy(contentDigest[:], digest.Sum(nil))
	return writer.recordPlannedEntry(preparedTreeEntryExpectation{
		relativePath:     path.Path(),
		kind:             entryKindRegular,
		mode:             mode.Perm(),
		size:             stat.Size,
		content:          contentDigest,
		creationMetadata: creationMetadata.creationMetadata(),
	})
}

func readerHasContent(reader io.Reader) (bool, error) {
	var probe [1]byte
	for range 100 {
		count, err := reader.Read(probe[:])
		if count != 0 {
			return true, nil
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return false, nil
			}
			return false, err
		}
	}
	return false, io.ErrNoProgress
}

func (writer *rootedTreeWriterUnix) validate(path mutationfs.TreeRelativePath, mode fs.FileMode) error {
	if !writer.active || writer.prepared == nil || writer.prepared.state != preparedRootedTreeReady {
		return fmt.Errorf("rooted tree writer is no longer active")
	}
	if err := writer.ctx.Err(); err != nil {
		return err
	}
	if err := path.Validate(); err != nil {
		return err
	}
	return validateFileMode(mode)
}

func (writer *rootedTreeWriterUnix) verifyStage() error {
	return writer.prepared.verifyStageObjectLocked()
}

func (writer *rootedTreeWriterUnix) recordPlannedEntry(entry preparedTreeEntryExpectation) error {
	if err := entry.validate(false); err != nil {
		return err
	}
	if _, exists := writer.prepared.plannedEntries[entry.relativePath]; exists {
		return fmt.Errorf("prepared tree entry %q was planned more than once", entry.relativePath)
	}
	writer.prepared.plannedEntries[entry.relativePath] = entry
	return nil
}

type openedTreeParent struct {
	fd     int
	base   string
	path   string
	opened []int
}

func (writer *rootedTreeWriterUnix) openParent(path mutationfs.TreeRelativePath) (openedTreeParent, error) {
	if err := writer.verifyStage(); err != nil {
		return openedTreeParent{}, err
	}
	components := strings.Split(path.Path(), "/")
	parent := openedTreeParent{
		fd:   writer.prepared.stageFD,
		base: components[len(components)-1],
		path: filepath.Join(append([]string{writer.prepared.stagePath}, components...)...),
	}
	currentPath := writer.prepared.stagePath
	for _, component := range components[:len(components)-1] {
		currentPath = filepath.Join(currentPath, component)
		identity, stat, err := observeAt(parent.fd, component, currentPath)
		if err != nil {
			parent.close()
			return openedTreeParent{}, err
		}
		if identity.kind != entryKindDirectory {
			parent.close()
			return openedTreeParent{}, fmt.Errorf("prepared tree ancestor %q is not a directory", currentPath)
		}
		if err := validateOwnedStat(currentPath, &stat); err != nil {
			parent.close()
			return openedTreeParent{}, err
		}
		fd, err := openExpectedAt(parent.fd, component, currentPath, identity)
		if err != nil {
			parent.close()
			return openedTreeParent{}, err
		}
		if err := writer.prepared.anchor.capability.ValidateDirectoryHandle(uintptr(fd)); err != nil {
			_ = unix.Close(fd)
			parent.close()
			return openedTreeParent{}, err
		}
		parent.fd = fd
		parent.opened = append(parent.opened, fd)
	}
	return parent, nil
}

func (parent *openedTreeParent) close() {
	for index := len(parent.opened) - 1; index >= 0; index-- {
		_ = unix.Close(parent.opened[index])
	}
	parent.opened = nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(payload []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(payload)
}
