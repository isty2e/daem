//go:build linux

package rootedpath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureRootTraversesSearchOnlyAncestorAndRetainsReadableFinalRoot(t *testing.T) {
	root := t.TempDir()
	ancestor := filepath.Join(root, "search-only")
	selected := filepath.Join(ancestor, "recovery")
	if err := os.MkdirAll(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(selected, "journal.json"), []byte("journal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ancestor, 0o111); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ancestor, 0o700) })

	captured, err := CaptureRootNoFollow(selected)
	if err != nil {
		t.Fatalf("CaptureRootNoFollow: %v", err)
	}
	defer captured.Close()
	working, err := captured.AcquireWorkingDirectory()
	if err != nil {
		t.Fatalf("AcquireWorkingDirectory: %v", err)
	}
	defer working.Close()
	directory, err := working.OpenDirectory()
	if err != nil {
		t.Fatalf("OpenDirectory: %v", err)
	}
	defer directory.Close()
	names, err := directory.Readdirnames(-1)
	if err != nil {
		t.Fatalf("Readdirnames: %v", err)
	}
	if len(names) != 1 || names[0] != "journal.json" {
		t.Fatalf("names = %#v", names)
	}
}
