package journal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func TestRemoveJournalRequiresFilesystemBeforeCapabilityValidation(t *testing.T) {
	err := RemoveJournal(context.Background(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "filesystem is required") {
		t.Fatalf("RemoveJournal error = %v, want filesystem requirement", err)
	}
}

func TestRemoveJournalIsIdempotentForMissingOperation(t *testing.T) {
	operationDir := filepath.Join(t.TempDir(), "missing")
	root, destination, err := rootedpath.CaptureDestination(operationDir)
	if err != nil {
		t.Fatalf("CaptureDestination: %v", err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("close captured root: %v", err)
		}
	})
	capability, err := root.Acquire(destination)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := RemoveJournal(t.Context(), journalTestFilesystem(), capability); err != nil {
		t.Fatalf("RemoveJournal missing operation: %v", err)
	}
	if _, err := os.Lstat(operationDir); !os.IsNotExist(err) {
		t.Fatalf("missing operation appeared after removal: %v", err)
	}
}
