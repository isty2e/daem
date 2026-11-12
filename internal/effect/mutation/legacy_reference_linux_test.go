//go:build linux

package mutation

import (
	"os"
	"path/filepath"
	"testing"
)

func legacyReferenceFixture(t *testing.T) PersistedDirectoryEntryAuthority {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "original")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "state.json")
	if err := os.WriteFile(path, []byte("source state"), 0o600); err != nil {
		t.Fatal(err)
	}
	observed, err := ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatal(err)
	}
	return observed
}
