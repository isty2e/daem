//go:build darwin || linux

package commit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestObserveEntryIdentityPreservesObjectAcrossDirectoryMetadataChanges(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "state\ncontrol")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	before, err := ObserveEntryIdentity(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	beforeFingerprint, err := before.ObjectFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "child"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := ObserveEntryIdentity(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	afterFingerprint, err := after.ObjectFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if !before.SameObject(after) || beforeFingerprint != afterFingerprint {
		t.Fatalf("same directory object changed identity: before=%q after=%q", beforeFingerprint, afterFingerprint)
	}
}
