package cache

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// CanonicalRootPath resolves ambient ancestor aliases while rejecting a
// symlink or non-directory at the selected cache-root path itself.
func CanonicalRootPath(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	current := absolute
	missing := make([]string, 0)
	for {
		info, inspectErr := os.Lstat(current)
		if inspectErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("cache root selection %q contains a symbolic-link component", current)
			}
			if !info.IsDir() {
				return "", fmt.Errorf("cache root ancestor %q is not a directory", current)
			}
			parent := filepath.Dir(current)
			physicalParent, resolveErr := filepath.EvalSymlinks(parent)
			if resolveErr != nil {
				return "", fmt.Errorf("resolve cache root ancestor parent %q: %w", parent, resolveErr)
			}
			physical := filepath.Join(physicalParent, filepath.Base(current))
			physicalInfo, inspectPhysicalErr := os.Lstat(physical)
			if inspectPhysicalErr != nil {
				return "", fmt.Errorf("inspect physical cache root ancestor %q: %w", physical, inspectPhysicalErr)
			}
			if physicalInfo.Mode()&os.ModeSymlink != 0 ||
				!physicalInfo.IsDir() ||
				!os.SameFile(info, physicalInfo) {
				return "", fmt.Errorf("cache root ancestor %q changed while resolving its physical parent", current)
			}
			for index := len(missing) - 1; index >= 0; index-- {
				physical = filepath.Join(physical, missing[index])
			}
			return filepath.Clean(physical), nil
		}
		if !errors.Is(inspectErr, fs.ErrNotExist) {
			return "", fmt.Errorf("inspect cache root ancestor %q: %w", current, inspectErr)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("cache root %q has no existing directory ancestor", value)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}
