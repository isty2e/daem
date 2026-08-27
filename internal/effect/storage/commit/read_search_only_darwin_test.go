//go:build darwin

package commit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDarwinObservationReadsTraverseSearchOnlyAncestor(t *testing.T) {
	root := canonicalTempDir(t)
	ancestor := filepath.Join(root, "search-only")
	directory := filepath.Join(ancestor, "recovery")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "journal.json")
	if err := os.WriteFile(file, []byte("journal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ancestor, 0o111); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ancestor, 0o700) })

	if _, err := ObserveEntryIdentity(t.Context(), directory); err != nil {
		t.Fatalf("ObserveEntryIdentity: %v", err)
	}
	if _, err := SnapshotDirectory(t.Context(), directory, 8); err != nil {
		t.Fatalf("SnapshotDirectory: %v", err)
	}
	snapshot, err := ReadRegularFileSnapshotUpTo(t.Context(), file, 64)
	if err != nil {
		t.Fatalf("ReadRegularFileSnapshotUpTo: %v", err)
	}
	if content := snapshot.Content(); string(content) != "journal" {
		t.Fatalf("content = %q", content)
	}
}
