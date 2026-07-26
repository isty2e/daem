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
// depth-first lexical entry stream. It owns hash semantics, not filesystem traversal.
type DirectoryHashBuilder struct {
	hasher       hash.Hash
	previousPath string
	directories  map[string]struct{}
	result       ContentHash
	failure      error
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

// AddDirectory appends one directory entry in depth-first lexical order.
func (builder *DirectoryHashBuilder) AddDirectory(relativePath string) error {
	if err := builder.admit(relativePath); err != nil {
		return err
	}
	writeRecord(builder.hasher, "dir", relativePath)
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
	written, err := io.CopyN(builder.hasher, reader, size)
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
	if builder.previousPath != "" && relativePath <= builder.previousPath {
		return fmt.Errorf("directory hash path %q is not after %q", relativePath, builder.previousPath)
	}
	if _, ok := builder.directories[path.Dir(relativePath)]; !ok {
		return fmt.Errorf("directory hash path %q has no admitted parent directory", relativePath)
	}
	builder.previousPath = relativePath
	return nil
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
