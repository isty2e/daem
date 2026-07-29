package filesnapshot_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/filesnapshot"
)

func TestReadRegularFileDistinguishesMissingAndStableContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	content, exists, err := filesnapshot.ReadRegularFile(missing, 16)
	if err != nil || exists || content != nil {
		t.Fatalf("missing read = (%q, %t, %v)", content, exists, err)
	}

	path := filepath.Join(root, "config")
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, exists, err = filesnapshot.ReadRegularFile(path, 16)
	if err != nil || !exists || string(content) != "value" {
		t.Fatalf("stable read = (%q, %t, %v)", content, exists, err)
	}
}

func TestReadRegularFileRejectsUnsafeShapes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := filesnapshot.ReadRegularFile(directory, 16); err == nil ||
		!strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory error = %v", err)
	}

	oversized := filepath.Join(root, "oversized")
	if err := os.WriteFile(oversized, []byte("too large"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := filesnapshot.ReadRegularFile(oversized, 4); err == nil ||
		!strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized error = %v", err)
	}

	symlink := filepath.Join(root, "symlink")
	if err := os.Symlink(oversized, symlink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, _, err := filesnapshot.ReadRegularFile(symlink, 16); err == nil ||
		!strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink error = %v", err)
	}
}
