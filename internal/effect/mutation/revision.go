package mutation

import (
	"context"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

const revisionFormatVersion = "daem-mutation-revision-v1"

type revisionKind uint8

const (
	revisionAbsent revisionKind = iota + 1
	revisionFile
	revisionDirectory
	revisionSymlink
)

// RevisionRequest identifies the boundary observation used to capture a revision.
type RevisionRequest struct {
	Path   string
	Effect PathEffect

	maximumRegularFileBytes int64
	requireBoundedFile      bool
}

// SnapshotRevision is immutable evidence for one filesystem observation.
type SnapshotRevision struct {
	kind          revisionKind
	canonicalPath string
	digest        [sha256.Size]byte
	valid         bool
}

// Equal reports whether two revisions captured the same semantic observation.
func (revision SnapshotRevision) Equal(other SnapshotRevision) bool {
	if !revision.valid || !other.valid ||
		revision.kind != other.kind ||
		revision.canonicalPath != other.canonicalPath ||
		revision.digest != other.digest {
		return false
	}
	return true
}

// CaptureRevision captures a context-aware immutable filesystem revision.
func CaptureRevision(ctx context.Context, request RevisionRequest) (SnapshotRevision, error) {
	return captureRevision(ctx, request, nil)
}

type revisionFileCacheEntry struct {
	info     os.FileInfo
	revision SnapshotRevision
}

func captureRevision(
	ctx context.Context,
	request RevisionRequest,
	fileCache map[string]revisionFileCacheEntry,
) (SnapshotRevision, error) {
	if ctx == nil {
		return SnapshotRevision{}, fmt.Errorf("mutation revision context is required")
	}
	if err := ctx.Err(); err != nil {
		return SnapshotRevision{}, err
	}
	identity, err := canonicalPathIdentity(request.Path, request.Effect)
	if err != nil {
		return SnapshotRevision{}, err
	}

	info, err := os.Lstat(identity.accessPath)
	if err != nil {
		if os.IsNotExist(err) {
			hasher := newRevisionHasher(identity.keyPath, revisionAbsent)
			return newSnapshotRevision(revisionAbsent, identity.keyPath, hasher), nil
		}
		return SnapshotRevision{}, fmt.Errorf("inspect mutation revision path %q: %w", request.Path, err)
	}

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		if request.Effect != PathEffectDirectoryEntry {
			return SnapshotRevision{}, fmt.Errorf("referent mutation path %q resolved to an unexpected symlink", request.Path)
		}
		target, err := os.Readlink(identity.accessPath)
		if err != nil {
			return SnapshotRevision{}, fmt.Errorf("read mutation revision symlink %q: %w", request.Path, err)
		}
		hasher := newRevisionHasher(identity.keyPath, revisionSymlink)
		writeRevisionRecord(hasher, "symlink", target)
		return newSnapshotRevision(revisionSymlink, identity.keyPath, hasher), nil
	case info.Mode().IsRegular():
		if request.requireBoundedFile && info.Size() > request.maximumRegularFileBytes {
			return SnapshotRevision{}, fmt.Errorf(
				"mutation revision file %q exceeds %d bytes",
				request.Path,
				request.maximumRegularFileBytes,
			)
		}
		if request.requireBoundedFile {
			if cached, ok := fileCache[identity.keyPath]; ok &&
				sameRevisionFileInfo(cached.info, info) {
				return cached.revision, nil
			}
		}
		hasher := newRevisionHasher(identity.keyPath, revisionFile)
		maximumBytes := int64(0)
		if request.requireBoundedFile {
			maximumBytes = request.maximumRegularFileBytes
		}
		if err := hashRevisionFile(
			ctx,
			hasher,
			identity.accessPath,
			".",
			info,
			maximumBytes,
		); err != nil {
			return SnapshotRevision{}, err
		}
		revision := newSnapshotRevision(revisionFile, identity.keyPath, hasher)
		if request.requireBoundedFile && fileCache != nil {
			fileCache[identity.keyPath] = revisionFileCacheEntry{
				info:     info,
				revision: revision,
			}
		}
		return revision, nil
	case info.IsDir():
		if request.requireBoundedFile {
			return SnapshotRevision{}, fmt.Errorf(
				"mutation revision path %q must be absent or resolve to a regular file",
				request.Path,
			)
		}
		hasher := newRevisionHasher(identity.keyPath, revisionDirectory)
		if err := hashRevisionDirectory(ctx, hasher, identity.accessPath); err != nil {
			return SnapshotRevision{}, err
		}
		return newSnapshotRevision(revisionDirectory, identity.keyPath, hasher), nil
	default:
		return SnapshotRevision{}, fmt.Errorf("mutation revision path %q has unsupported file mode %s", request.Path, info.Mode())
	}
}

func sameRevisionFileInfo(left os.FileInfo, right os.FileInfo) bool {
	return os.SameFile(left, right) &&
		left.Mode() == right.Mode() &&
		left.Size() == right.Size() &&
		left.ModTime().Equal(right.ModTime())
}

func newRevisionHasher(canonicalPath string, kind revisionKind) hash.Hash {
	hasher := sha256.New()
	writeRevisionRecord(hasher, revisionFormatVersion, canonicalPath, strconv.Itoa(int(kind)))
	return hasher
}

func newSnapshotRevision(kind revisionKind, canonicalPath string, hasher hash.Hash) SnapshotRevision {
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return SnapshotRevision{kind: kind, canonicalPath: canonicalPath, digest: digest, valid: true}
}

func hashRevisionDirectory(ctx context.Context, hasher hash.Hash, root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect mutation revision entry %q: %w", path, err)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("compute mutation revision path for %q: %w", path, err)
		}
		relative = filepath.ToSlash(relative)
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("read mutation revision symlink %q: %w", path, err)
			}
			writeRevisionRecord(hasher, "symlink", relative, target)
			return nil
		case info.IsDir():
			writeRevisionRecord(hasher, "directory", relative)
			return nil
		case info.Mode().IsRegular():
			return hashRevisionFile(ctx, hasher, path, relative, info, 0)
		default:
			return fmt.Errorf("mutation revision entry %q has unsupported file mode %s", path, info.Mode())
		}
	})
}

func hashRevisionFile(
	ctx context.Context,
	hasher hash.Hash,
	path string,
	relative string,
	info os.FileInfo,
	maximumBytes int64,
) error {
	executable := "not-executable"
	if info.Mode().Perm()&0o111 != 0 {
		executable = "executable"
	}
	writeRevisionRecord(hasher, "file", filepath.ToSlash(relative), executable, strconv.FormatInt(info.Size(), 10))
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open mutation revision file %q: %w", path, err)
	}
	defer file.Close()

	buffer := make([]byte, 32*1024)
	total := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			total += int64(count)
			if maximumBytes > 0 && total > maximumBytes {
				return fmt.Errorf(
					"mutation revision file %q exceeds %d bytes",
					path,
					maximumBytes,
				)
			}
			_, _ = hasher.Write(buffer[:count])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read mutation revision file %q: %w", path, readErr)
		}
	}
	writeRevisionRecord(hasher, "end-file")
	return nil
}

func writeRevisionRecord(hasher hash.Hash, fields ...string) {
	for _, field := range fields {
		_, _ = hasher.Write([]byte(strconv.Itoa(len(field))))
		_, _ = hasher.Write([]byte(":"))
		_, _ = hasher.Write([]byte(field))
	}
	_, _ = hasher.Write([]byte("\n"))
}
