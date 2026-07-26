//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestCommitFileFaultClassification(t *testing.T) {
	tests := []struct {
		phase phase
		kind  mutationfs.FailureKind
		seen  bool
	}{
		{phase: phaseValidate, kind: failureUncommitted},
		{phase: phaseCreateAncestors, kind: failureUncommitted},
		{phase: phaseCreateTemporary, kind: failureUncommitted},
		{phase: phaseWritePayload, kind: failureUncommitted},
		{phase: phaseApplyMode, kind: failureUncommitted},
		{phase: phaseApplyMetadata, kind: failureUncommitted},
		{phase: phaseSyncPayload, kind: failureUncommitted},
		{phase: phaseClosePayload, kind: failureUncommitted},
		{phase: phaseCommitEntry, kind: failureUncommitted},
		{phase: phaseVerifyEntry, kind: failureIndeterminateCommit, seen: true},
		{phase: phaseSyncParent, kind: failureIndeterminateCommit, seen: true},
		{phase: phaseSyncAncestors, kind: failureIndeterminateCommit, seen: true},
	}
	for _, test := range tests {
		t.Run(string(test.phase), func(t *testing.T) {
			root := canonicalTempDir(t)
			target := filepath.Join(root, "state.json")
			if test.phase == phaseSyncAncestors {
				target = filepath.Join(root, "created", "state.json")
			}
			request, err := NewFileCreate(target, []byte("payload"), 0o600)
			if err != nil {
				t.Fatalf("NewFileCreate returned error: %v", err)
			}
			err = commitFileWithFaults(context.Background(), request, faultAt(test.phase))
			assertFailure(t, err, test.kind, test.phase)
			_, statErr := os.Lstat(target)
			if test.seen && statErr != nil {
				t.Fatalf("target error = %v, want visible", statErr)
			}
			if !test.seen && !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("target error = %v, want not exist", statErr)
			}
			assertNoPrivateEntries(t, root)
		})
	}
}

func TestCommitFileReportsCleanupResidue(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "created", "nested", "state.json")
	request, err := NewFileCreate(target, []byte("payload"), 0o600)
	if err != nil {
		t.Fatalf("NewFileCreate returned error: %v", err)
	}
	faults := faultPlan{failures: map[phase]error{
		phaseWritePayload:     errors.New("write fault"),
		phaseCleanupTemporary: errors.New("temporary cleanup fault"),
		phaseCleanupAncestors: errors.New("ancestor cleanup fault"),
	}}
	err = commitFileWithFaults(context.Background(), request, faults)
	failure := assertFailure(t, err, failureUncommitted, phaseWritePayload)
	if len(failure.retainedResidue()) < 3 {
		t.Fatalf("residue = %v, want temporary and created ancestors", failure.retainedResidue())
	}
}

func TestCommitFileDoesNotPublishAfterPartialOrFinalWriteError(t *testing.T) {
	tests := []struct {
		name       string
		writeBytes int
	}{
		{name: "partial write", writeBytes: 4},
		{name: "complete write with final error", writeBytes: len("payload")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := canonicalTempDir(t)
			target := filepath.Join(root, "state.json")
			request, err := NewFileCreate(target, []byte("payload"), 0o600)
			if err != nil {
				t.Fatalf("NewFileCreate returned error: %v", err)
			}
			faults := faultPlan{payloadWrite: func(_ context.Context, writer io.Writer, payload []byte) error {
				if _, err := writer.Write(payload[:test.writeBytes]); err != nil {
					return err
				}
				return errors.New("injected write completion error")
			}}
			err = commitFileWithFaults(context.Background(), request, faults)
			assertFailure(t, err, failureUncommitted, phaseWritePayload)
			if _, statErr := os.Lstat(target); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("target error = %v, want not exist", statErr)
			}
			assertNoPrivateEntries(t, root)
		})
	}
}

func TestCommitFileClassifiesUnsupportedSyncByVisibility(t *testing.T) {
	tests := []struct {
		name  string
		phase phase
		kind  mutationfs.FailureKind
		seen  bool
	}{
		{name: "payload sync", phase: phaseSyncPayload, kind: failureUnsupportedGuarantee},
		{name: "parent sync", phase: phaseSyncParent, kind: failureIndeterminateCommit, seen: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := canonicalTempDir(t)
			target := filepath.Join(root, "state.json")
			request, err := NewFileCreate(target, []byte("payload"), 0o600)
			if err != nil {
				t.Fatalf("NewFileCreate returned error: %v", err)
			}
			faults := faultPlan{failures: map[phase]error{
				test.phase: unsupported("injected unsupported sync", nil),
			}}
			err = commitFileWithFaults(context.Background(), request, faults)
			assertFailure(t, err, test.kind, test.phase)
			_, statErr := os.Lstat(target)
			if test.seen && statErr != nil {
				t.Fatalf("target error = %v, want visible", statErr)
			}
			if !test.seen && !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("target error = %v, want not exist", statErr)
			}
		})
	}
}

func TestCommitFileMetadataCaptureFault(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "state.json")
	writeTestFile(t, target, "before", 0o600)
	request, err := NewFileReplacement(target, []byte("after"), 0o600, captureIdentity(t, target))
	if err != nil {
		t.Fatalf("NewFileReplacement returned error: %v", err)
	}
	err = commitFileWithFaults(context.Background(), request, faultAt(phaseCaptureMetadata))
	assertFailure(t, err, failureUncommitted, phaseCaptureMetadata)
	assertFile(t, target, "before", 0o600)
}

func TestCommitFileFinalRevalidationFault(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "state.json")
	writeTestFile(t, target, "before", 0o600)
	request, err := NewFileReplacement(target, []byte("after"), 0o600, captureIdentity(t, target))
	if err != nil {
		t.Fatalf("NewFileReplacement returned error: %v", err)
	}
	err = commitFileWithFaults(context.Background(), request, faultAt(phaseRevalidateEntry))
	assertFailure(t, err, failureUncommitted, phaseRevalidateEntry)
	assertFile(t, target, "before", 0o600)
	assertNoPrivateEntries(t, root)
}

func TestCommitFileSkipsAbsentAncestorSyncPhase(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "state.json")
	request, err := NewFileCreate(target, []byte("payload"), 0o600)
	if err != nil {
		t.Fatalf("NewFileCreate returned error: %v", err)
	}
	if err := commitFileWithFaults(context.Background(), request, faultAt(phaseSyncAncestors)); err != nil {
		t.Fatalf("commitFileWithFaults returned error for absent ancestor work: %v", err)
	}
	assertFile(t, target, "payload", 0o600)
}

func TestCommitFileProcessInterruptionBoundaries(t *testing.T) {
	tests := []struct {
		name        string
		helperPhase string
		visible     bool
		temporary   bool
	}{
		{name: "before visibility", helperPhase: "pre", temporary: true},
		{name: "after visibility", helperPhase: "post", visible: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := canonicalTempDir(t)
			target := filepath.Join(root, "state.json")
			command := exec.Command(os.Args[0], "-test.run=^TestCommitFileInterruptionHelper$")
			command.Env = append(
				os.Environ(),
				"DAEM_COMMIT_HELPER_PHASE="+test.helperPhase,
				"DAEM_COMMIT_HELPER_TARGET="+target,
			)
			err := command.Run()
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) || exitError.ExitCode() != 73 {
				t.Fatalf("helper error = %v, want exit code 73", err)
			}
			payload, readErr := os.ReadFile(target)
			if test.visible {
				if readErr != nil || string(payload) != "complete-payload" {
					t.Fatalf("visible payload = %q, error = %v", payload, readErr)
				}
			} else if !errors.Is(readErr, os.ErrNotExist) {
				t.Fatalf("target error = %v, want not exist", readErr)
			}
			if got := directoryHasPrefix(t, root, temporaryPrefix); got != test.temporary {
				t.Fatalf("temporary residue = %t, want %t", got, test.temporary)
			}
		})
	}
}

func TestCommitFileInterruptionHelper(t *testing.T) {
	helperPhase := os.Getenv("DAEM_COMMIT_HELPER_PHASE")
	if helperPhase == "" {
		t.Skip("subprocess helper")
	}
	target := os.Getenv("DAEM_COMMIT_HELPER_TARGET")
	request, err := NewFileCreate(target, []byte("complete-payload"), 0o600)
	if err != nil {
		t.Fatalf("NewFileCreate returned error: %v", err)
	}
	interruptionPhase := phaseSyncPayload
	if helperPhase == "post" {
		interruptionPhase = phaseSyncParent
	}
	faults := faultPlan{actions: map[phase]func(){interruptionPhase: func() { os.Exit(73) }}}
	if err := commitFileWithFaults(context.Background(), request, faults); err != nil {
		t.Fatalf("commitFileWithFaults returned before interruption: %v", err)
	}
	t.Fatal("commitFileWithFaults returned without interruption")
}
