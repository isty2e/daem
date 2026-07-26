package mutation

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type canonicalPath struct {
	keyPath    string
	accessPath string
}

// CanonicalDirectoryEntryPath returns the physical access path used by a
// directory-entry mutation domain. Ancestor symlinks are resolved while the
// final entry name is retained for no-follow mutation.
func CanonicalDirectoryEntryPath(path string) (string, error) {
	identity, err := canonicalPathIdentity(path, PathEffectDirectoryEntry)
	if err != nil {
		return "", err
	}
	return identity.accessPath, nil
}

// CanonicalDirectoryEntryKey returns the platform-normalized equality key used
// by directory-entry mutation domains. It is suitable for durable authority identity.
func CanonicalDirectoryEntryKey(path string) (string, error) {
	identity, err := canonicalPathIdentity(path, PathEffectDirectoryEntry)
	if err != nil {
		return "", err
	}
	return identity.keyPath, nil
}

func canonicalPathIdentity(path string, effect PathEffect) (canonicalPath, error) {
	if err := effect.validate(); err != nil {
		return canonicalPath{}, err
	}
	if strings.TrimSpace(path) == "" {
		return canonicalPath{}, fmt.Errorf("mutation path is required")
	}
	if strings.ContainsRune(path, '\x00') {
		return canonicalPath{}, fmt.Errorf("mutation path contains a NUL byte")
	}
	absolutePath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return canonicalPath{}, fmt.Errorf("resolve mutation path %q: %w", path, err)
	}

	resolvePath := absolutePath
	suffix := ""
	if effect == PathEffectDirectoryEntry && filepath.Dir(absolutePath) != absolutePath {
		resolvePath = filepath.Dir(absolutePath)
		suffix = filepath.Base(absolutePath)
	}
	resolved, err := resolveDeepestExisting(resolvePath)
	if err != nil {
		return canonicalPath{}, fmt.Errorf("canonicalize mutation path %q: %w", path, err)
	}
	if suffix != "" {
		resolved = filepath.Join(resolved, suffix)
	}
	resolved = filepath.Clean(resolved)
	return canonicalPath{
		keyPath:    normalizePlatformPathKey(resolved),
		accessPath: resolved,
	}, nil
}

func resolveDeepestExisting(path string) (string, error) {
	candidate := filepath.Clean(path)
	missing := make([]string, 0)
	for {
		_, err := os.Lstat(candidate)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return "", err
			}
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", fmt.Errorf("no existing ancestor for %q", path)
		}
		missing = append(missing, filepath.Base(candidate))
		candidate = parent
	}
}

func normalizePlatformPathKey(path string) string {
	normalized := filepath.Clean(path)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.ToLower(normalized)
	}
	return normalized
}

func pathContains(parent string, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
