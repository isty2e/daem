//go:build windows

package commit

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestWindowsFileCreateReplaceAndReadRoundTrip(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	create, err := NewFileCreate(path, []byte("first"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitFile(t.Context(), create); err != nil {
		t.Fatal(err)
	}
	assertWindowsRegularFileSnapshot(t, path, "first", 0o600)

	expected, err := CaptureEntryIdentity(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	replace, err := NewFileReplacement(path, []byte("second"), 0o644, expected)
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitFile(t.Context(), replace); err != nil {
		t.Fatal(err)
	}
	assertWindowsRegularFileSnapshot(t, path, "second", 0o644)
	assertNoWindowsStorageResidue(t, root)
}

func TestWindowsFileCreateCollisionAndStaleReplacementAreUncommitted(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	if err := os.WriteFile(path, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	create, err := NewFileCreate(path, []byte("new"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitFile(t.Context(), create); !hasStorageFailureKind(err, mutationfs.FailureUncommitted) {
		t.Fatalf("create collision error = %v, want uncommitted", err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "foreign" {
		t.Fatalf("collision destination = %q, %v", content, err)
	}

	canonicalPath := filepath.Join(root, "canonical.json")
	request, err := NewFileCreate(canonicalPath, []byte("before"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitFile(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	expected, err := CaptureEntryIdentity(t.Context(), canonicalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonicalPath, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	replacement, err := NewFileReplacement(canonicalPath, []byte("replacement"), 0o600, expected)
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitFile(t.Context(), replacement); !hasStorageFailureKind(err, mutationfs.FailureUncommitted) {
		t.Fatalf("stale replacement error = %v, want uncommitted", err)
	}
	content, err = os.ReadFile(canonicalPath)
	if err != nil || string(content) != "changed" {
		t.Fatalf("stale replacement destination = %q, %v", content, err)
	}
	assertNoWindowsStorageResidue(t, root)
}

func TestWindowsFileCommitHonorsCancellationBeforeEffects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	request, err := NewFileCreate(path, []byte("payload"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CommitFile(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled commit error = %v, want context.Canceled", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("canceled destination observation = %v, want missing", err)
	}
}

func assertWindowsRegularFileSnapshot(t *testing.T, path string, content string, mode fs.FileMode) {
	t.Helper()
	snapshot, err := ReadRegularFileSnapshotUpTo(t.Context(), path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if string(snapshot.Content()) != content || snapshot.Mode() != mode {
		t.Fatalf("snapshot = content %q mode %04o, want %q %04o", snapshot.Content(), snapshot.Mode(), content, mode)
	}
	if snapshot.Identity() == nil || snapshot.Identity().Kind() != mutationfs.EntryKindFile {
		t.Fatalf("snapshot identity = %#v", snapshot.Identity())
	}
}

func assertNoWindowsStorageResidue(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), temporaryPrefix) || strings.HasPrefix(entry.Name(), tombstonePrefix) ||
			strings.HasPrefix(entry.Name(), cleanupPrefix) {
			t.Fatalf("unexpected storage residue %q", entry.Name())
		}
	}
}

func hasStorageFailureKind(err error, kind mutationfs.FailureKind) bool {
	var failure interface{ Kind() mutationfs.FailureKind }
	return errors.As(err, &failure) && failure.Kind() == kind
}
