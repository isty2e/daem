package access

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/isty2e/daem/internal/supply/artifact"
)

// HashPath computes hash-v1 for a regular file or directory through the
// platform no-follow access boundary.
func HashPath(ctx context.Context, root string) (artifact.ContentHash, artifact.ArtifactKind, error) {
	if ctx == nil {
		return "", "", fmt.Errorf("hash source path: context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}

	displayPath := filepath.Clean(root)
	view, err := OpenView(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", "", fmt.Errorf("source path %q does not exist: %w", displayPath, err)
		}
		return "", "", fmt.Errorf("open source path %q: %w", displayPath, err)
	}
	contentHash, err := view.Hash(ctx)
	if err != nil {
		return "", "", fmt.Errorf("hash source path %q: %w", displayPath, err)
	}
	return contentHash, view.Kind(), nil
}
