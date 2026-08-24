//go:build windows

package gitcli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsGitCacheCapabilityFailsBeforeCacheEffects(t *testing.T) {
	cacheRoot := filepath.Join(t.TempDir(), "missing", "git-cache")
	resolver, err := NewResolver(cacheRoot)
	if err != nil {
		t.Fatal(err)
	}
	root, err := resolver.captureCacheRoot(t.Context())
	if root != nil {
		_ = root.Close()
		t.Fatal("unsupported Windows Git cache returned rooted authority")
	}
	if err == nil || !strings.Contains(err.Error(), "descriptor-backed working directories are unsupported") {
		t.Fatalf("Windows Git cache preflight error = %v, want descriptor-backed cwd blocker", err)
	}
	if _, statErr := os.Lstat(cacheRoot); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("Windows Git cache root = %v, want no cache effect", statErr)
	}
}
