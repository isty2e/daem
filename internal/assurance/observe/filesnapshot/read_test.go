package filesnapshot_test

import (
	"errors"
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
	if _, _, err := filesnapshot.ReadRegularFile(directory, 16); !errors.Is(err, filesnapshot.ErrNotRegular) ||
		!strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory error = %v", err)
	}

	oversized := filepath.Join(root, "oversized")
	if err := os.WriteFile(oversized, []byte("too large"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := filesnapshot.ReadRegularFile(oversized, 4); !errors.Is(err, filesnapshot.ErrLimitExceeded) ||
		!strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized error = %v", err)
	}

	symlink := filepath.Join(root, "symlink")
	if err := os.Symlink(oversized, symlink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, _, err := filesnapshot.ReadRegularFile(symlink, 16); !errors.Is(err, filesnapshot.ErrSymlink) ||
		!strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink error = %v", err)
	}
}

func TestReadRegularFileAllowsSymlinkedAncestorButNotFinalSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	actual := filepath.Join(root, "actual")
	if err := os.Mkdir(actual, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actual, "config"), []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(actual, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	content, exists, err := filesnapshot.ReadRegularFile(filepath.Join(alias, "config"), 16)
	if err != nil || !exists || string(content) != "value" {
		t.Fatalf("ancestor-symlink read = (%q, %t, %v)", content, exists, err)
	}
}
