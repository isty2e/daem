//go:build windows

package commit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestWindowsCreatedAncestorCleanupRetainsPendingDurabilityState(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "created", "state.json")
	var cleanup AncestorCleanup
	if err := cleanup.PrepareParent(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	if cleanup.state == nil {
		t.Fatal("created ancestor cleanup state is unavailable")
	}
	if len(cleanup.state.directories) != 1 {
		t.Fatalf("created ancestor count = %d, want 1", len(cleanup.state.directories))
	}
	directory := &cleanup.state.directories[0]
	injected := errors.New("injected parent durability failure")
	err := removeWindowsCreatedDirectoryWithFaults(
		t.Context(),
		directory,
		faultPlan{failures: map[phase]error{phaseSyncCleanupParent: injected}},
	)
	if !hasStorageFailureKind(err, mutationfs.FailureIndeterminateCommit) {
		t.Fatalf("post-delete cleanup failure = %v, want indeterminate", err)
	}
	if directory.cleanupState != windowsCreatedDirectoryCleanupPendingDurability ||
		directory.pendingDurabilityFailure == nil || directory.handle != nil {
		t.Fatalf(
			"cleanup state = %d pending=%v handle=%v",
			directory.cleanupState,
			directory.pendingDurabilityFailure,
			directory.handle,
		)
	}
	if _, statErr := os.Lstat(directory.path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("deleted ancestor = %v, want absent", statErr)
	}
	retryErr := cleanup.RemoveEmpty(t.Context())
	if !errors.Is(retryErr, injected) || !hasStorageFailureKind(retryErr, mutationfs.FailureIndeterminateCommit) {
		t.Fatalf("pending durability retry = %v, want retained typed failure", retryErr)
	}
	cleanup.Close()
}
