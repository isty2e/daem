package mutation

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/isty2e/daem/internal/filesnapshot"
)

const revisionFormatVersion = "daem-mutation-revision-v1"

type revisionKind uint8

const (
	revisionAbsent revisionKind = iota + 1
	revisionFile
	revisionDirectory
	revisionSymlink
	revisionShallowEntry
)

type revisionCaptureMode uint8

const (
	revisionCaptureContent revisionCaptureMode = iota + 1
	revisionCaptureBoundedFile
	revisionCaptureRequiredAbsentEntry
)

// RevisionRequest identifies the boundary observation used to capture a revision.
type RevisionRequest struct {
	Path   string
	Effect PathEffect

	maximumRegularFileBytes int64
	captureMode             revisionCaptureMode
}

// NewBoundedContentRevisionRequest observes complete file or directory content
// under the mutation revision operation limits.
func NewBoundedContentRevisionRequest(path string, effect PathEffect) RevisionRequest {
	return RevisionRequest{
		Path:        path,
		Effect:      effect,
		captureMode: revisionCaptureContent,
	}
}

// NewBoundedFileRevisionRequest observes an absent path or one regular file
// no larger than maximumBytes. Existing directories and special files are
// rejected without traversal.
func NewBoundedFileRevisionRequest(
	maximumBytes int64,
	path string,
	effect PathEffect,
) (RevisionRequest, error) {
	if maximumBytes <= 0 {
		return RevisionRequest{}, fmt.Errorf(
			"bounded mutation revision maximum bytes must be positive",
		)
	}
	return RevisionRequest{
		Path:                    path,
		Effect:                  effect,
		maximumRegularFileBytes: maximumBytes,
		captureMode:             revisionCaptureBoundedFile,
	}, nil
}

// NewRequiredAbsentRevisionRequest observes only the final namespace entry.
// Existing directories and special files are never traversed or opened.
func NewRequiredAbsentRevisionRequest(path string) RevisionRequest {
	return RevisionRequest{
		Path:        path,
		Effect:      PathEffectDirectoryEntry,
		captureMode: revisionCaptureRequiredAbsentEntry,
	}
}

// Equal reports whether two requests ask for the same revision semantics.
func (request RevisionRequest) Equal(other RevisionRequest) bool {
	return request.Path == other.Path &&
		request.Effect == other.Effect &&
		request.maximumRegularFileBytes == other.maximumRegularFileBytes &&
		request.captureMode == other.captureMode
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

func validateRevisionBaseline(request RevisionRequest, revision SnapshotRevision) error {
	if request.captureMode == revisionCaptureRequiredAbsentEntry {
		if revision.kind != revisionAbsent {
			return StaleSnapshotError{}
		}
	}
	return nil
}

type revisionFileCacheEntry struct {
	version  filesnapshot.RegularFileVersion
	revision SnapshotRevision
}

// RevisionObservationPass owns the aggregate budget and file cache for one
// workflow-defined filesystem observation pass. It is sequential and must not
// be shared across concurrent goroutines.
type RevisionObservationPass struct {
	fileCache map[string]revisionFileCacheEntry
	budget    *revisionCaptureBudget
}

// NewRevisionObservationPass starts one bounded mutation revision pass.
func NewRevisionObservationPass() *RevisionObservationPass {
	observation, err := newRevisionObservationPass(defaultRevisionCaptureLimits())
	if err != nil {
		panic(err)
	}
	return observation
}

func newRevisionObservationPass(
	limits revisionCaptureLimits,
) (*RevisionObservationPass, error) {
	budget, err := newRevisionCaptureBudget(limits)
	if err != nil {
		return nil, err
	}
	return &RevisionObservationPass{
		fileCache: make(map[string]revisionFileCacheEntry),
		budget:    budget,
	}, nil
}

// Capture records one request against the pass's remaining aggregate budget.
func (observation *RevisionObservationPass) Capture(
	ctx context.Context,
	request RevisionRequest,
) (SnapshotRevision, error) {
	revision, err := observation.capture(ctx, request)
	if err != nil {
		return SnapshotRevision{}, err
	}
	if err := validateRevisionBaseline(request, revision); err != nil {
		return SnapshotRevision{}, err
	}
	return revision, nil
}

func (observation *RevisionObservationPass) capture(
	ctx context.Context,
	request RevisionRequest,
) (SnapshotRevision, error) {
	if observation == nil || observation.budget == nil || observation.fileCache == nil {
		return SnapshotRevision{}, fmt.Errorf("mutation revision observation pass is not initialized")
	}
	return captureRevision(
		ctx,
		request,
		observation.fileCache,
		observation.budget,
	)
}

func captureRevision(
	ctx context.Context,
	request RevisionRequest,
	fileCache map[string]revisionFileCacheEntry,
	operationBudget *revisionCaptureBudget,
) (SnapshotRevision, error) {
	if ctx == nil {
		return SnapshotRevision{}, fmt.Errorf("mutation revision context is required")
	}
	if err := ctx.Err(); err != nil {
		return SnapshotRevision{}, err
	}
	if request.captureMode == 0 {
		return SnapshotRevision{}, fmt.Errorf("mutation revision request capture mode is required")
	}
	if operationBudget == nil {
		return SnapshotRevision{}, fmt.Errorf("mutation revision operation budget is required")
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
	if request.captureMode == revisionCaptureRequiredAbsentEntry {
		return shallowEntryRevision(identity, info), nil
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
		if err := ctx.Err(); err != nil {
			return SnapshotRevision{}, err
		}
		current, err := os.Lstat(identity.accessPath)
		if err != nil {
			return SnapshotRevision{}, fmt.Errorf("reinspect mutation revision symlink %q: %w", request.Path, err)
		}
		if !sameRevisionEntryVersion(info, current) {
			return SnapshotRevision{}, fmt.Errorf("mutation revision symlink %q changed while reading", request.Path)
		}
		hasher := newRevisionHasher(identity.keyPath, revisionSymlink)
		writeRevisionRecord(hasher, "symlink", target)
		return newSnapshotRevision(revisionSymlink, identity.keyPath, hasher), nil
	case info.Mode().IsRegular():
		if request.captureMode == revisionCaptureBoundedFile &&
			info.Size() > request.maximumRegularFileBytes {
			return SnapshotRevision{}, fmt.Errorf(
				"mutation revision file %q exceeds %d bytes",
				request.Path,
				request.maximumRegularFileBytes,
			)
		}
		if request.captureMode == revisionCaptureBoundedFile {
			version, reusable := filesnapshot.RegularFileVersionOf(info)
			if cached, ok := fileCache[identity.keyPath]; ok &&
				reusable && cached.version.Equal(version) {
				return cached.revision, nil
			}
			return captureBoundedRevisionFile(
				ctx,
				request,
				identity,
				fileCache,
				info,
				operationBudget,
			)
		}
		treeBudget, err := operationBudget.beginTree()
		if err != nil {
			return SnapshotRevision{}, err
		}
		hasher := newRevisionHasher(identity.keyPath, revisionFile)
		if err := hashRevisionFile(
			ctx,
			hasher,
			identity.accessPath,
			".",
			info,
			treeBudget,
		); err != nil {
			return SnapshotRevision{}, err
		}
		return newSnapshotRevision(revisionFile, identity.keyPath, hasher), nil
	case info.IsDir():
		if request.captureMode == revisionCaptureBoundedFile {
			return SnapshotRevision{}, fmt.Errorf(
				"mutation revision path %q must be absent or resolve to a regular file",
				request.Path,
			)
		}
		treeBudget, err := operationBudget.beginTree()
		if err != nil {
			return SnapshotRevision{}, err
		}
		hasher := newRevisionHasher(identity.keyPath, revisionDirectory)
		if err := hashRevisionDirectory(
			ctx,
			hasher,
			identity.accessPath,
			info,
			treeBudget,
		); err != nil {
			return SnapshotRevision{}, err
		}
		return newSnapshotRevision(revisionDirectory, identity.keyPath, hasher), nil
	default:
		return SnapshotRevision{}, fmt.Errorf("mutation revision path %q has unsupported file mode %s", request.Path, info.Mode())
	}
}

func captureBoundedRevisionFile(
	ctx context.Context,
	request RevisionRequest,
	identity canonicalPath,
	fileCache map[string]revisionFileCacheEntry,
	info os.FileInfo,
	operationBudget *revisionCaptureBudget,
) (SnapshotRevision, error) {
	treeBudget, err := operationBudget.beginTree()
	if err != nil {
		return SnapshotRevision{}, err
	}
	if info.Size() > treeBudget.remainingBytes() {
		return SnapshotRevision{}, RevisionLimitError{
			scope: revisionLimitOperation, resource: revisionLimitBytes,
			limit:    operationBudget.limits.maximumOperationBytes,
			observed: operationBudget.bytes + info.Size(),
		}
	}
	if err := treeBudget.admitBytes(info.Size()); err != nil {
		return SnapshotRevision{}, err
	}
	readMaximum := info.Size()
	if readMaximum == 0 {
		readMaximum = 1
	}
	readMaximum = min(readMaximum, request.maximumRegularFileBytes)
	snapshot, exists, err := filesnapshot.ReadRegularFileSnapshotContext(
		ctx,
		identity.accessPath,
		readMaximum,
	)
	if err != nil {
		return SnapshotRevision{}, fmt.Errorf(
			"read bounded mutation revision file %q: %w",
			request.Path,
			err,
		)
	}
	if !exists {
		return SnapshotRevision{}, fmt.Errorf(
			"read bounded mutation revision file %q: %w",
			request.Path,
			filesnapshot.ErrChanged,
		)
	}
	beforeVersion, beforeReusable := filesnapshot.RegularFileVersionOf(info)
	afterVersion, afterReusable := snapshot.FileVersion()
	if beforeReusable || afterReusable {
		if !beforeReusable || !afterReusable || !beforeVersion.Equal(afterVersion) {
			return SnapshotRevision{}, fmt.Errorf(
				"read bounded mutation revision file %q: %w",
				request.Path,
				filesnapshot.ErrChanged,
			)
		}
	}
	hasher := newRevisionHasher(identity.keyPath, revisionFile)
	writeRevisionRecord(
		hasher,
		"stable-regular-file-snapshot",
		snapshot.Revision(),
	)
	revision := newSnapshotRevision(revisionFile, identity.keyPath, hasher)
	if fileCache != nil {
		if version, reusable := snapshot.FileVersion(); reusable {
			fileCache[identity.keyPath] = revisionFileCacheEntry{
				version:  version,
				revision: revision,
			}
		}
	}
	return revision, nil
}

func shallowEntryRevision(identity canonicalPath, info os.FileInfo) SnapshotRevision {
	hasher := newRevisionHasher(identity.keyPath, revisionShallowEntry)
	writeRevisionRecord(hasher, "mode", info.Mode().String())
	return newSnapshotRevision(revisionShallowEntry, identity.keyPath, hasher)
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

func hashRevisionDirectory(
	ctx context.Context,
	hasher hash.Hash,
	root string,
	rootInfo os.FileInfo,
	budget *revisionTreeBudget,
) (resultErr error) {
	directory, err := openRevisionDirectory(root)
	if err != nil {
		return fmt.Errorf("open mutation revision directory %q: %w", root, err)
	}
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	openedInfo, err := directory.Stat()
	if err != nil {
		return fmt.Errorf("inspect open mutation revision directory %q: %w", root, err)
	}
	if !sameRevisionEntryVersion(rootInfo, openedInfo) {
		return fmt.Errorf("mutation revision directory %q changed while opening", root)
	}
	if err := hashRevisionDirectoryEntries(ctx, hasher, directory, ".", 0, budget); err != nil {
		return err
	}
	afterOpen, err := directory.Stat()
	if err != nil {
		return fmt.Errorf("reinspect open mutation revision directory %q: %w", root, err)
	}
	afterPath, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("reinspect mutation revision directory %q: %w", root, err)
	}
	if !sameRevisionEntryVersion(openedInfo, afterOpen) ||
		!sameRevisionEntryVersion(openedInfo, afterPath) {
		return fmt.Errorf("mutation revision directory %q changed while reading", root)
	}
	return nil
}

func hashRevisionDirectoryEntries(
	ctx context.Context,
	hasher hash.Hash,
	directory *os.File,
	relativeRoot string,
	directoryDepth int,
	budget *revisionTreeBudget,
) error {
	names, err := readRevisionDirectoryNames(
		ctx,
		directory,
		budget.remainingEntries(),
	)
	if err != nil {
		return err
	}
	if len(names) > budget.remainingEntries() {
		return budget.admitEntries(len(names))
	}
	if err := budget.admitEntries(len(names)); err != nil {
		return err
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		relative := name
		if relativeRoot != "." {
			relative = filepath.Join(relativeRoot, name)
		}
		relative = filepath.ToSlash(relative)
		entry, err := observeRevisionChild(directory, name)
		if err != nil {
			return fmt.Errorf("inspect mutation revision entry %q: %w", relative, err)
		}
		switch {
		case entry.isSymlink():
			target, err := readRevisionSymlink(directory, name, entry)
			if err != nil {
				return fmt.Errorf("read mutation revision symlink %q: %w", relative, err)
			}
			writeRevisionRecord(hasher, "symlink", relative, target)
		case entry.isDirectory():
			childDepth := directoryDepth + 1
			if err := budget.admitDirectoryDepth(childDepth); err != nil {
				return err
			}
			writeRevisionRecord(hasher, "directory", relative)
			child, err := openRevisionChild(directory, name, entry)
			if err != nil {
				return fmt.Errorf("open mutation revision directory %q: %w", relative, err)
			}
			if err := hashRevisionDirectoryEntries(
				ctx,
				hasher,
				child,
				relative,
				childDepth,
				budget,
			); err != nil {
				return errors.Join(err, child.Close())
			}
			if err := verifyRevisionChild(directory, name, child, entry); err != nil {
				return errors.Join(err, child.Close())
			}
			if err := child.Close(); err != nil {
				return err
			}
		case entry.isRegular():
			child, err := openRevisionChild(directory, name, entry)
			if err != nil {
				return fmt.Errorf("open mutation revision file %q: %w", relative, err)
			}
			if err := hashRevisionOpenedFile(ctx, hasher, child, relative, entry, budget); err != nil {
				return errors.Join(err, child.Close())
			}
			if err := verifyRevisionChild(directory, name, child, entry); err != nil {
				return errors.Join(err, child.Close())
			}
			if err := child.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("mutation revision entry %q has unsupported file mode", relative)
		}
	}
	return nil
}

func readRevisionDirectoryNames(
	ctx context.Context,
	directory *os.File,
	maximumEntries int,
) ([]string, error) {
	const batchMaximum = 256

	if maximumEntries < 0 {
		return nil, fmt.Errorf("mutation revision directory entry capacity is exhausted")
	}
	names := make([]string, 0, min(maximumEntries+1, batchMaximum))
	for len(names) <= maximumEntries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := maximumEntries + 1 - len(names)
		batch, readErr := directory.Readdirnames(min(remaining, batchMaximum))
		names = append(names, batch...)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("enumerate mutation revision directory: %w", readErr)
		}
		if len(batch) == 0 {
			return nil, fmt.Errorf("enumerate mutation revision directory: no progress")
		}
	}
	slices.Sort(names)
	return names, nil
}

func hashRevisionOpenedFile(
	ctx context.Context,
	hasher hash.Hash,
	file *os.File,
	relative string,
	entry revisionNativeEntry,
	budget *revisionTreeBudget,
) error {
	if err := budget.admitBytes(entry.size); err != nil {
		return err
	}
	executable := "not-executable"
	if entry.executable() {
		executable = "executable"
	}
	writeRevisionRecord(
		hasher,
		"file",
		filepath.ToSlash(relative),
		executable,
		strconv.FormatInt(entry.size, 10),
	)
	reader := &revisionContextReader{ctx: ctx, reader: file}
	if _, err := io.CopyN(hasher, reader, entry.size); err != nil {
		return fmt.Errorf("read mutation revision file %q: %w", relative, err)
	}
	current, err := observeRevisionOpened(file)
	if err != nil {
		return fmt.Errorf("reinspect mutation revision file %q: %w", relative, err)
	}
	if !entry.identity.equal(current.identity) {
		return fmt.Errorf("mutation revision file %q changed while reading", relative)
	}
	writeRevisionRecord(hasher, "end-file")
	return nil
}

func hashRevisionFile(
	ctx context.Context,
	hasher hash.Hash,
	path string,
	relative string,
	info os.FileInfo,
	budget *revisionTreeBudget,
) error {
	file, err := openRevisionRegularFile(path)
	if err != nil {
		return fmt.Errorf("open mutation revision file %q: %w", path, err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect open mutation revision file %q: %w", path, err)
	}
	if !opened.Mode().IsRegular() || !sameRevisionFileVersion(info, opened) {
		return fmt.Errorf("mutation revision file %q changed while opening", path)
	}
	if budget == nil {
		return fmt.Errorf("mutation revision file budget is required")
	}
	if err := budget.admitBytes(opened.Size()); err != nil {
		return err
	}
	executable := "not-executable"
	if opened.Mode().Perm()&0o111 != 0 {
		executable = "executable"
	}
	writeRevisionRecord(hasher, "file", filepath.ToSlash(relative), executable, strconv.FormatInt(opened.Size(), 10))
	reader := &revisionContextReader{ctx: ctx, reader: file}
	if _, err := io.CopyN(hasher, reader, opened.Size()); err != nil {
		return fmt.Errorf("read mutation revision file %q: %w", path, err)
	}
	afterOpen, err := file.Stat()
	if err != nil {
		return fmt.Errorf("reinspect open mutation revision file %q: %w", path, err)
	}
	afterPath, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("reinspect mutation revision file %q: %w", path, err)
	}
	if !sameRevisionFileVersion(opened, afterOpen) ||
		!sameRevisionFileVersion(opened, afterPath) {
		return fmt.Errorf("mutation revision file %q changed while reading", path)
	}
	writeRevisionRecord(hasher, "end-file")
	return nil
}

func sameRevisionFileVersion(left os.FileInfo, right os.FileInfo) bool {
	if left == nil || right == nil || !left.Mode().IsRegular() || !right.Mode().IsRegular() ||
		!os.SameFile(left, right) {
		return false
	}
	leftVersion, leftOK := filesnapshot.RegularFileVersionOf(left)
	rightVersion, rightOK := filesnapshot.RegularFileVersionOf(right)
	return leftOK && rightOK && leftVersion.Equal(rightVersion)
}

func sameRevisionEntryVersion(left os.FileInfo, right os.FileInfo) bool {
	leftNative, leftOK := revisionNativeEntryFromFileInfo(left)
	rightNative, rightOK := revisionNativeEntryFromFileInfo(right)
	return leftOK && rightOK && leftNative.identity.equal(rightNative.identity)
}

type revisionContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *revisionContextReader) Read(payload []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(payload)
}

func writeRevisionRecord(hasher hash.Hash, fields ...string) {
	for _, field := range fields {
		_, _ = hasher.Write([]byte(strconv.Itoa(len(field))))
		_, _ = hasher.Write([]byte(":"))
		_, _ = hasher.Write([]byte(field))
	}
	_, _ = hasher.Write([]byte("\n"))
}
