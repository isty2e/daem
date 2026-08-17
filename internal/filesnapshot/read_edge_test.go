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

func TestReadRegularFileSnapshotContextReturnsDescriptorMode(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "snapshot")
	if err := os.WriteFile(path, []byte("content"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	snapshot, exists, err := ReadRegularFileSnapshotContext(t.Context(), path, 64)
	if err != nil || !exists {
		t.Fatalf("snapshot read = (%t, %v)", exists, err)
	}
	if string(snapshot.Content()) != "content" || snapshot.Mode().Perm() != 0o640 || snapshot.Revision() == "" {
		t.Fatalf(
			"snapshot = (%q, %04o, %q), want content mode 0640 and a revision",
			snapshot.Content(),
			snapshot.Mode().Perm(),
			snapshot.Revision(),
		)
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

func TestReadRegularFileContextCountedReportsAttemptedBytes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	missing, err := ReadRegularFileContextCounted(context.Background(), filepath.Join(root, "missing"), 16)
	if err != nil || missing.Exists || missing.Attempted != 0 {
		t.Fatalf("missing counted read = %+v, %v", missing, err)
	}

	path := filepath.Join(root, "source")
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	success, err := ReadRegularFileContextCounted(context.Background(), path, 64)
	if err != nil || !success.Exists || string(success.Content) != "content" || success.Attempted != 7 {
		t.Fatalf("success counted read = %+v, %v", success, err)
	}

	symlink := filepath.Join(root, "link")
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal(err)
	}
	rejected, err := ReadRegularFileContextCounted(context.Background(), symlink, 64)
	if !errors.Is(err, ErrSymlink) || rejected.Exists || rejected.Attempted != 0 {
		t.Fatalf("symlink counted read = %+v, %v", rejected, err)
	}

	directory := filepath.Join(root, "dir")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	notRegular, err := ReadRegularFileContextCounted(context.Background(), directory, 64)
	if !errors.Is(err, ErrNotRegular) || notRegular.Exists || notRegular.Attempted != 0 {
		t.Fatalf("directory counted read = %+v, %v", notRegular, err)
	}

	changed, err := readRegularFileContextCounted(context.Background(), path, 64, readHooks{
		afterOpen: func() {
			displaced := filepath.Join(root, "displaced")
			if renameErr := os.Rename(path, displaced); renameErr != nil {
				t.Fatalf("rename observed file: %v", renameErr)
			}
			if writeErr := os.WriteFile(path, []byte("afterxx"), 0o600); writeErr != nil {
				t.Fatalf("write replacement: %v", writeErr)
			}
		},
	})
	if !errors.Is(err, ErrChanged) || changed.Exists || changed.Attempted != 7 {
		t.Fatalf("changed counted read = %+v, %v", changed, err)
	}
}

func TestReadRegularFileReferentContextFollowsStableFinalSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	referent := filepath.Join(root, "referent")
	if err := os.WriteFile(referent, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "selected")
	if err := os.Symlink(referent, path); err != nil {
		t.Fatal(err)
	}

	content, exists, err := ReadRegularFileReferentContext(t.Context(), path, 64)
	if err != nil || !exists || string(content) != "content" {
		t.Fatalf("referent read = (%q, %t, %v)", content, exists, err)
	}
}

func TestReadRegularFileReferentContextRejectsFinalSymlinkRetarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	selected := filepath.Join(root, "selected")
	if err := os.Symlink(first, selected); err != nil {
		t.Fatal(err)
	}

	_, _, err := readRegularFileReferentContext(t.Context(), selected, 64, readHooks{
		afterInspect: func() {
			if removeErr := os.Remove(selected); removeErr != nil {
				t.Fatalf("remove selected symlink: %v", removeErr)
			}
			if linkErr := os.Symlink(second, selected); linkErr != nil {
				t.Fatalf("retarget selected symlink: %v", linkErr)
			}
		},
	})
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("retarget error = %v, want ErrChanged", err)
	}
}

func TestReadRegularFileReferentContextRejectsReferentReplacementBeforeOpen(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	referent := filepath.Join(root, "referent")
	displaced := filepath.Join(root, "displaced")
	if err := os.WriteFile(referent, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(root, "selected")
	if err := os.Symlink(referent, selected); err != nil {
		t.Fatal(err)
	}

	_, _, err := readRegularFileReferentContext(t.Context(), selected, 64, readHooks{
		afterInspect: func() {
			if renameErr := os.Rename(referent, displaced); renameErr != nil {
				t.Fatalf("displace referent: %v", renameErr)
			}
			if writeErr := os.WriteFile(referent, []byte("after"), 0o600); writeErr != nil {
				t.Fatalf("replace referent: %v", writeErr)
			}
		},
	})
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("referent replacement error = %v, want ErrChanged", err)
	}
}
