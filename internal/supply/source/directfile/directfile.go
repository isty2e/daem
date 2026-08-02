// Package directfile owns the resource budget for directly materialized
// regular-file sources. Archive extraction and directory traversal retain
// independent policies.
package directfile

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

// MaximumBytes is the maximum admitted size of one directly materialized
// regular-file source.
const MaximumBytes int64 = 128 << 20

// ErrLimitExceeded classifies direct-file resource exhaustion.
var ErrLimitExceeded = errors.New("direct file limit exceeded")

// LimitError reports the first byte count known to exceed the direct-file
// policy.
type LimitError struct {
	limit    int64
	observed int64
}

func (err *LimitError) Error() string {
	if err == nil {
		return ErrLimitExceeded.Error()
	}
	return fmt.Sprintf("%s: observed=%d limit=%d", ErrLimitExceeded, err.observed, err.limit)
}

func (err *LimitError) Unwrap() error {
	return ErrLimitExceeded
}

// Limit returns the maximum admitted direct-file byte count.
func (err *LimitError) Limit() int64 {
	if err == nil {
		return 0
	}
	return err.limit
}

// Observed returns the first byte count known to exceed the limit.
func (err *LimitError) Observed() int64 {
	if err == nil {
		return 0
	}
	return err.observed
}

type policy struct {
	maxBytes int64
}

var standardPolicy = policy{maxBytes: MaximumBytes}

// CheckKnownSize rejects a known direct-file transport size above the
// package-owned budget. Streaming ingestion must still enforce the same
// budget because transport metadata can be absent or inaccurate.
func CheckKnownSize(size int64) error {
	return standardPolicy.checkKnownSize(size)
}

// Hash computes exact file identity within the package-owned byte budget.
func Hash(ctx context.Context, view access.View) (artifact.ContentHash, error) {
	return standardPolicy.hash(ctx, view)
}

// ReadExact returns exact locked file bytes within the package-owned byte
// budget.
func ReadExact(
	ctx context.Context,
	view access.View,
	expected artifact.ExactIdentity,
) (access.FileContent, error) {
	return standardPolicy.readExact(ctx, view, expected)
}

// Copy streams one direct file into unpublished caller-owned staging while
// enforcing the package-owned byte budget.
func Copy(ctx context.Context, destination io.Writer, source io.Reader) error {
	return standardPolicy.copy(ctx, destination, source)
}

func (value policy) checkKnownSize(size int64) error {
	if err := value.validate(); err != nil {
		return err
	}
	if size < 0 {
		return fmt.Errorf("direct file size must not be negative")
	}
	if size > value.maxBytes {
		return value.limitError(size)
	}
	return nil
}

func (value policy) hash(ctx context.Context, view access.View) (artifact.ContentHash, error) {
	if err := value.validate(); err != nil {
		return "", err
	}
	if view.Kind() != artifact.ArtifactKindFile {
		return "", fmt.Errorf("direct file hash requires a file artifact")
	}
	limit, err := access.NewTraversalLimit(1, value.maxBytes)
	if err != nil {
		return "", err
	}
	contentHash, err := view.HashWithLimit(ctx, limit)
	if err != nil {
		return "", value.mapAccessLimit(err)
	}
	return contentHash, nil
}

func (value policy) readExact(
	ctx context.Context,
	view access.View,
	expected artifact.ExactIdentity,
) (access.FileContent, error) {
	if err := value.validate(); err != nil {
		return access.FileContent{}, err
	}
	content, err := view.ReadRootFileVerified(ctx, expected, value.maxBytes)
	if err != nil {
		return access.FileContent{}, value.mapAccessLimit(err)
	}
	return content, nil
}

func (value policy) copy(ctx context.Context, destination io.Writer, source io.Reader) error {
	if err := value.validate(); err != nil {
		return err
	}
	if ctx == nil {
		return fmt.Errorf("direct file copy context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if destination == nil {
		return fmt.Errorf("direct file copy destination is required")
	}
	if source == nil {
		return fmt.Errorf("direct file copy source is required")
	}

	buffer := make([]byte, 32*1024)
	var observed int64
	emptyReads := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		readSize := len(buffer)
		remaining := value.maxBytes - observed
		if remaining < int64(readSize) {
			readSize = int(remaining) + 1
		}
		count, readErr := source.Read(buffer[:readSize])
		if count < 0 || count > readSize {
			return fmt.Errorf("direct file source returned invalid byte count %d", count)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if count == 0 && readErr == nil {
			emptyReads++
			if emptyReads >= 100 {
				return io.ErrNoProgress
			}
			continue
		}
		emptyReads = 0

		observed += int64(count)
		if observed > value.maxBytes {
			return value.limitError(observed)
		}
		if count > 0 {
			written, writeErr := destination.Write(buffer[:count])
			if writeErr != nil {
				return writeErr
			}
			if written != count {
				return io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func (value policy) validate() error {
	if value.maxBytes <= 0 {
		return fmt.Errorf("direct file budget is not initialized")
	}
	return nil
}

func (value policy) mapAccessLimit(err error) error {
	var limitErr *access.LimitError
	if !errors.As(err, &limitErr) {
		return err
	}
	return value.limitError(limitErr.Observed())
}

func (value policy) limitError(observed int64) *LimitError {
	return &LimitError{limit: value.maxBytes, observed: observed}
}
