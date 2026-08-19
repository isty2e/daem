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
	// ErrUnsupported reports that this platform cannot bind a directory-entry
	// snapshot to the retained directory descriptor.
	ErrUnsupported = errors.New("descriptor-relative file snapshot is unsupported on this platform")
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
	version  RegularFileVersion
}

// Content returns a defensive copy of the observed bytes.
func (snapshot Snapshot) Content() []byte { return append([]byte(nil), snapshot.content...) }

// Mode returns the mode observed from the same file descriptor as Content.
func (snapshot Snapshot) Mode() os.FileMode { return snapshot.mode }

// Revision returns the exact content, metadata, and object identity observed
// from the same stable file descriptor as Content.
func (snapshot Snapshot) Revision() string { return snapshot.revision }

// FileVersion returns strong metadata evidence for cache reuse. ok is false
// when the platform cannot supply object identity and change-version facts.
func (snapshot Snapshot) FileVersion() (version RegularFileVersion, ok bool) {
	return snapshot.version, snapshot.version.valid
}

type limitExceededError struct {
	maximumBytes int64
}

func (err limitExceededError) Error() string {
	return fmt.Sprintf("file exceeds %d bytes", err.maximumBytes)
}

func (err limitExceededError) Unwrap() error { return ErrLimitExceeded }

// CountedContent is one bounded regular-file observation plus the bytes the
// read loop actually transferred before success, cancellation, or failure.
type CountedContent struct {
	Content   []byte
	Exists    bool
	Attempted int64
}

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
	result, err := ReadRegularFileContextCounted(ctx, path, maximumBytes)
	return result.Content, result.Exists, err
}

// ReadRegularFileContextCounted is ReadRegularFileContext plus attempted read bytes.
func ReadRegularFileContextCounted(
	ctx context.Context,
	path string,
	maximumBytes int64,
) (CountedContent, error) {
	return readRegularFileContextCounted(ctx, path, maximumBytes, readHooks{})
}

// ReadRegularFileAtCounted reads at most maximumBytes from one directory entry
// of dir without following that entry, and reports attempted read bytes.
// Identity checks stay on dir's descriptor and name; they do not re-inspect a
// pathname. A missing entry returns exists=false. Platforms without a proven
// descriptor-relative adapter return ErrUnsupported without reading.
func ReadRegularFileAtCounted(
	ctx context.Context,
	dir *os.File,
	name string,
	maximumBytes int64,
) (CountedContent, error) {
	return readRegularFileAtCounted(ctx, dir, name, maximumBytes)
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
	result, err := readRegularFileContextCounted(ctx, path, maximumBytes, hooks)
	return result.Content, result.Exists, err
}

func readRegularFileContextCounted(
	ctx context.Context,
	path string,
	maximumBytes int64,
	hooks readHooks,
) (CountedContent, error) {
	snapshot, exists, attempted, err := readRegularFileSnapshot(
		ctx,
		path,
		maximumBytes,
		rejectFinalSymlink,
		hooks,
	)
	if err != nil || !exists {
		return CountedContent{Exists: exists, Attempted: attempted}, err
	}
	return CountedContent{Content: snapshot.content, Exists: true, Attempted: attempted}, nil
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
	snapshot, exists, _, err = readRegularFileSnapshot(
		ctx,
		path,
		maximumBytes,
		rejectFinalSymlink,
		hooks,
	)
	return snapshot, exists, err
}

func readRegularFileReferentSnapshotContext(
	ctx context.Context,
	path string,
	maximumBytes int64,
	hooks readHooks,
) (snapshot Snapshot, exists bool, err error) {
	snapshot, exists, _, err = readRegularFileSnapshot(
		ctx,
		path,
		maximumBytes,
		followFinalSymlink,
		hooks,
	)
	return snapshot, exists, err
}

func readRegularFileSnapshot(
	ctx context.Context,
	path string,
	maximumBytes int64,
	symlinkPolicy finalSymlinkPolicy,
	hooks readHooks,
) (snapshot Snapshot, exists bool, attempted int64, err error) {
	if ctx == nil {
		return Snapshot{}, false, 0, fmt.Errorf("file snapshot context is required")
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, 0, err
	}
	if maximumBytes <= 0 {
		return Snapshot{}, false, 0, fmt.Errorf("maximum file size must be positive")
	}

	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, false, 0, nil
	}
	if err != nil {
		return Snapshot{}, false, 0, err
	}
	beforeIsSymlink := before.Mode()&os.ModeSymlink != 0
	if beforeIsSymlink && symlinkPolicy == rejectFinalSymlink {
		return Snapshot{}, false, 0, ErrSymlink
	}
	if !beforeIsSymlink && !before.Mode().IsRegular() {
		return Snapshot{}, false, 0, ErrNotRegular
	}
	if !beforeIsSymlink && before.Size() > maximumBytes {
		return Snapshot{}, false, 0, limitError(maximumBytes)
	}
	beforeLinkTarget := ""
	beforeReferent := before
	if beforeIsSymlink {
		beforeLinkTarget, err = os.Readlink(path)
		if err != nil {
			return Snapshot{}, false, 0, fmt.Errorf("read file symlink: %w", err)
		}
		beforeReferent, err = os.Stat(path)
		if err != nil {
			return Snapshot{}, false, 0, fmt.Errorf("inspect file referent: %w", err)
		}
		if !beforeReferent.Mode().IsRegular() {
			return Snapshot{}, false, 0, ErrNotRegular
		}
		if beforeReferent.Size() > maximumBytes {
			return Snapshot{}, false, 0, limitError(maximumBytes)
		}
	}
	if hooks.afterInspect != nil {
		hooks.afterInspect()
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, 0, err
	}

	file, err := openRegularFile(path, symlinkPolicy == followFinalSymlink)
	if err != nil {
		return Snapshot{}, false, 0, classifyOpenFailure(
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
		return Snapshot{}, false, 0, err
	}
	if !opened.Mode().IsRegular() {
		if !beforeIsSymlink {
			return Snapshot{}, false, 0, ErrChanged
		}
		return Snapshot{}, false, 0, ErrNotRegular
	}
	if !os.SameFile(beforeReferent, opened) ||
		!sameFileVersion(beforeReferent, opened) {
		return Snapshot{}, false, 0, ErrChanged
	}
	if opened.Size() > maximumBytes {
		return Snapshot{}, false, 0, limitError(maximumBytes)
	}
	if hooks.afterOpen != nil {
		hooks.afterOpen()
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, 0, err
	}

	content, attempted, err := readBoundedRegularFile(ctx, file, maximumBytes, opened.Size())
	if err != nil {
		return Snapshot{}, false, attempted, err
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, attempted, err
	}

	afterOpen, err := file.Stat()
	if err != nil {
		return Snapshot{}, false, attempted, err
	}
	afterEntry, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, false, attempted, ErrChanged
	}
	if err != nil {
		return Snapshot{}, false, attempted, fmt.Errorf("reinspect file: %w", err)
	}
	if !sameSelectedEntry(path, before, afterEntry, beforeLinkTarget, symlinkPolicy) ||
		!os.SameFile(opened, afterOpen) ||
		!sameFileVersion(opened, afterOpen) ||
		int64(len(content)) != opened.Size() ||
		int64(len(content)) != afterOpen.Size() {
		return Snapshot{}, false, attempted, ErrChanged
	}
	if symlinkPolicy == followFinalSymlink {
		afterReferent, statErr := os.Stat(path)
		if statErr != nil || !afterReferent.Mode().IsRegular() ||
			!os.SameFile(opened, afterReferent) ||
			!sameFileVersion(opened, afterReferent) ||
			int64(len(content)) != afterReferent.Size() {
			return Snapshot{}, false, attempted, ErrChanged
		}
	} else if !os.SameFile(opened, afterEntry) ||
		!sameFileVersion(opened, afterEntry) ||
		int64(len(content)) != afterEntry.Size() {
		return Snapshot{}, false, attempted, ErrChanged
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, attempted, err
	}
	version, _ := RegularFileVersionOf(opened)
	return Snapshot{
		content:  content,
		mode:     opened.Mode(),
		revision: regularFileSnapshotRevision(opened, content),
		version:  version,
	}, true, attempted, nil
}

func readBoundedRegularFile(
	ctx context.Context,
	file *os.File,
	maximumBytes int64,
	sizeHint int64,
) (content []byte, attempted int64, err error) {
	if sizeHint < 0 {
		sizeHint = 0
	}
	buffer := make([]byte, 32*1024)
	content = make([]byte, 0, min(sizeHint, int64(len(buffer))))
	for {
		if err := ctx.Err(); err != nil {
			return nil, attempted, err
		}
		remaining := maximumBytes - attempted
		readSize := len(buffer)
		if remaining < int64(readSize) {
			readSize = int(remaining) + 1
		}
		if readSize <= 0 {
			return nil, attempted, limitError(maximumBytes)
		}
		count, readErr := file.Read(buffer[:readSize])
		if count > 0 {
			attempted += int64(count)
			content = append(content, buffer[:count]...)
			if attempted > maximumBytes {
				return nil, attempted, limitError(maximumBytes)
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return nil, attempted, readErr
			}
			break
		}
		if count == 0 {
			return nil, attempted, fmt.Errorf("read regular file: no progress")
		}
	}
	return content, attempted, nil
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
