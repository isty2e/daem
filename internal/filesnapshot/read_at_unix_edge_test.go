//go:build darwin || linux || freebsd || netbsd || openbsd

package filesnapshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRegularFileAtCountedHonorsCancellationBeforeSuccess(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "plugin.json")
	if err := os.WriteFile(path, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })
	ctx, cancel := context.WithCancel(context.Background())

	counted, err := readRegularFileAtCountedWithHooks(ctx, dir, "plugin.json", 64, readHooks{
		beforeSuccess: cancel,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("final validation cancellation = %+v, %v, want context.Canceled", counted, err)
	}
	if counted.Exists || counted.Attempted != 6 || len(counted.Content) != 0 {
		t.Fatalf("final validation cancellation = %+v, want attempted bytes without content", counted)
	}
}

func TestReadRegularFileAtCountedRejectsOversizedReplacementBeforeOpenAsChanged(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "plugin.json")
	displaced := filepath.Join(root, "displaced")
	if err := os.WriteFile(path, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })

	_, err = readRegularFileAtCountedWithHooks(context.Background(), dir, "plugin.json", 4, readHooks{
		afterInspect: func() {
			if renameErr := os.Rename(path, displaced); renameErr != nil {
				t.Fatalf("rename inspected file: %v", renameErr)
			}
			if writeErr := os.WriteFile(path, []byte("too large"), 0o600); writeErr != nil {
				t.Fatalf("write oversized replacement: %v", writeErr)
			}
		},
	})
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("oversized replacement error = %v, want ErrChanged", err)
	}
}
