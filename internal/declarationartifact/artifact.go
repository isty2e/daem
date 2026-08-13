// Package declarationartifact defines the physical and byte-size contract for
// daem manifests and lockfiles.
package declarationartifact

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/isty2e/daem/internal/filesnapshot"
)

const MaximumBytes int64 = 64 << 20

// ErrTooLarge reports declaration bytes beyond MaximumBytes.
var ErrTooLarge = errors.New("declaration artifact exceeds maximum size")

type tooLargeError struct {
	observed int64
}

func (err tooLargeError) Error() string {
	return fmt.Sprintf(
		"declaration artifact contains %d bytes, maximum %d",
		err.observed,
		MaximumBytes,
	)
}

func (err tooLargeError) Unwrap() error { return ErrTooLarge }

// Admit verifies that in-memory declaration bytes satisfy the shared limit.
func Admit(content []byte) error {
	if int64(len(content)) > MaximumBytes {
		return tooLargeError{observed: int64(len(content))}
	}
	return nil
}

// Read returns one bounded, stable regular-file referent selected by path.
// Final symlinks are admitted only while both the selected link and its
// referent remain stable for the complete read.
func Read(ctx context.Context, path string) ([]byte, error) {
	content, exists, err := filesnapshot.ReadRegularFileReferentContext(
		ctx,
		path,
		MaximumBytes,
	)
	if err != nil {
		if errors.Is(err, filesnapshot.ErrLimitExceeded) {
			return nil, &os.PathError{
				Op:   "read",
				Path: path,
				Err: fmt.Errorf(
					"%w (maximum %d bytes)",
					ErrTooLarge,
					MaximumBytes,
				),
			}
		}
		return nil, &os.PathError{Op: "read", Path: path, Err: err}
	}
	if !exists {
		return nil, &os.PathError{Op: "open", Path: path, Err: syscall.ENOENT}
	}
	return content, nil
}
