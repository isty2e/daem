package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheAncestorAliasDoesNotDependOnExistingSuffix(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "project")
	alias := filepath.Join(root, "alias")
	if err := os.Mkdir(physical, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("project", alias); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(alias, ".daem", "cache", "sources")
	before, err := CanonicalRootPath(selected)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(physical, ".daem", "cache", "sources"), 0o700); err != nil {
		t.Fatal(err)
	}
	after, err := CanonicalRootPath(selected)
	if err != nil || before != after {
		t.Fatalf("cache root changed with directory existence: %q -> %q, %v", before, after, err)
	}
	if err := os.RemoveAll(filepath.Join(physical, ".daem", "cache", "sources")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(physical, filepath.Join(physical, ".daem", "cache", "sources")); err != nil {
		t.Fatal(err)
	}
	if _, err := CanonicalRootPath(selected); err == nil {
		t.Fatal("selected cache-root link was accepted")
	}
}
