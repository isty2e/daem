//go:build darwin || linux

package filesnapshot_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/filesnapshot"
)

func TestReadRegularFileAtStaysOnRetainedDirectoryDescriptor(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dirPath := filepath.Join(root, "plugin")
	if err := os.Mkdir(dirPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirPath, "plugin.json"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir, err := os.Open(dirPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })

	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "plugin.json"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dirPath, dirPath+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dirPath); err != nil {
		t.Fatal(err)
	}

	content, exists, err := filesnapshot.ReadRegularFileAt(t.Context(), dir, "plugin.json", 64)
	if err != nil || !exists || string(content) != "inside" {
		t.Fatalf("ReadRegularFileAt after path replacement = (%q, %t, %v), want inside", content, exists, err)
	}
}

func TestReadRegularFileAtRejectsFinalSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	dir, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })

	_, exists, err := filesnapshot.ReadRegularFileAt(t.Context(), dir, "link", 64)
	if exists || !errors.Is(err, filesnapshot.ErrSymlink) {
		t.Fatalf("ReadRegularFileAt(symlink) = (%t, %v), want ErrSymlink", exists, err)
	}
}

func TestReadRegularFileAtRejectsNestedName(t *testing.T) {
	t.Parallel()

	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })

	if _, _, err := filesnapshot.ReadRegularFileAt(t.Context(), dir, "nested/name", 64); err == nil {
		t.Fatal("ReadRegularFileAt nested name succeeded")
	}
}
