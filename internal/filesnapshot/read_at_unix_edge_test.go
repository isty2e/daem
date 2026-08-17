//go:build darwin || linux

package filesnapshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

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
