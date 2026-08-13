// Package filesnapshot reads bounded regular files without accepting an
// identity change during the read.
package filesnapshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

var (
	// ErrSymlink reports a final path component that is a symlink.
	ErrSymlink = errors.New("file must not be a symlink")
	// ErrNotRegular reports a final path component that is not a regular file.
	ErrNotRegular = errors.New("path must be a regular file")
	// ErrLimitExceeded reports content beyond the caller-owned byte budget.
	ErrLimitExceeded = errors.New("file size limit exceeded")
	// ErrChanged reports a path or referent that changed during observation.
	ErrChanged = errors.New("file changed while reading")
)

type readHooks struct {
	afterInspect func()
	afterOpen    func()
}

type finalSymlinkPolicy uint8

const (
	rejectFinalSymlink finalSymlinkPolicy = iota
	followFinalSymlink
)

type changeVersion struct {
	seconds     int64
	nanoseconds int64
}

// Snapshot is one identity-stable regular-file observation.
type Snapshot struct {
	content  []byte
	mode     os.FileMode
	revision string
}

// Content returns a defensive copy of the observed bytes.
func (snapshot Snapshot) Content() []byte { return append([]byte(nil), snapshot.content...) }

// Mode returns the mode observed from the same file descriptor as Content.
func (snapshot Snapshot) Mode() os.FileMode { return snapshot.mode }

// Revision returns the exact content, metadata, and object identity observed
// from the same stable file descriptor as Content.
func (snapshot Snapshot) Revision() string { return snapshot.revision }

type limitExceededError struct {
	maximumBytes int64
}

func (err limitExceededError) Error() string {
	return fmt.Sprintf("file exceeds %d bytes", err.maximumBytes)
}

func (err limitExceededError) Unwrap() error { return ErrLimitExceeded }

// ReadRegularFile reads at most maximumBytes from path. A missing path returns
// exists=false; every other non-regular or unstable state is an error.
func ReadRegularFile(path string, maximumBytes int64) (content []byte, exists bool, err error) {
	return ReadRegularFileContext(context.Background(), path, maximumBytes)
}

// ReadRegularFileContext reads at most maximumBytes from path and checks ctx
// around each regular-file read. A missing path returns exists=false; every
// other non-regular or unstable state is an error.
func ReadRegularFileContext(
	ctx context.Context,
	path string,
	maximumBytes int64,
) (content []byte, exists bool, err error) {
	return readRegularFileContext(ctx, path, maximumBytes, readHooks{})
}

// ReadRegularFileSnapshotContext reads one bounded regular-file snapshot and
// returns content and mode from the same stable descriptor.
func ReadRegularFileSnapshotContext(
	ctx context.Context,
	path string,
	maximumBytes int64,
) (snapshot Snapshot, exists bool, err error) {
	return readRegularFileSnapshotContext(ctx, path, maximumBytes, readHooks{})
}

// ReadRegularFileReferentContext reads the regular-file referent selected by
// path, including through a stable final symlink.
func ReadRegularFileReferentContext(
	ctx context.Context,
	path string,
	maximumBytes int64,
) (content []byte, exists bool, err error) {
	return readRegularFileReferentContext(ctx, path, maximumBytes, readHooks{})
}

// ReadRegularFileReferentSnapshotContext returns one bounded snapshot of the
// regular-file referent selected by path, including through a stable final
// symlink.
func ReadRegularFileReferentSnapshotContext(
	ctx context.Context,
	path string,
	maximumBytes int64,
) (snapshot Snapshot, exists bool, err error) {
	return readRegularFileReferentSnapshotContext(ctx, path, maximumBytes, readHooks{})
}

func readRegularFileContext(
	ctx context.Context,
	path string,
	maximumBytes int64,
	hooks readHooks,
) (content []byte, exists bool, err error) {
	snapshot, exists, err := readRegularFileSnapshot(
		ctx,
		path,
		maximumBytes,
		rejectFinalSymlink,
		hooks,
	)
	if err != nil || !exists {
		return nil, exists, err
	}
	return snapshot.content, true, nil
}

func readRegularFileReferentContext(
	ctx context.Context,
	path string,
	maximumBytes int64,
	hooks readHooks,
) (content []byte, exists bool, err error) {
	snapshot, exists, err := readRegularFileReferentSnapshotContext(
		ctx,
		path,
		maximumBytes,
		hooks,
	)
	if err != nil || !exists {
		return nil, exists, err
	}
	return snapshot.content, true, nil
}

func readRegularFileSnapshotContext(
	ctx context.Context,
	path string,
	maximumBytes int64,
	hooks readHooks,
) (snapshot Snapshot, exists bool, err error) {
	return readRegularFileSnapshot(
		ctx,
		path,
		maximumBytes,
		rejectFinalSymlink,
		hooks,
	)
}

func readRegularFileReferentSnapshotContext(
	ctx context.Context,
	path string,
	maximumBytes int64,
	hooks readHooks,
) (snapshot Snapshot, exists bool, err error) {
	return readRegularFileSnapshot(
		ctx,
		path,
		maximumBytes,
		followFinalSymlink,
		hooks,
	)
}

func readRegularFileSnapshot(
	ctx context.Context,
	path string,
	maximumBytes int64,
	symlinkPolicy finalSymlinkPolicy,
	hooks readHooks,
) (snapshot Snapshot, exists bool, err error) {
	if ctx == nil {
		return Snapshot{}, false, fmt.Errorf("file snapshot context is required")
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, err
	}
	if maximumBytes <= 0 {
		return Snapshot{}, false, fmt.Errorf("maximum file size must be positive")
	}

	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, err
	}
	beforeIsSymlink := before.Mode()&os.ModeSymlink != 0
	if beforeIsSymlink && symlinkPolicy == rejectFinalSymlink {
		return Snapshot{}, false, ErrSymlink
	}
	if !beforeIsSymlink && !before.Mode().IsRegular() {
		return Snapshot{}, false, ErrNotRegular
	}
	if !beforeIsSymlink && before.Size() > maximumBytes {
		return Snapshot{}, false, limitError(maximumBytes)
	}
	beforeLinkTarget := ""
	beforeReferent := before
	if beforeIsSymlink {
		beforeLinkTarget, err = os.Readlink(path)
		if err != nil {
			return Snapshot{}, false, fmt.Errorf("read file symlink: %w", err)
		}
		beforeReferent, err = os.Stat(path)
		if err != nil {
			return Snapshot{}, false, fmt.Errorf("inspect file referent: %w", err)
		}
		if !beforeReferent.Mode().IsRegular() {
			return Snapshot{}, false, ErrNotRegular
		}
		if beforeReferent.Size() > maximumBytes {
			return Snapshot{}, false, limitError(maximumBytes)
		}
	}
	if hooks.afterInspect != nil {
		hooks.afterInspect()
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, err
	}

	file, err := openRegularFile(path, symlinkPolicy == followFinalSymlink)
	if err != nil {
		return Snapshot{}, false, classifyOpenFailure(
			path,
			before,
			beforeReferent,
			beforeLinkTarget,
			symlinkPolicy,
			err,
		)
	}
	defer file.Close()

	opened, err := file.Stat()
	if err != nil {
		return Snapshot{}, false, err
	}
	if !opened.Mode().IsRegular() {
		if !beforeIsSymlink {
			return Snapshot{}, false, ErrChanged
		}
		return Snapshot{}, false, ErrNotRegular
	}
	if opened.Size() > maximumBytes {
		return Snapshot{}, false, limitError(maximumBytes)
	}
	if !os.SameFile(beforeReferent, opened) ||
		!sameFileVersion(beforeReferent, opened) {
		return Snapshot{}, false, ErrChanged
	}
	if hooks.afterOpen != nil {
		hooks.afterOpen()
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, err
	}

	buffer := make([]byte, 32*1024)
	content := make([]byte, 0, min(opened.Size(), int64(len(buffer))))
	for {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, false, err
		}
		remaining := maximumBytes - int64(len(content))
		readSize := len(buffer)
		if remaining < int64(readSize) {
			readSize = int(remaining) + 1
		}
		count, readErr := file.Read(buffer[:readSize])
		if count > 0 {
			content = append(content, buffer[:count]...)
			if int64(len(content)) > maximumBytes {
				return Snapshot{}, false, limitError(maximumBytes)
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return Snapshot{}, false, readErr
			}
			break
		}
		if count == 0 {
			return Snapshot{}, false, fmt.Errorf("read regular file: no progress")
		}
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, err
	}

	afterOpen, err := file.Stat()
	if err != nil {
		return Snapshot{}, false, err
	}
	afterEntry, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, false, ErrChanged
	}
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("reinspect file: %w", err)
	}
	if !sameSelectedEntry(path, before, afterEntry, beforeLinkTarget, symlinkPolicy) ||
		!os.SameFile(opened, afterOpen) ||
		!sameFileVersion(opened, afterOpen) ||
		int64(len(content)) != opened.Size() ||
		int64(len(content)) != afterOpen.Size() {
		return Snapshot{}, false, ErrChanged
	}
	if symlinkPolicy == followFinalSymlink {
		afterReferent, statErr := os.Stat(path)
		if statErr != nil || !afterReferent.Mode().IsRegular() ||
			!os.SameFile(opened, afterReferent) ||
			!sameFileVersion(opened, afterReferent) ||
			int64(len(content)) != afterReferent.Size() {
			return Snapshot{}, false, ErrChanged
		}
	} else if !os.SameFile(opened, afterEntry) ||
		!sameFileVersion(opened, afterEntry) ||
		int64(len(content)) != afterEntry.Size() {
		return Snapshot{}, false, ErrChanged
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, err
	}
	return Snapshot{
		content:  content,
		mode:     opened.Mode(),
		revision: regularFileSnapshotRevision(opened, content),
	}, true, nil
}

func classifyOpenFailure(
	path string,
	before os.FileInfo,
	beforeReferent os.FileInfo,
	beforeLinkTarget string,
	symlinkPolicy finalSymlinkPolicy,
	openErr error,
) error {
	if errors.Is(openErr, ErrChanged) || errors.Is(openErr, os.ErrNotExist) {
		return ErrChanged
	}

	after, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return ErrChanged
	}
	if err != nil {
		return openErr
	}
	if !sameSelectedEntry(path, before, after, beforeLinkTarget, symlinkPolicy) {
		return ErrChanged
	}
	if symlinkPolicy == followFinalSymlink {
		afterReferent, statErr := os.Stat(path)
		if statErr != nil || !os.SameFile(beforeReferent, afterReferent) ||
			!sameFileVersion(beforeReferent, afterReferent) {
			return ErrChanged
		}
	}
	return openErr
}

func sameSelectedEntry(
	path string,
	before os.FileInfo,
	after os.FileInfo,
	beforeLinkTarget string,
	symlinkPolicy finalSymlinkPolicy,
) bool {
	if !os.SameFile(before, after) || !sameFileVersion(before, after) {
		return false
	}
	beforeIsSymlink := before.Mode()&os.ModeSymlink != 0
	afterIsSymlink := after.Mode()&os.ModeSymlink != 0
	if beforeIsSymlink != afterIsSymlink {
		return false
	}
	if beforeIsSymlink {
		if symlinkPolicy != followFinalSymlink {
			return false
		}
		afterLinkTarget, err := os.Readlink(path)
		if err != nil {
			return false
		}
		return beforeLinkTarget == afterLinkTarget
	}
	return before.Mode().IsRegular() && after.Mode().IsRegular()
}

func sameFileVersion(left os.FileInfo, right os.FileInfo) bool {
	if left.Size() != right.Size() || !left.ModTime().Equal(right.ModTime()) {
		return false
	}
	leftChange, leftOK := fileChangeVersion(left)
	rightChange, rightOK := fileChangeVersion(right)
	if leftOK != rightOK {
		return false
	}
	return !leftOK || leftChange == rightChange
}

func limitError(maximumBytes int64) error {
	return limitExceededError{maximumBytes: maximumBytes}
}
