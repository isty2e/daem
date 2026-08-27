//go:build darwin || linux

package skill

import (
	"os"
	"path/filepath"
	"strings"
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

func TestSearchRootCacheRejectsChangedSharedResolvedRoot(t *testing.T) {
	base := t.TempDir()
	physical := filepath.Join(base, "physical")
	if err := os.Mkdir(physical, 0o700); err != nil {
		t.Fatal(err)
	}
	firstAlias := filepath.Join(base, "first")
	secondAlias := filepath.Join(base, "second")
	for _, alias := range []string{firstAlias, secondAlias} {
		if err := os.Symlink(physical, alias); err != nil {
			t.Fatal(err)
		}
	}

	cache := NewSearchRootCache()
	if entries, err := cache.entries(t.Context(), firstAlias); err != nil || len(entries) != 0 {
		t.Fatalf("initial entries = %#v, error = %v, want empty", entries, err)
	}
	if err := os.Mkdir(filepath.Join(physical, "review"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.entries(t.Context(), secondAlias); err == nil ||
		!strings.Contains(err.Error(), "changed after observation") {
		t.Fatalf("shared resolved-root reuse error = %v, want stale listing", err)
	}
}
