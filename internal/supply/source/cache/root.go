package cache

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/mutation"
)

// CanonicalRootPath resolves ambient ancestor aliases while rejecting a
// symlink or non-directory at the selected cache-root path itself.
func CanonicalRootPath(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	physical, err := mutation.CanonicalDirectoryEntryPath(filepath.Clean(absolute))
	if err != nil {
		return "", fmt.Errorf("resolve cache root ancestors %q: %w", value, err)
	}
	info, err := os.Lstat(physical)
	if errors.Is(err, fs.ErrNotExist) {
		return physical, nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect cache root %q: %w", value, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("cache root selection %q contains a symbolic-link component", value)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("cache root %q is not a directory", value)
	}
	return physical, nil
}
