//go:build darwin || linux

package rootedpath

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSearchOnlyDirectoryOpenAcceptsExecuteOnlyDirectory(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "search-only")
	if err := os.Mkdir(directory, 0o111); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0o700) })

	fd, err := openSearchOnlyDirectory(directory)
	if err != nil {
		t.Fatalf("open search-only directory: %v", err)
	}
	if err := unix.Close(fd); err != nil {
		t.Fatalf("close search-only directory: %v", err)
	}
}
