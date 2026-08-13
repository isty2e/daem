package declarationartifact_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/declarationartifact"
)

func TestReadAdmitsStableFinalSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	referent := filepath.Join(root, "manifest.toml")
	if err := os.WriteFile(referent, []byte("version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(root, "selected.toml")
	if err := os.Symlink(referent, selected); err != nil {
		t.Fatal(err)
	}

	content, err := declarationartifact.Read(t.Context(), selected)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "version = 1\n" {
		t.Fatalf("content = %q", content)
	}
}

func TestReadRejectsDirectoryWithoutTraversingIt(t *testing.T) {
	t.Parallel()

	path := t.TempDir()
	if _, err := declarationartifact.Read(t.Context(), path); err == nil {
		t.Fatal("Read admitted a directory")
	}
}

func TestReadPreservesNativeMissingFileSemantics(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing")
	if _, err := declarationartifact.Read(t.Context(), path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Read error = %v, want os.ErrNotExist", err)
	}
}

func TestAdmitRejectsContentOverMaximum(t *testing.T) {
	t.Parallel()

	content := make([]byte, declarationartifact.MaximumBytes+1)
	if err := declarationartifact.Admit(content); !errors.Is(err, declarationartifact.ErrTooLarge) {
		t.Fatalf("Admit error = %v, want ErrTooLarge", err)
	}
}

func TestReadRejectsOversizedFileBeforeReadingContent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "oversized.toml")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(declarationartifact.MaximumBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := declarationartifact.Read(t.Context(), path); !errors.Is(err, declarationartifact.ErrTooLarge) {
		t.Fatalf("Read error = %v, want ErrTooLarge", err)
	}
}
