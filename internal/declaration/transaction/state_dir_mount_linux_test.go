//go:build linux

package transaction

import (
	"os"
	"path/filepath"
	"testing"
)

func TestObserveStateDirMountIsStableAcrossChildChanges(t *testing.T) {
	path := t.TempDir()
	before, beforeIncarnation, err := observeStateDirPlatform(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if before == "" {
		t.Fatal("mount identity is empty")
	}
	if err := os.WriteFile(filepath.Join(path, "child"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, afterIncarnation, err := observeStateDirPlatform(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if after != before || afterIncarnation != beforeIncarnation {
		t.Fatalf(
			"platform identity changed after child write: before=(%q,%q) after=(%q,%q)",
			before,
			beforeIncarnation,
			after,
			afterIncarnation,
		)
	}
}
