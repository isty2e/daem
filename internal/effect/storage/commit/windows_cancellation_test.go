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

func TestWindowsIdentityCaptureAdmitsCancellationBeforeTraversal(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	identity, err := CaptureEntryIdentity(ctx, filepath.Join(root, "missing.json"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled identity capture = %v, want context.Canceled", err)
	}
	if !hasStorageFailureKind(err, mutationfs.FailureUncommitted) {
		t.Fatalf("pre-cancelled identity capture failure kind = %v, want uncommitted", err)
	}
	if identity.Kind() != "" {
		t.Fatalf("pre-cancelled identity capture returned identity %q", identity.Kind())
	}
}

func TestWindowsLogicalRemovalAdmitsPreEffectCancellationAsUncommitted(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "victim.json")
	if err := os.WriteFile(target, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	capability := acquireWindowsTestCommitCapability(t, target)
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		_ = capability.Close()
		t.Fatal(err)
	}
	request, err := NewLogicalRemoval(target, expected)
	if err != nil {
		t.Fatal(err)
	}
	request.capability = capability

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	outcome, err := CommitLogicalRemovalWithOutcome(ctx, request)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled logical removal = %v, want context.Canceled", err)
	}
	if !hasStorageFailureKind(err, mutationfs.FailureUncommitted) {
		t.Fatalf("pre-cancelled logical removal failure kind = %v, want uncommitted", err)
	}
	if outcome.State() != mutationfs.CommitOutcomeUncommitted {
		t.Fatalf("pre-cancelled logical removal outcome = %q, want uncommitted", outcome.State())
	}
	if _, statErr := os.Lstat(target); statErr != nil {
		t.Fatalf("pre-cancelled logical removal removed the entry: %v", statErr)
	}
}

func TestWindowsPrepareCommitParentRemovesAncestorsWhenCancelledAfterCreation(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "missing", "nested", "state.json")
	var cleanup AncestorCleanup

	ctx, cancel := context.WithCancel(t.Context())
	firings := 0
	faults := faultPlan{actions: map[phase]func(){
		phaseCreateAncestors: func() {
			firings++
			if firings == 3 {
				cancel()
			}
		},
	}}
	state, err := cleanup.requireOpen()
	if err != nil {
		t.Fatal(err)
	}
	prepareErr := prepareWindowsCommitParentWithFaults(ctx, target, state, faults)
	if !errors.Is(prepareErr, context.Canceled) {
		t.Fatalf("post-creation cancellation = %v, want context.Canceled", prepareErr)
	}
	if !hasStorageFailureKind(prepareErr, mutationfs.FailureUncommitted) {
		t.Fatalf("post-creation cancellation failure kind = %v, want uncommitted", prepareErr)
	}
	removeErr := cleanup.RemoveEmpty(context.Background())
	cleanup.Close()
	if removeErr != nil {
		t.Fatalf("created ancestor cleanup after cancellation = %v", removeErr)
	}
	if _, statErr := os.Lstat(filepath.Join(root, "missing")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("created ancestor retained after cancelled preparation: %v", statErr)
	}
}
