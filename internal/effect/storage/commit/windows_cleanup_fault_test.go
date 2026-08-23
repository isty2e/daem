//go:build windows

package commit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"golang.org/x/sys/windows"
)

func setWindowsTestEntryCanonicalMode(t *testing.T, path string, mode os.FileMode, directory bool) {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	access := uint32(windows.READ_CONTROL | windows.WRITE_DAC | windows.SYNCHRONIZE)
	flags := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		access |= windows.FILE_LIST_DIRECTORY
		flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
	} else {
		access |= windows.FILE_READ_ATTRIBUTES
	}
	handle, err := windows.CreateFile(
		name,
		access,
		windowsParentShareMode,
		nil,
		windows.OPEN_EXISTING,
		flags,
		0,
	)
	if err != nil {
		t.Fatalf("open %q for canonical security: %v", path, err)
	}
	defer windows.Close(handle)
	setWindowsTestDirectoryMode(t, handle, mode)
}

func TestWindowsRootedCleanupPreflightRejectsBeforeAnyDisposition(t *testing.T) {
	root := t.TempDir()
	residue := filepath.Join(root, ".retained")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}
	setWindowsTestEntryCanonicalMode(t, residue, 0o700, true)
	child := filepath.Join(residue, "child.json")
	payload := []byte("payload")
	if err := os.WriteFile(child, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	setWindowsTestEntryCanonicalMode(t, child, 0o600, false)

	capability := acquireWindowsTestCommitCapability(t, residue)
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		_ = capability.Close()
		t.Fatal(err)
	}
	limits, err := mutationfs.NewTreeTraversalLimits(2, 2, int64(len(payload))-1)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRootedEntryCleanup(capability, expected, limits)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := CommitRootedEntryCleanup(t.Context(), request)
	if err == nil || outcome.State() != mutationfs.CommitOutcomeUncommitted {
		t.Fatalf("budget-exhausted cleanup = %q, %v, want uncommitted failure", outcome.State(), err)
	}
	entries, readErr := os.ReadDir(residue)
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("cleanup tree after preflight rejection = %v, %v, want untouched child", entries, readErr)
	}
	if _, statErr := os.Lstat(child); statErr != nil {
		t.Fatalf("child removed despite preflight rejection: %v", statErr)
	}
}

func TestWindowsRootedCleanupReportsResidueAfterPartialDisposition(t *testing.T) {
	root := t.TempDir()
	residue := filepath.Join(root, ".retained")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}
	setWindowsTestEntryCanonicalMode(t, residue, 0o700, true)
	for _, name := range []string{"first.json", "second.json"} {
		child := filepath.Join(residue, name)
		if err := os.WriteFile(child, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		setWindowsTestEntryCanonicalMode(t, child, 0o600, false)
	}

	capability := acquireWindowsTestCommitCapability(t, residue)
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		_ = capability.Close()
		t.Fatal(err)
	}
	request, err := NewRootedEntryCleanup(capability, expected, defaultTreeTraversalLimits())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	dispositions := 0
	faults := faultPlan{actions: map[phase]func(){
		phaseCleanupEntry: func() {
			dispositions++
			if dispositions == 2 {
				cancel()
			}
		},
	}}
	outcome, err := commitRootedEntryCleanupWithFaults(ctx, request, faults)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("partial cleanup error = %v, want context.Canceled", err)
	}
	if !hasStorageFailureKind(err, mutationfs.FailureRetainedResidue) {
		t.Fatalf("partial cleanup failure kind = %v, want retained residue", err)
	}
	if outcome.State() != mutationfs.CommitOutcomeRetainedRecoverable {
		t.Fatalf("partial cleanup outcome = %q, want retained recoverable", outcome.State())
	}
	if dispositions != 2 {
		t.Fatalf("disposition attempts = %d, want 2", dispositions)
	}
	entries, readErr := os.ReadDir(residue)
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("remaining cleanup children = %v, %v, want exactly one", entries, readErr)
	}
}

func TestWindowsRootedCleanupHonorsCancellationBeforeFirstDisposition(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, ".retained")
	if err := os.WriteFile(target, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	setWindowsTestEntryCanonicalMode(t, target, 0o600, false)

	capability := acquireWindowsTestCommitCapability(t, target)
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		_ = capability.Close()
		t.Fatal(err)
	}
	request, err := NewRootedEntryCleanup(capability, expected, defaultTreeTraversalLimits())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	faults := faultPlan{actions: map[phase]func(){phaseCleanupEntry: cancel}}
	outcome, err := commitRootedEntryCleanupWithFaults(ctx, request, faults)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled cleanup error = %v, want context.Canceled", err)
	}
	if !hasStorageFailureKind(err, mutationfs.FailureUncommitted) {
		t.Fatalf("cancelled cleanup failure kind = %v, want uncommitted", err)
	}
	if outcome.State() != mutationfs.CommitOutcomeUncommitted {
		t.Fatalf("cancelled cleanup outcome = %q, want uncommitted", outcome.State())
	}
	if _, statErr := os.Lstat(target); statErr != nil {
		t.Fatalf("cleanup entry removed despite cancellation: %v", statErr)
	}
}
