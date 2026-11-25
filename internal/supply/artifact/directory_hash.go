package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"path"
	"strconv"
	"strings"
)

// DirectoryHashBuilder computes the canonical directory hash from a stable,
// depth-first component-lexical entry stream. It owns hash semantics, not
// filesystem traversal. Sibling components use exact Go string order, and a
// directory precedes every descendant.
type DirectoryHashBuilder struct {
	hasher               hash.Hash
	installed            hash.Hash
	legacyCandidate      hash.Hash
	candidateExecutables ExecutablePaths
	observedExecutables  ExecutablePaths
	previousPath         string
	directories          map[string]struct{}
	result               ContentHash
	failure              error
}

// ExecutablePaths supplies only the executable labels for a candidate hash-v1
// tree. A match still requires every path, kind and byte in the observed tree.
type ExecutablePaths map[string]bool

func NewLegacyTreeEvidenceBuilder(candidate, collect ExecutablePaths) *DirectoryHashBuilder {
	b := NewDirectoryEvidenceBuilder()
	b.observedExecutables = collect
	if candidate != nil {
		b.candidateExecutables = candidate
		b.legacyCandidate = sha256.New()
		writeRecord(b.legacyCandidate, hashFormatVersion)
	}
	return b
}

// NewDirectoryHashBuilder starts an empty canonical directory hash.
func NewDirectoryHashBuilder() *DirectoryHashBuilder {
	hasher := sha256.New()
	writeRecord(hasher, hashFormatVersion)
	return &DirectoryHashBuilder{
		hasher:      hasher,
		directories: map[string]struct{}{".": {}},
	}
}

// NewDirectoryEvidenceBuilder also computes the installation baseline in the
// same traversal. Source-only consumers do not pay for this second digest.
func NewDirectoryEvidenceBuilder() *DirectoryHashBuilder {
	b := NewDirectoryHashBuilder()
	b.installed = beginInstalled("directory")
	return b
}

func (builder *DirectoryHashBuilder) Hashes() (PathHashes, error) {
	source, err := builder.Sum()
	if err != nil {
		return PathHashes{}, err
	}
	if builder.installed == nil {
		return PathHashes{}, fmt.Errorf("directory has no installed hash")
	}
	hashes := PathHashes{Source: source, Installed: finishInstalled(builder.installed)}
	if builder.legacyCandidate != nil {
		hashes.Alternate = ContentHash(hashAlgorithm + ":" + hex.EncodeToString(builder.legacyCandidate.Sum(nil)))
	}
	return hashes, nil
}

// AddDirectory appends one directory entry in depth-first component-lexical order.
func (builder *DirectoryHashBuilder) AddDirectory(relativePath string) error {
	if err := builder.admit(relativePath); err != nil {
		return err
	}
	writeRecord(builder.hasher, "dir", relativePath)
	if builder.legacyCandidate != nil {
		writeRecord(builder.legacyCandidate, "dir", relativePath)
	}
	if builder.installed != nil {
		writeRecord(builder.installed, "dir", relativePath)
	}
	builder.directories[relativePath] = struct{}{}
	return nil
}

// AddFile appends one complete regular-file entry. The reader is consumed
// synchronously and must contain exactly size bytes.
func (builder *DirectoryHashBuilder) AddFile(
	ctx context.Context,
	relativePath string,
	executable bool,
	size int64,
	content io.Reader,
) error {
	if ctx == nil {
		return fmt.Errorf("directory hash file %q context is required", relativePath)
	}
	if size < 0 {
		return fmt.Errorf("directory hash file %q has negative size", relativePath)
	}
	if content == nil {
		return fmt.Errorf("directory hash file %q content is required", relativePath)
	}
	if err := builder.admit(relativePath); err != nil {
		return err
	}
	writeRecord(builder.hasher, "file", relativePath, executableLabel(executable), strconv.FormatInt(size, 10))

	reader := contextHashReader{ctx: ctx, reader: content}
	var destination io.Writer = builder.hasher
	if builder.installed != nil {
		writeRecord(builder.installed, "file", relativePath, strconv.FormatInt(size, 10))
		destination = io.MultiWriter(builder.hasher, builder.installed)
	}
	if builder.observedExecutables != nil && executable {
		builder.observedExecutables[relativePath] = true
	}
	if builder.legacyCandidate != nil {
		writeRecord(builder.legacyCandidate, "file", relativePath, executableLabel(builder.candidateExecutables[relativePath]), strconv.FormatInt(size, 10))
		destination = io.MultiWriter(destination, builder.legacyCandidate)
	}
	written, err := io.CopyN(destination, reader, size)
	if err != nil {
		return builder.fail(fmt.Errorf("hash directory file %q after %d bytes: %w", relativePath, written, err))
	}
	var extra [1]byte
	count, readErr := reader.Read(extra[:])
	if count != 0 || readErr != io.EOF {
		if readErr == nil {
			readErr = fmt.Errorf("content exceeds declared size %d", size)
		}
		return builder.fail(fmt.Errorf("hash directory file %q: %w", relativePath, readErr))
	}
	writeRecord(builder.hasher, "endfile")
	if builder.legacyCandidate != nil {
		writeRecord(builder.legacyCandidate, "endfile")
	}
	return nil
}

// Sum finalizes and returns the canonical directory hash. It is idempotent;
// no entry may be added after finalization.
func (builder *DirectoryHashBuilder) Sum() (ContentHash, error) {
	if builder == nil || builder.hasher == nil {
		return "", fmt.Errorf("directory hash builder is not initialized")
	}
	if builder.failure != nil {
		return "", fmt.Errorf("directory hash builder failed: %w", builder.failure)
	}
	if builder.result == "" {
		builder.result = ContentHash(hashAlgorithm + ":" + hex.EncodeToString(builder.hasher.Sum(nil)))
	}
	return builder.result, nil
}

func (builder *DirectoryHashBuilder) admit(relativePath string) error {
	if builder == nil || builder.hasher == nil || builder.directories == nil {
		return fmt.Errorf("directory hash builder is not initialized")
	}
	if builder.failure != nil {
		return fmt.Errorf("directory hash builder failed: %w", builder.failure)
	}
	if builder.result != "" {
		return fmt.Errorf("directory hash builder is finalized")
	}
	if relativePath == "" || relativePath == "." || strings.ContainsRune(relativePath, '\x00') ||
		strings.HasPrefix(relativePath, "/") || path.Clean(relativePath) != relativePath {
		return fmt.Errorf("directory hash path %q is not canonical", relativePath)
	}
	for component := range strings.SplitSeq(relativePath, "/") {
		if component == "" || component == "." || component == ".." {
			return fmt.Errorf("directory hash path %q is not canonical", relativePath)
		}
	}
	if builder.previousPath != "" && compareDirectoryHashPaths(relativePath, builder.previousPath) <= 0 {
		return fmt.Errorf(
			"directory hash path %q is not after %q in component-lexical preorder",
			relativePath,
			builder.previousPath,
		)
	}
	if _, ok := builder.directories[path.Dir(relativePath)]; !ok {
		return fmt.Errorf("directory hash path %q has no admitted parent directory", relativePath)
	}
	builder.previousPath = relativePath
	return nil
}

func compareDirectoryHashPaths(left string, right string) int {
	for {
		leftComponent, leftRemainder, leftHasRemainder := strings.Cut(left, "/")
		rightComponent, rightRemainder, rightHasRemainder := strings.Cut(right, "/")
		if order := strings.Compare(leftComponent, rightComponent); order != 0 {
			return order
		}
		switch {
		case !leftHasRemainder && !rightHasRemainder:
			return 0
		case !leftHasRemainder:
			return -1
		case !rightHasRemainder:
			return 1
		default:
			left = leftRemainder
			right = rightRemainder
		}
	}
}

func (builder *DirectoryHashBuilder) fail(err error) error {
	builder.failure = err
	return err
}

type contextHashReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextHashReader) Read(payload []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(payload)
}
