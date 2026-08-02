// Package filesnapshot reads bounded host-owned regular files without
// following a final symlink or accepting an identity change during the read.
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

type changeVersion struct {
	seconds     int64
	nanoseconds int64
}

// Snapshot is one identity-stable regular-file observation.
type Snapshot struct {
	content []byte
	mode    os.FileMode
}

// Content returns a defensive copy of the observed bytes.
func (snapshot Snapshot) Content() []byte { return append([]byte(nil), snapshot.content...) }

// Mode returns the mode observed from the same file descriptor as Content.
func (snapshot Snapshot) Mode() os.FileMode { return snapshot.mode }

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

func readRegularFileContext(
	ctx context.Context,
	path string,
	maximumBytes int64,
	hooks readHooks,
) (content []byte, exists bool, err error) {
	snapshot, exists, err := readRegularFileSnapshotContext(ctx, path, maximumBytes, hooks)
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
	if before.Mode()&os.ModeSymlink != 0 {
		return Snapshot{}, false, ErrSymlink
	}
	if !before.Mode().IsRegular() {
		return Snapshot{}, false, ErrNotRegular
	}
	if before.Size() > maximumBytes {
		return Snapshot{}, false, limitError(maximumBytes)
	}
	if hooks.afterInspect != nil {
		hooks.afterInspect()
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, err
	}

	file, err := openRegularFile(path)
	if err != nil {
		return Snapshot{}, false, classifyOpenFailure(path, before, err)
	}
	defer file.Close()

	opened, err := file.Stat()
	if err != nil {
		return Snapshot{}, false, err
	}
	if !os.SameFile(before, opened) ||
		!opened.Mode().IsRegular() ||
		!sameFileVersion(before, opened) {
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
	afterPath, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, false, ErrChanged
	}
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("reinspect file: %w", err)
	}
	if afterPath.Mode()&os.ModeSymlink != 0 ||
		!afterPath.Mode().IsRegular() ||
		!os.SameFile(opened, afterOpen) ||
		!os.SameFile(opened, afterPath) ||
		!sameFileVersion(opened, afterOpen) ||
		!sameFileVersion(opened, afterPath) ||
		int64(len(content)) != opened.Size() ||
		int64(len(content)) != afterOpen.Size() ||
		int64(len(content)) != afterPath.Size() {
		return Snapshot{}, false, ErrChanged
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, err
	}
	return Snapshot{content: content, mode: opened.Mode()}, true, nil
}

func classifyOpenFailure(path string, before os.FileInfo, openErr error) error {
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
	if after.Mode()&os.ModeSymlink != 0 ||
		!after.Mode().IsRegular() ||
		!os.SameFile(before, after) ||
		!sameFileVersion(before, after) {
		return ErrChanged
	}
	return openErr
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
