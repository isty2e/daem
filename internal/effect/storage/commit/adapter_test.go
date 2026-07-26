package commit

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestAdapterClassifiesRequestValidationBeforeVisibility(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temporary root: %v", err)
	}
	path := filepath.Join(root, "directory")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	identity, err := Adapter{}.CaptureEntryIdentity(context.Background(), path)
	if err != nil {
		t.Fatalf("capture directory identity: %v", err)
	}

	err = (Adapter{}).ReplaceFile(context.Background(), path, []byte("content"), 0o600, identity)
	kind, classified := mutationfs.FailureKindOf(err)
	if !classified || kind != mutationfs.FailureUncommitted {
		t.Fatalf("ReplaceFile failure = %v, classification = %q/%t, want uncommitted", err, kind, classified)
	}
}
