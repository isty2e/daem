//go:build darwin || linux

package filesnapshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRegularFileContextRejectsRewriteWithRestoredMtime(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = readRegularFileContext(context.Background(), path, 64, readHooks{
		afterOpen: func() {
			if writeErr := os.WriteFile(path, []byte("after!"), 0o600); writeErr != nil {
				t.Fatalf("rewrite observed file: %v", writeErr)
			}
			if timeErr := os.Chtimes(path, before.ModTime(), before.ModTime()); timeErr != nil {
				t.Fatalf("restore mtime: %v", timeErr)
			}
		},
	})
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("rewrite error = %v, want ErrChanged", err)
	}
}
