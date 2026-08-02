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

func readRegularFileContext(
	ctx context.Context,
	path string,
	maximumBytes int64,
	hooks readHooks,
) (content []byte, exists bool, err error) {
	if ctx == nil {
		return nil, false, fmt.Errorf("file snapshot context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if maximumBytes <= 0 {
		return nil, false, fmt.Errorf("maximum file size must be positive")
	}

	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, false, ErrSymlink
	}
	if !before.Mode().IsRegular() {
		return nil, false, ErrNotRegular
	}
	if before.Size() > maximumBytes {
		return nil, false, limitError(maximumBytes)
	}
	if hooks.afterInspect != nil {
		hooks.afterInspect()
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, ErrChanged
	}
	if err != nil {
		return nil, false, err
	}
	defer file.Close()

	opened, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if !os.SameFile(before, opened) ||
		!sameFileVersion(before, opened) {
		return nil, false, ErrChanged
	}
	if hooks.afterOpen != nil {
		hooks.afterOpen()
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	buffer := make([]byte, 32*1024)
	content = make([]byte, 0, min(opened.Size(), int64(len(buffer))))
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
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
				return nil, false, limitError(maximumBytes)
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return nil, false, readErr
			}
			break
		}
		if count == 0 {
			return nil, false, fmt.Errorf("read regular file: no progress")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	afterOpen, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	afterPath, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, ErrChanged
	}
	if err != nil {
		return nil, false, fmt.Errorf("reinspect file: %w", err)
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
		return nil, false, ErrChanged
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	return content, true, nil
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
