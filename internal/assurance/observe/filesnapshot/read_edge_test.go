package filesnapshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRegularFileContextAdmitsExactLimit(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "exact")
	if err := os.WriteFile(path, []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, exists, err := ReadRegularFileContext(context.Background(), path, 4)
	if err != nil || !exists || string(content) != "1234" {
		t.Fatalf("exact-limit read = (%q, %t, %v)", content, exists, err)
	}
}

func TestReadRegularFileContextRejectsReplacementAfterOpen(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "source")
	displaced := filepath.Join(root, "displaced")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := readRegularFileContext(context.Background(), path, 64, readHooks{
		afterOpen: func() {
			if renameErr := os.Rename(path, displaced); renameErr != nil {
				t.Fatalf("rename observed file: %v", renameErr)
			}
			if writeErr := os.WriteFile(path, []byte("after"), 0o600); writeErr != nil {
				t.Fatalf("write replacement: %v", writeErr)
			}
		},
	})
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("replacement error = %v, want ErrChanged", err)
	}
}

func TestReadRegularFileContextRejectsReplacementBeforeOpen(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "source")
	displaced := filepath.Join(root, "displaced")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := readRegularFileContext(context.Background(), path, 64, readHooks{
		afterInspect: func() {
			if renameErr := os.Rename(path, displaced); renameErr != nil {
				t.Fatalf("rename inspected file: %v", renameErr)
			}
			if writeErr := os.WriteFile(path, []byte("after"), 0o600); writeErr != nil {
				t.Fatalf("write replacement: %v", writeErr)
			}
		},
	})
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("replacement error = %v, want ErrChanged", err)
	}
}

func TestReadRegularFileContextRejectsTruncationAfterOpen(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := readRegularFileContext(context.Background(), path, 64, readHooks{
		afterOpen: func() {
			if truncateErr := os.Truncate(path, 0); truncateErr != nil {
				t.Fatalf("truncate observed file: %v", truncateErr)
			}
		},
	})
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("truncation error = %v, want ErrChanged", err)
	}
}

func TestReadRegularFileContextStopsAfterCancellation(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	_, _, err := readRegularFileContext(ctx, path, 64, readHooks{afterOpen: cancel})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v, want context.Canceled", err)
	}
}
