package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/supply/artifact"
)

type rootedEntryState uint8

const (
	rootedEntryUnknown rootedEntryState = iota
	rootedEntryMissing
	rootedEntryValid
	rootedEntryOwnedInvalid
)

func rootedEntryDestination(
	root *rootedpath.CapturedRoot,
	relativeRoot string,
) (rootedpath.Destination, string, error) {
	if root == nil {
		return rootedpath.Destination{}, "", fmt.Errorf("cache root authority is required")
	}
	relative, err := rootedpath.NewRelativeDestination(relativeRoot)
	if err != nil {
		return rootedpath.Destination{}, "", fmt.Errorf("validate rooted cache entry path: %w", err)
	}
	authority, err := root.Authority()
	if err != nil {
		return rootedpath.Destination{}, "", err
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		return rootedpath.Destination{}, "", err
	}
	lexicalPath, err := destination.LexicalPath()
	if err != nil {
		return rootedpath.Destination{}, "", err
	}
	return destination, lexicalPath, nil
}

func verifyRootedDirectory(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	relativeRoot string,
	spec EntrySpec,
) (rootedEntryState, artifact.ContentHash, artifact.ArtifactKind, error) {
	if err := validateContext(ctx, "rooted entry verification"); err != nil {
		return rootedEntryUnknown, "", "", err
	}
	if err := spec.validate(); err != nil {
		return rootedEntryUnknown, "", "", err
	}
	destination, lexicalPath, err := rootedEntryDestination(root, relativeRoot)
	if err != nil {
		return rootedEntryUnknown, "", "", err
	}
	capability, err := root.Acquire(destination)
	if err != nil {
		return rootedEntryUnknown, "", "", err
	}
	defer capability.Close()

	sink := newRootedEntryVerificationSink(ctx, spec)
	_, err = storagecommit.SnapshotRootedDirectory(
		ctx,
		capability,
		cacheEnvelopeTraversalLimits(),
		sink,
	)
	if err != nil {
		if fsErr := ctx.Err(); fsErr != nil {
			return rootedEntryUnknown, "", "", fsErr
		}
		if isMissingRootedEntry(err) {
			return rootedEntryMissing, "", "", nil
		}
		return rootedEntryUnknown, "", "", fmt.Errorf(
			"verify rooted cache entry %q: %w",
			lexicalPath,
			err,
		)
	}
	owned, hash, kind, verifyErr := sink.result()
	if verifyErr == nil {
		return rootedEntryValid, hash, kind, nil
	}
	if owned {
		return rootedEntryOwnedInvalid, "", "", invalidEntry(lexicalPath, verifyErr.Error())
	}
	return rootedEntryUnknown, "", "", invalidEntry(lexicalPath, verifyErr.Error())
}

// VerifyDirectoryRooted verifies one cache entry below a retained cache root.
// Missing entries return valid=false without error.
func VerifyDirectoryRooted(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	relativeRoot string,
	spec EntrySpec,
) (bool, error) {
	state, _, _, err := verifyRootedDirectory(ctx, root, relativeRoot, spec)
	return state == rootedEntryValid, err
}

func isMissingRootedEntry(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

func retireRootedDirectory(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	relativeRoot string,
) error {
	destination, lexicalPath, err := rootedEntryDestination(root, relativeRoot)
	if err != nil {
		return err
	}
	capability, err := root.Acquire(destination)
	if err != nil {
		return err
	}
	identity, err := storagecommit.CaptureRootedEntryIdentity(ctx, capability)
	if err != nil {
		_ = capability.Close()
		if isMissingRootedEntry(err) {
			return nil
		}
		return fmt.Errorf("capture rooted cache entry %q for retirement: %w", lexicalPath, err)
	}
	request, err := storagecommit.NewRootedLogicalRemoval(capability, identity)
	if err != nil {
		_ = capability.Close()
		return fmt.Errorf("construct rooted cache entry retirement for %q: %w", lexicalPath, err)
	}
	if err := storagecommit.CommitLogicalRemoval(ctx, request); err != nil {
		return fmt.Errorf("retire rooted cache entry %q: %w", lexicalPath, err)
	}
	return nil
}

// RetireDirectoryRooted identity-guards removal of one cache entry below a
// retained cache root. The caller must hold the entry's exact advisory lock.
func RetireDirectoryRooted(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	relativeRoot string,
) error {
	if err := validateContext(ctx, "rooted cache entry retirement"); err != nil {
		return err
	}
	return retireRootedDirectory(ctx, root, relativeRoot)
}

// VerifiedFileLimitError reports a rooted cache file whose observed size
// exceeds the caller's read or verification ceiling.
type VerifiedFileLimitError struct {
	maximum  int64
	observed int64
}

func (err *VerifiedFileLimitError) Error() string {
	if err == nil {
		return "rooted verified cache file exceeds its byte limit"
	}
	return fmt.Sprintf(
		"rooted verified cache file exceeds its byte limit: observed=%d limit=%d",
		err.observed,
		err.maximum,
	)
}

// Observed returns the file size that exceeded the ceiling.
func (err *VerifiedFileLimitError) Observed() int64 {
	if err == nil {
		return 0
	}
	return err.observed
}

// ReadVerifiedFileRooted returns one bounded file snapshot only when its
// containing entry verifies below the same retained cache root. Missing entries
// return found=false without error.
func ReadVerifiedFileRooted(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	relativeRoot string,
	spec EntrySpec,
	maximumBytes int,
) (VerifiedFile, bool, error) {
	return readVerifiedFileRooted(ctx, root, relativeRoot, spec, maximumBytes, true)
}

// VerifyFileRooted verifies one bounded file-backed cache entry without
// retaining its payload bytes. Missing entries return valid=false without error.
func VerifyFileRooted(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	relativeRoot string,
	spec EntrySpec,
	maximumBytes int,
) (bool, error) {
	_, valid, err := readVerifiedFileRooted(
		ctx,
		root,
		relativeRoot,
		spec,
		maximumBytes,
		false,
	)
	return valid, err
}

func readVerifiedFileRooted(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	relativeRoot string,
	spec EntrySpec,
	maximumBytes int,
	captureContent bool,
) (VerifiedFile, bool, error) {
	if err := validateContext(ctx, "rooted verified file read"); err != nil {
		return VerifiedFile{}, false, err
	}
	if maximumBytes <= 0 {
		return VerifiedFile{}, false, fmt.Errorf("verified cache file maximum bytes must be positive")
	}
	if err := spec.validate(); err != nil {
		return VerifiedFile{}, false, err
	}
	destination, lexicalPath, err := rootedEntryDestination(root, relativeRoot)
	if err != nil {
		return VerifiedFile{}, false, err
	}
	capability, err := root.Acquire(destination)
	if err != nil {
		return VerifiedFile{}, false, err
	}
	defer capability.Close()

	sink := newRootedVerifiedFileSink(ctx, spec, maximumBytes, captureContent)
	_, err = storagecommit.SnapshotRootedDirectory(
		ctx,
		capability,
		cacheEnvelopeTraversalLimits(),
		sink,
	)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return VerifiedFile{}, false, contextErr
		}
		if isMissingRootedEntry(err) {
			return VerifiedFile{}, false, nil
		}
		return VerifiedFile{}, false, fmt.Errorf(
			"read rooted cache file from %q: %w",
			lexicalPath,
			err,
		)
	}
	file, verifyErr := sink.result()
	if verifyErr != nil {
		return VerifiedFile{}, false, invalidEntry(lexicalPath, verifyErr.Error())
	}
	return file, true, nil
}

type rootedEntryVerificationSink struct {
	content *rootedContentHashSink
	spec    EntrySpec

	record        []byte
	recordMode    fs.FileMode
	recordPresent bool
	recordFailure error
}

type rootedVerifiedFileSink struct {
	entry        *rootedEntryVerificationSink
	contentPath  string
	maximumBytes int
	capture      bool

	content []byte
	mode    fs.FileMode
	seen    bool
	failure error
}

func newRootedVerifiedFileSink(
	ctx context.Context,
	spec EntrySpec,
	maximumBytes int,
	capture bool,
) *rootedVerifiedFileSink {
	return &rootedVerifiedFileSink{
		entry:        newRootedEntryVerificationSink(ctx, spec),
		contentPath:  spec.contentPath,
		maximumBytes: maximumBytes,
		capture:      capture,
	}
}

func (sink *rootedVerifiedFileSink) VisitRoot(mode fs.FileMode) error {
	return sink.entry.VisitRoot(mode)
}

func (sink *rootedVerifiedFileSink) VisitDirectory(
	relative mutationfs.TreeRelativePath,
	mode fs.FileMode,
) error {
	return sink.entry.VisitDirectory(relative, mode)
}

func (sink *rootedVerifiedFileSink) VisitRegularFile(
	relative mutationfs.TreeRelativePath,
	mode fs.FileMode,
	size int64,
	content io.Reader,
) error {
	if relative.Path() != sink.contentPath {
		return sink.entry.VisitRegularFile(relative, mode, size, content)
	}
	if sink.seen {
		sink.failure = fmt.Errorf("cache file %q appears more than once", sink.contentPath)
	}
	sink.seen = true
	sink.mode = mode.Perm()
	if size > int64(sink.maximumBytes) {
		return &VerifiedFileLimitError{
			maximum:  int64(sink.maximumBytes),
			observed: size,
		}
	}
	if !sink.capture {
		return sink.entry.VisitRegularFile(relative, mode, size, content)
	}

	var captured bytes.Buffer
	if err := sink.entry.VisitRegularFile(
		relative,
		mode,
		size,
		io.TeeReader(content, &captured),
	); err != nil {
		return err
	}
	if int64(captured.Len()) != size {
		return fmt.Errorf(
			"captured cache file %q has %d bytes, want %d",
			sink.contentPath,
			captured.Len(),
			size,
		)
	}
	sink.content = captured.Bytes()
	return nil
}

func (sink *rootedVerifiedFileSink) result() (VerifiedFile, error) {
	_, _, kind, err := sink.entry.result()
	if err != nil {
		return VerifiedFile{}, err
	}
	if sink.failure != nil {
		return VerifiedFile{}, sink.failure
	}
	if !sink.seen || kind != artifact.ArtifactKindFile {
		return VerifiedFile{}, fmt.Errorf("cache content path %q is not a regular file", sink.contentPath)
	}
	return VerifiedFile{
		content: bytes.Clone(sink.content),
		mode:    sink.mode,
	}, nil
}

func newRootedEntryVerificationSink(
	ctx context.Context,
	spec EntrySpec,
) *rootedEntryVerificationSink {
	return &rootedEntryVerificationSink{
		content: newRootedContentHashSink(ctx, spec.contentPath),
		spec:    spec,
	}
}

func (sink *rootedEntryVerificationSink) VisitRoot(mode fs.FileMode) error {
	return sink.content.VisitRoot(mode)
}

func (sink *rootedEntryVerificationSink) VisitDirectory(
	relative mutationfs.TreeRelativePath,
	mode fs.FileMode,
) error {
	if relative.Path() == completionRecordName {
		sink.recordFailure = fmt.Errorf("completion record is not a regular file")
		return nil
	}
	return sink.content.VisitDirectory(relative, mode)
}

func (sink *rootedEntryVerificationSink) VisitRegularFile(
	relative mutationfs.TreeRelativePath,
	mode fs.FileMode,
	size int64,
	content io.Reader,
) error {
	if relative.Path() != completionRecordName {
		return sink.content.VisitRegularFile(relative, mode, size, content)
	}
	if sink.recordPresent {
		sink.recordFailure = fmt.Errorf("completion record appears more than once")
	}
	sink.recordPresent = true
	sink.recordMode = mode.Perm()
	limited := io.LimitReader(content, maximumCompletionBytes+1)
	record, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if _, err := io.Copy(io.Discard, content); err != nil {
		return err
	}
	if len(record) > maximumCompletionBytes {
		sink.recordFailure = fmt.Errorf("completion record exceeds %d bytes", maximumCompletionBytes)
		return nil
	}
	sink.record = record
	return nil
}

func (sink *rootedEntryVerificationSink) result() (
	bool,
	artifact.ContentHash,
	artifact.ArtifactKind,
	error,
) {
	if !sink.recordPresent {
		return false, "", "", fmt.Errorf("completion record is missing")
	}
	if sink.recordFailure != nil {
		return false, "", "", sink.recordFailure
	}
	if sink.recordMode != 0o600 {
		return false, "", "", fmt.Errorf(
			"completion record mode is %04o, want 0600",
			sink.recordMode,
		)
	}
	record, err := decodeCompletionRecord(sink.record)
	if err != nil {
		return false, "", "", err
	}
	canonical, err := encodeCompletionRecord(record)
	if err != nil {
		return false, "", "", err
	}
	if !bytes.Equal(sink.record, canonical) {
		return false, "", "", fmt.Errorf("completion record is not canonical")
	}
	if err := record.validateOwnership(sink.spec); err != nil {
		return false, "", "", err
	}
	if err := record.validate(sink.spec); err != nil {
		return true, "", "", err
	}

	contentHash, contentKind, err := sink.content.result()
	if err != nil {
		return true, "", "", err
	}
	if contentHash != record.ContentHash || contentKind != record.Kind {
		return true, "", "", fmt.Errorf(
			"cached content identity %q/%q does not match completion record %q/%q",
			contentHash,
			contentKind,
			record.ContentHash,
			record.Kind,
		)
	}
	return true, contentHash, contentKind, nil
}

type rootedContentHashSink struct {
	ctx         context.Context
	contentPath string
	kind        artifact.ArtifactKind
	hash        artifact.ContentHash
	builder     *artifact.DirectoryHashBuilder
	seen        bool
}

func newRootedContentHashSink(
	ctx context.Context,
	contentPath string,
) *rootedContentHashSink {
	return &rootedContentHashSink{ctx: ctx, contentPath: contentPath}
}

func (sink *rootedContentHashSink) VisitRoot(fs.FileMode) error {
	return nil
}

func (sink *rootedContentHashSink) VisitDirectory(
	relative mutationfs.TreeRelativePath,
	_ fs.FileMode,
) error {
	relativePath := relative.Path()
	switch {
	case relativePath == sink.contentPath:
		if sink.seen {
			return fmt.Errorf("cache content path %q appears more than once", sink.contentPath)
		}
		sink.seen = true
		sink.kind = artifact.ArtifactKindDirectory
		sink.builder = artifact.NewDirectoryHashBuilder()
	case sink.builder != nil && isContentDescendant(sink.contentPath, relativePath):
		return sink.builder.AddDirectory(strings.TrimPrefix(relativePath, sink.contentPath+"/"))
	}
	return nil
}

func (sink *rootedContentHashSink) VisitRegularFile(
	relative mutationfs.TreeRelativePath,
	mode fs.FileMode,
	size int64,
	content io.Reader,
) error {
	relativePath := relative.Path()
	executable := mode.Perm()&0o111 != 0
	switch {
	case relativePath == sink.contentPath:
		if sink.seen {
			return fmt.Errorf("cache content path %q appears more than once", sink.contentPath)
		}
		sink.seen = true
		sink.kind = artifact.ArtifactKindFile
		hash, err := artifact.HashFileReader(sink.ctx, content, size, executable)
		if err != nil {
			return err
		}
		sink.hash = hash
		return nil
	case sink.builder != nil && isContentDescendant(sink.contentPath, relativePath):
		return sink.builder.AddFile(
			sink.ctx,
			strings.TrimPrefix(relativePath, sink.contentPath+"/"),
			executable,
			size,
			content,
		)
	default:
		_, err := io.Copy(io.Discard, content)
		return err
	}
}

func (sink *rootedContentHashSink) result() (
	artifact.ContentHash,
	artifact.ArtifactKind,
	error,
) {
	if !sink.seen {
		return "", "", fmt.Errorf("cache content path %q is missing", sink.contentPath)
	}
	if sink.kind == artifact.ArtifactKindDirectory {
		hash, err := sink.builder.Sum()
		if err != nil {
			return "", "", err
		}
		sink.hash = hash
	}
	if err := validateContentIdentity(sink.hash, sink.kind); err != nil {
		return "", "", err
	}
	return sink.hash, sink.kind, nil
}

func isContentDescendant(contentPath string, candidate string) bool {
	return len(candidate) > len(contentPath) &&
		candidate[:len(contentPath)] == contentPath &&
		candidate[len(contentPath)] == '/'
}
