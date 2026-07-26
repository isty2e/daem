package execute

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestApplyWithOptionsRollbackStageFailureEmitsCleanupEvents(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	var events []Event
	_, err := ApplyWithOptions(context.Background(), fixture.input([]applyEventAction{action}), ApplyOptions{
		Events: func(event Event) {
			events = append(events, event)
			if event.Kind == EventRollbackStageStarted {
				writeApplyEventFile(t, fixture.hostPath("CREATE.md"), "external\n")
			}
		},
	})

	if err == nil || !strings.Contains(err.Error(), "captured recovery journal is blocked") {
		t.Fatalf("error = %v, want guarded rollback stage failure", err)
	}
	assertEventKinds(t, events, []EventKind{
		EventJournalCaptureStarted,
		EventJournalCaptured,
		EventRollbackStageStarted,
		EventRollbackStageFailed,
		EventJournalCleanupStarted,
		EventJournalCleaned,
	})
	content, readErr := os.ReadFile(fixture.hostPath("CREATE.md"))
	if readErr != nil || string(content) != "external\n" {
		t.Fatalf("external content = %q, error = %v, want preserved", content, readErr)
	}
}

func TestApplyWithOptionsActionFailureEmitsRollbackAndCleanup(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	fixture.replacePayloadContent(action.Subject, "mismatch")

	events, err := fixture.applyExpectError(action)

	if err == nil || !strings.Contains(err.Error(), "host payload hash") {
		t.Fatalf("error = %v, want payload hash failure", err)
	}
	assertEventKinds(t, events, []EventKind{
		EventJournalCaptureStarted,
		EventJournalCaptured,
		EventRollbackStageStarted,
		EventRollbackStaged,
		EventActionStarted,
		EventActionFailed,
		EventRollbackRestoreStarted,
		EventRollbackRestored,
		EventJournalCleanupStarted,
		EventJournalCleaned,
	})
	assertHostMissing(t, fixture.hostPath("CREATE.md"))
}

func TestApplyWithOptionsStatefileWriteFailureEmitsRollbackAndCleanup(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	var events []Event
	result, err := ApplyWithOptions(context.Background(), fixture.input([]applyEventAction{action}), ApplyOptions{
		Events: func(event Event) {
			events = append(events, event)
			if event.Kind == EventStatefileWriteStarted {
				if err := os.MkdirAll(fixture.paths.StatefilePath, 0o700); err != nil {
					t.Fatalf("create statefile path directory: %v", err)
				}
			}
		},
	})

	if err == nil || !strings.Contains(err.Error(), "write statefile") {
		t.Fatalf("result = %#v, error = %v, want statefile write failure", result, err)
	}
	assertEventKinds(t, events, []EventKind{
		EventJournalCaptureStarted,
		EventJournalCaptured,
		EventRollbackStageStarted,
		EventRollbackStaged,
		EventActionStarted,
		EventActionDone,
		EventStatefileWriteStarted,
		EventStatefileWriteFailed,
		EventRollbackRestoreStarted,
		EventRollbackRestored,
		EventJournalCleanupStarted,
		EventJournalCleaned,
	})
	assertHostMissing(t, fixture.hostPath("CREATE.md"))
}

func TestApplyWithOptionsIgnoresUntouchedPathDriftDuringRollback(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.updateAction("update", "UPDATE.md", "old update\n", "new update\n")
	fixture.replacePayloadContent(action.Subject, "mismatch")
	var events []Event
	result, err := ApplyWithOptions(context.Background(), fixture.input([]applyEventAction{action}), ApplyOptions{
		Events: func(event Event) {
			events = append(events, event)
			if event.Kind == EventRollbackRestoreStarted {
				hostPath := fixture.hostPath("UPDATE.md")
				if err := os.Remove(hostPath); err != nil && !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("remove host path before rollback: %v", err)
				}
				if err := os.MkdirAll(hostPath, 0o700); err != nil {
					t.Fatalf("create host path directory before rollback: %v", err)
				}
			}
		},
	})

	if err == nil || !strings.Contains(err.Error(), "host changes rolled back") {
		t.Fatalf("result = %#v, error = %v, want clean rollback of untouched action", result, err)
	}
	assertEventKinds(t, events, []EventKind{
		EventJournalCaptureStarted,
		EventJournalCaptured,
		EventRollbackStageStarted,
		EventRollbackStaged,
		EventActionStarted,
		EventActionFailed,
		EventRollbackRestoreStarted,
		EventRollbackRestored,
		EventJournalCleanupStarted,
		EventJournalCleaned,
	})
	info, statErr := os.Stat(fixture.hostPath("UPDATE.md"))
	if statErr != nil || !info.IsDir() {
		t.Fatalf("untouched external replacement = %#v, error = %v, want preserved directory", info, statErr)
	}
}

func TestApplyWithOptionsRefusesRollbackOverTouchedPathDrift(t *testing.T) {
	fixture := newApplyEventFixture(t)
	first := fixture.createAction("first", "FIRST.md", "first\n")
	second := fixture.createAction("second", "SECOND.md", "second\n")
	fixture.replacePayloadContent(second.Subject, "mismatch")
	var events []Event

	_, err := ApplyWithOptions(context.Background(), fixture.input([]applyEventAction{first, second}), ApplyOptions{
		Events: func(event Event) {
			events = append(events, event)
			if event.Kind == EventActionDone && event.Action != nil && event.Action.Destination == first.Destination {
				writeApplyEventFile(t, fixture.hostPath("FIRST.md"), "external\n")
			}
		},
	})

	if err == nil || !strings.Contains(err.Error(), "guarded rollback is blocked by current evidence") {
		t.Fatalf("error = %v, want touched-path drift refusal", err)
	}
	content, readErr := os.ReadFile(fixture.hostPath("FIRST.md"))
	if readErr != nil || string(content) != "external\n" {
		t.Fatalf("touched external content = %q, error = %v, want preserved", content, readErr)
	}
	assertHostMissing(t, fixture.hostPath("SECOND.md"))
	assertEventKinds(t, events, []EventKind{
		EventJournalCaptureStarted,
		EventJournalCaptured,
		EventRollbackStageStarted,
		EventRollbackStaged,
		EventActionStarted,
		EventActionDone,
		EventActionStarted,
		EventActionFailed,
		EventRollbackRestoreStarted,
		EventRollbackRestoreFailed,
	})
	assertNoEventKind(t, events, EventJournalCleaned)
	entries, readDirErr := os.ReadDir(fixture.paths.RecoveryDir)
	if readDirErr != nil || len(entries) != 1 {
		t.Fatalf("recovery entries = %#v, error = %v, want retained journal", entries, readDirErr)
	}
}

func TestApplyWithOptionsRollbackRestoresExecutableHookAssetMode(t *testing.T) {
	fixture := newApplyEventFixture(t)
	first := fixture.updateExecutableFileAction(t, "guard", ".daem/hook-assets/guard/old/asset", "old\n", "new\n")
	second := fixture.createAction("second", "SECOND.md", "second\n")
	fixture.replacePayloadContent(second.Subject, "mismatch")

	_, err := ApplyWithOptions(context.Background(), fixture.input([]applyEventAction{first, second}), ApplyOptions{})
	if err == nil || !strings.Contains(err.Error(), "host changes rolled back") {
		t.Fatalf("error = %v, want guarded rollback", err)
	}
	content, readErr := os.ReadFile(fixture.hostPath(string(first.Destination)))
	if !errors.Is(readErr, os.ErrNotExist) {
		t.Fatalf("rolled-back new destination content = %q, error = %v, want absent", content, readErr)
	}
	oldDestination := first.PreviousState.Destination
	content, readErr = os.ReadFile(fixture.hostPath(string(oldDestination)))
	if readErr != nil || string(content) != "old\n" {
		t.Fatalf("restored old content = %q, error = %v, want old content", content, readErr)
	}
	info, statErr := os.Stat(fixture.hostPath(string(oldDestination)))
	if statErr != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("restored mode = %v, error = %v, want 0700", info, statErr)
	}
	assertNoActiveRecoveryOperation(t, fixture.paths.RecoveryDir)
}

func TestApplyWithOptionsRollbackRestoresNonExecutablePermissionMode(t *testing.T) {
	fixture := newApplyEventFixture(t)
	first := fixture.updateAction("first", "FIRST.md", "old\n", "new\n")
	first.LiveFileMode = 0o640
	first.DesiredFileMode = 0o600
	if err := os.Chmod(fixture.hostPath(string(first.PreviousState.Destination)), 0o640); err != nil {
		t.Fatalf("chmod before path: %v", err)
	}
	second := fixture.createAction("second", "SECOND.md", "second\n")
	fixture.replacePayloadContent(second.Subject, "mismatch")

	_, err := ApplyWithOptions(context.Background(), fixture.input([]applyEventAction{first, second}), ApplyOptions{})
	if err == nil || !strings.Contains(err.Error(), "host changes rolled back") {
		t.Fatalf("error = %v, want guarded rollback", err)
	}
	assertHostMissing(t, fixture.hostPath(string(first.Destination)))
	info, statErr := os.Stat(fixture.hostPath(string(first.PreviousState.Destination)))
	if statErr != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("restored mode = %v, error = %v, want 0640", info, statErr)
	}
	assertNoActiveRecoveryOperation(t, fixture.paths.RecoveryDir)
}

func TestApplyWithOptionsCancellationAfterSuccessfulEffectUsesGuardedRollback(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.updateExecutableFileAction(t, "guard", ".daem/hook-assets/guard/cancel/asset", "old\n", "new\n")
	ctx, cancel := context.WithCancel(context.Background())
	var events []Event

	_, err := ApplyWithOptions(ctx, fixture.input([]applyEventAction{action}), ApplyOptions{
		Events: func(event Event) {
			events = append(events, event)
			if event.Kind == EventActionDone {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "host changes rolled back") {
		t.Fatalf("error = %v, want canceled apply with guarded rollback", err)
	}
	content, readErr := os.ReadFile(fixture.hostPath(string(action.Destination)))
	if !errors.Is(readErr, os.ErrNotExist) {
		t.Fatalf("rolled-back new destination content = %q, error = %v, want absent", content, readErr)
	}
	oldDestination := action.PreviousState.Destination
	content, readErr = os.ReadFile(fixture.hostPath(string(oldDestination)))
	if readErr != nil || string(content) != "old\n" {
		t.Fatalf("restored old content = %q, error = %v, want old content", content, readErr)
	}
	info, statErr := os.Stat(fixture.hostPath(string(oldDestination)))
	if statErr != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("restored mode = %v, error = %v, want 0700", info, statErr)
	}
	assertNoEventKind(t, events, EventStatefileWritten)
	assertNoActiveRecoveryOperation(t, fixture.paths.RecoveryDir)
}

func TestApplyWithOptionsRefusesRollbackOverTouchedModeDrift(t *testing.T) {
	fixture := newApplyEventFixture(t)
	first := fixture.createAction("first", "FIRST.md", "first\n")
	second := fixture.createAction("second", "SECOND.md", "second\n")
	fixture.replacePayloadContent(second.Subject, "mismatch")

	_, err := ApplyWithOptions(context.Background(), fixture.input([]applyEventAction{first, second}), ApplyOptions{
		Events: func(event Event) {
			if event.Kind == EventActionDone && event.Action != nil && event.Action.Destination == first.Destination {
				if err := os.Chmod(fixture.hostPath("FIRST.md"), 0o640); err != nil {
					t.Fatalf("chmod touched path: %v", err)
				}
			}
		},
	})
	if err == nil || !strings.Contains(err.Error(), "guarded rollback is blocked by current evidence") {
		t.Fatalf("error = %v, want mode-drift rollback refusal", err)
	}
	info, statErr := os.Stat(fixture.hostPath("FIRST.md"))
	if statErr != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("touched mode = %v, error = %v, want external 0640 preserved", info, statErr)
	}
	entries, readDirErr := os.ReadDir(fixture.paths.RecoveryDir)
	if readDirErr != nil || len(entries) != 1 {
		t.Fatalf("recovery entries = %#v, error = %v, want retained journal", entries, readDirErr)
	}
}

func TestApplyWithOptionsJournalCleanupFailureDoesNotRollbackAfterStatefileWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions are required to force cleanup failure")
	}
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	var events []Event
	result, err := ApplyWithOptions(context.Background(), fixture.input([]applyEventAction{action}), ApplyOptions{
		Events: func(event Event) {
			events = append(events, event)
			if event.Kind == EventJournalCleanupStarted {
				if err := os.Chmod(fixture.paths.RecoveryDir, 0o500); err != nil {
					t.Fatalf("chmod recovery dir: %v", err)
				}
			}
		},
	})
	t.Cleanup(func() {
		_ = os.Chmod(fixture.paths.RecoveryDir, 0o700)
	})

	if err == nil || !strings.Contains(err.Error(), "remove recovery journal") {
		t.Fatalf("result = %#v, error = %v, want cleanup failure", result, err)
	}
	assertEventKinds(t, events, []EventKind{
		EventJournalCaptureStarted,
		EventJournalCaptured,
		EventRollbackStageStarted,
		EventRollbackStaged,
		EventActionStarted,
		EventActionDone,
		EventStatefileWriteStarted,
		EventStatefileWritten,
		EventJournalCleanupStarted,
		EventJournalCleanupFailed,
	})
	assertNoEventKind(t, events, EventRollbackRestoreStarted)
	assertHostFileContent(t, fixture.hostPath("CREATE.md"), "created\n")
	if _, statErr := os.Stat(fixture.paths.StatefilePath); statErr != nil {
		t.Fatalf("statefile stat after cleanup failure: %v", statErr)
	}
}
