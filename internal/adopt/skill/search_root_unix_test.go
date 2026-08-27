//go:build darwin || linux

package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSearchRootCachePreservesFinalRootSymlinkCompatibility(t *testing.T) {
	base := t.TempDir()
	physical := filepath.Join(base, "physical")
	if err := os.Mkdir(physical, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(physical, "review"), 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}

	cache := NewSearchRootCache()
	entries, err := cache.entries(t.Context(), alias)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0] != "review" {
		t.Fatalf("symlinked root entries = %#v, want [review]", entries)
	}
}
