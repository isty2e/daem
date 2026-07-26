package execute

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyWithOptionsRejectsMissingDestinationResolverBeforeEffects(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	input := fixture.input([]applyEventAction{action})
	input.Resolver = nil
	var events []Event

	_, err := ApplyWithOptions(context.Background(), input, ApplyOptions{
		Events: func(event Event) { events = append(events, event) },
	})
	if err == nil || !strings.Contains(err.Error(), "apply destination resolver is required") {
		t.Fatalf("ApplyWithOptions error = %v, want missing-resolver rejection", err)
	}
	if len(events) != 0 {
		t.Fatalf("missing resolver emitted events: %#v", events)
	}
	assertHostMissing(t, fixture.hostPath("CREATE.md"))
	assertHostMissing(t, fixture.paths.StatefilePath)
	assertNoActiveRecoveryOperation(t, fixture.paths.RecoveryDir)
}

func TestApplyWithOptionsRejectsMissingStateCodecBeforeEffects(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	input := fixture.input([]applyEventAction{action})
	input.StateCodec = nil
	var events []Event

	_, err := ApplyWithOptions(context.Background(), input, ApplyOptions{
		Events: func(event Event) { events = append(events, event) },
	})
	if err == nil || !strings.Contains(err.Error(), "apply state codec is required") {
		t.Fatalf("ApplyWithOptions error = %v, want missing-codec rejection", err)
	}
	if len(events) != 0 {
		t.Fatalf("missing codec emitted events: %#v", events)
	}
	assertHostMissing(t, fixture.hostPath("CREATE.md"))
	assertHostMissing(t, fixture.paths.StatefilePath)
	assertNoActiveRecoveryOperation(t, fixture.paths.RecoveryDir)
}

func TestApplyWithOptionsRejectsStateEncodingFailureBeforeEffects(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	input := fixture.input([]applyEventAction{action})
	codecErr := errors.New("injected state encoding failure")
	input.StateCodec = failingStateCodec{encodeErr: codecErr}
	var events []Event

	_, err := ApplyWithOptions(context.Background(), input, ApplyOptions{
		Events: func(event Event) { events = append(events, event) },
	})
	if !errors.Is(err, codecErr) {
		t.Fatalf("ApplyWithOptions error = %v, want encoding failure", err)
	}
	if len(events) != 0 {
		t.Fatalf("encoding failure emitted events: %#v", events)
	}
	assertHostMissing(t, fixture.hostPath("CREATE.md"))
	assertHostMissing(t, fixture.paths.StatefilePath)
	assertNoActiveRecoveryOperation(t, fixture.paths.RecoveryDir)
}

func TestApplyWithOptionsRequiresFilesystemBeforeEffects(t *testing.T) {
	fixture := newApplyEventFixture(t)
	input := fixture.input([]applyEventAction{
		fixture.createAction("create", "CREATE.md", "created\n"),
	})
	input.Filesystem = nil

	_, err := ApplyWithOptions(context.Background(), input, ApplyOptions{})
	if err == nil || !strings.Contains(err.Error(), "apply filesystem is required") {
		t.Fatalf("ApplyWithOptions error = %v, want missing filesystem rejection", err)
	}
	assertHostMissing(t, fixture.hostPath("CREATE.md"))
	assertHostMissing(t, fixture.paths.StatefilePath)
	assertNoActiveRecoveryOperation(t, fixture.paths.RecoveryDir)
}

func TestApplyWithOptionsJournalCaptureFailureEmitsOnlyCaptureEvents(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	fixture.paths.RecoveryDir = filepath.Join(fixture.root, "recovery-file")
	writeApplyEventFile(t, fixture.paths.RecoveryDir, "not a directory")

	events, err := fixture.applyExpectError(action)

	if err == nil || !strings.Contains(err.Error(), "capture recovery journal") {
		t.Fatalf("error = %v, want capture recovery journal failure", err)
	}
	assertEventKinds(t, events, []EventKind{
		EventJournalCaptureStarted,
		EventJournalCaptureFailed,
	})
	if events[1].Err == nil {
		t.Fatalf("capture failed event Err = nil")
	}
}

func TestApplyWithOptionsCancellationAfterJournalRollsBackWithoutSuccess(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	ctx, cancel := context.WithCancel(context.Background())
	var events []Event
	_, err := ApplyWithOptions(ctx, fixture.input([]applyEventAction{action}), ApplyOptions{
		Events: func(event Event) {
			events = append(events, event)
			if event.Kind == EventJournalCaptured {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyWithOptions error = %v, want context cancellation", err)
	}
	assertHostMissing(t, fixture.hostPath("CREATE.md"))
	assertHostMissing(t, fixture.paths.StatefilePath)
	if containsApplyEventKind(events, EventStatefileWritten) {
		t.Fatalf("events contain statefile success after cancellation: %#v", events)
	}
}

func TestApplyWithOptionsCancellationAfterStateCommitRetainsClassifiableJournal(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	ctx, cancel := context.WithCancel(context.Background())
	var events []Event
	result, err := ApplyWithOptions(ctx, fixture.input([]applyEventAction{action}), ApplyOptions{
		Events: func(event Event) {
			events = append(events, event)
			if event.Kind == EventStatefileWritten {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyWithOptions error = %v, want context cancellation", err)
	}
	assertHostFileContent(t, fixture.hostPath("CREATE.md"), "created\n")
	assertCommittedApplyResult(t, result, fixture.paths.StatefilePath, fixture.paths.StatefilePath, 1)
	if containsApplyEventKind(events, EventJournalCleaned) {
		t.Fatalf("events cleaned journal after cancellation: %#v", events)
	}
	entries, err := os.ReadDir(fixture.paths.RecoveryDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("recovery entries = %d, err = %v, want one retained journal", len(entries), err)
	}
}
