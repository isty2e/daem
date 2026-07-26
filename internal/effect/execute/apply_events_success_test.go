package execute

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
)

func TestApplyWithOptionsSuccessfulApplyEmitsEventsInOrder(t *testing.T) {
	fixture := newApplyEventFixture(t)
	createAction := fixture.createAction("create", "CREATE.md", "created\n")
	updateAction := fixture.updateAction("update", "UPDATE.md", "old update\n", "new update\n")
	deleteAction := fixture.deleteAction("delete", "DELETE.md", "delete me\n")
	recordAction := fixture.recordAction("record", "RECORD.md", "record me\n")
	actions := []applyEventAction{createAction, updateAction, deleteAction, recordAction}
	events := fixture.applyWithEvents(t, actions)

	assertEventKinds(t, events, []EventKind{
		EventJournalCaptureStarted,
		EventJournalCaptured,
		EventRollbackStageStarted,
		EventRollbackStaged,
		EventActionStarted,
		EventActionDone,
		EventActionStarted,
		EventActionStarted,
		EventActionDone,
		EventActionDone,
		EventActionStarted,
		EventActionDone,
		EventStatefileWriteStarted,
		EventStatefileWritten,
		EventJournalCleanupStarted,
		EventJournalCleaned,
	})
	assertActionEventIndexes(t, events, []int{0, 0, 1, 3, 3, 1, 2, 2})
	assertAllEventsTotalActions(t, events, len(actions))
	assertApplyEventNoErrors(t, events)
	assertHostFileContent(t, fixture.hostPath("CREATE.md"), "created\n")
	assertHostFileContent(t, fixture.hostPath("UPDATE.md"), "new update\n")
	assertHostMissing(t, fixture.hostPath("DELETE.md"))
	assertHostFileContent(t, fixture.hostPath("RECORD.md"), "record me\n")
}

func TestApplyWithOptionsNoOpDoesNotRequireStatePersistence(t *testing.T) {
	fixture := newApplyEventFixture(t)

	result, err := ApplyWithOptions(context.Background(), ApplyInput{
		Paths:        fixture.paths,
		Resolver:     destinationResolver(fixture.paths),
		CurrentState: fixture.current,
	}, ApplyOptions{})
	if err != nil {
		t.Fatalf("no-op ApplyWithOptions returned error: %v", err)
	}
	if result.ActionCount != 0 ||
		result.StatePath != fixture.paths.StatefilePath ||
		!result.State.Equal(fixture.current) {
		t.Fatalf("no-op result = %#v, want unchanged state and selected path", result)
	}
	assertHostMissing(t, fixture.paths.StatefilePath)
	assertNoActiveRecoveryOperation(t, fixture.paths.RecoveryDir)
}

func TestApplyWithOptionsNilSinkPreservesSuccessfulApplyBehavior(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")

	result, err := ApplyWithOptions(context.Background(), fixture.input([]applyEventAction{action}), ApplyOptions{})
	if err != nil {
		t.Fatalf("ApplyWithOptions returned error: %v", err)
	}
	if result.ActionCount != 1 || result.StatePath != fixture.paths.StatefilePath {
		t.Fatalf("result = %#v, want one action state path", result)
	}
	assertHostFileContent(t, fixture.hostPath("CREATE.md"), "created\n")
	if _, err := statefile.Load(t.Context(), fixture.paths.StatefilePath); err != nil {
		t.Fatalf("load statefile: %v", err)
	}
}

func TestApplyWithOptionsRecordOnlyEmitsActionEventsWithoutHostWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions are required to prove record does not write the destination")
	}
	fixture := newApplyEventFixture(t)
	action := fixture.recordAction("record", "readonly/RECORD.md", "record me\n")
	readOnlyDir := filepath.Dir(fixture.hostPath(string(action.Destination)))
	if err := os.Chmod(readOnlyDir, 0o500); err != nil {
		t.Fatalf("chmod readonly directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(readOnlyDir, 0o700)
	})

	events := fixture.applyWithEvents(t, []applyEventAction{action})

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
		EventJournalCleaned,
	})
	assertActionEventIndexes(t, events, []int{0, 0})
	assertHostFileContent(t, fixture.hostPath("readonly/RECORD.md"), "record me\n")
}

func TestApplyWithOptionsZeroActionsEmitsNoEvents(t *testing.T) {
	fixture := newApplyEventFixture(t)
	var events []Event
	result, err := ApplyWithOptions(context.Background(), fixture.input(nil), ApplyOptions{
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("ApplyWithOptions returned error: %v", err)
	}
	if result.ActionCount != 0 || result.StatePath != fixture.paths.StatefilePath {
		t.Fatalf("result = %#v, want zero action state path", result)
	}
	if len(events) != 0 {
		t.Fatalf("events = %#v, want none", events)
	}
	assertHostMissing(t, fixture.paths.StatefilePath)
}
