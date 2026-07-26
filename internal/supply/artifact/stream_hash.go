package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
)

// HashFileReader computes hash-v1 for exactly size bytes from a regular file.
// It owns hash semantics only; callers own filesystem traversal and lifecycle.
func HashFileReader(
	ctx context.Context,
	content io.Reader,
	size int64,
	executable bool,
) (ContentHash, error) {
	if ctx == nil {
		return "", fmt.Errorf("file hash context is required")
	}
	if content == nil {
		return "", fmt.Errorf("file hash content is required")
	}
	if size < 0 {
		return "", fmt.Errorf("file hash size must be non-negative")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	hasher := sha256.New()
	writeRecord(hasher, hashFormatVersion)
	writeRecord(hasher, "file", ".", executableLabel(executable), strconv.FormatInt(size, 10))
	reader := contextHashReader{ctx: ctx, reader: content}
	written, err := io.CopyN(hasher, reader, size)
	if err != nil {
		return "", fmt.Errorf("hash file after %d bytes: %w", written, err)
	}
	var extra [1]byte
	count, readErr := reader.Read(extra[:])
	if count != 0 || readErr != io.EOF {
		if readErr == nil {
			readErr = fmt.Errorf("content exceeds declared size %d", size)
		}
		return "", fmt.Errorf("hash file: %w", readErr)
	}
	writeRecord(hasher, "endfile")
	return ContentHash(hashAlgorithm + ":" + hex.EncodeToString(hasher.Sum(nil))), nil
}
