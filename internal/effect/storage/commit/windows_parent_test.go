//go:build windows

package commit

import (
	"context"
	"errors"
	"io/fs"
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

func TestWindowsPrepareCommitParentHonorsCancellationBeforeAncestorCreate(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "missing", "nested", "state.json")
	ctx, cancel := context.WithCancel(t.Context())
	var cleanup AncestorCleanup
	state, err := cleanup.requireOpen()
	if err != nil {
		t.Fatal(err)
	}
	faults := faultPlan{actions: map[phase]func(){phaseCreateAncestors: cancel}}
	prepareErr := prepareWindowsCommitParentWithFaults(ctx, target, state, faults)
	if !errors.Is(prepareErr, context.Canceled) {
		t.Fatalf("cancelled ancestor preparation = %v, want context.Canceled", prepareErr)
	}
	if len(state.directories) != 0 {
		t.Fatalf("ancestors retained after cancellation = %d, want 0", len(state.directories))
	}
	if _, statErr := os.Lstat(filepath.Join(root, "missing")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("ancestor created after cancellation: %v", statErr)
	}
	cleanup.Close()
}

func TestWindowsCreatedAncestorRemovalHonorsCancellationBeforeDisposition(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "created", "state.json")
	var cleanup AncestorCleanup
	if err := cleanup.PrepareParent(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	if cleanup.state == nil || len(cleanup.state.directories) != 1 {
		t.Fatalf("created ancestor state = %+v, want one retained directory", cleanup.state)
	}
	directory := &cleanup.state.directories[0]
	ctx, cancel := context.WithCancel(t.Context())
	faults := faultPlan{actions: map[phase]func(){phaseCleanupEntry: cancel}}
	err := removeWindowsCreatedDirectoryWithFaults(ctx, directory, faults)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled ancestor removal = %v, want context.Canceled", err)
	}
	if directory.cleanupState != windowsCreatedDirectoryCleanupActive {
		t.Fatalf("cleanup state = %d, want active after cancelled disposition", directory.cleanupState)
	}
	if _, statErr := os.Lstat(directory.path); statErr != nil {
		t.Fatalf("created ancestor removed despite cancellation: %v", statErr)
	}
	cleanup.Close()
}
