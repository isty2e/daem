package execute

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"runtime"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type indeterminateCreateFilesystem struct {
	mutationfs.Store
	target  string
	failure error
}

func (filesystem indeterminateCreateFilesystem) CreateRootedFile(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	content []byte,
	mode fs.FileMode,
) error {
	if capability.Destination().Relative().Path() != filesystem.target {
		return filesystem.Store.CreateRootedFile(ctx, capability, content, mode)
	}
	if err := filesystem.Store.CreateRootedFile(
		ctx,
		capability,
		[]byte("indeterminate\n"),
		mode,
	); err != nil {
		return err
	}
	return filesystem.failure
}

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

func TestApplyPreparedEffectRollbackRetirementFailurePreservesPrimary(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	retirement := errors.New("injected prepared-effect retirement acceptance failure")

	var events []Event
	_, err := ApplyWithOptions(context.Background(), fixture.input([]applyEventAction{action}), ApplyOptions{
		Events: func(event Event) {
			events = append(events, event)
			if event.Kind == EventRollbackStageStarted {
				writeApplyEventFile(t, fixture.hostPath("CREATE.md"), "external\n")
			}
		},
		AcceptCompensationVisibilityChanges: func(context.Context) error {
			return retirement
		},
	})
	if err == nil || !strings.Contains(err.Error(), "captured recovery journal is blocked") ||
		!strings.Contains(err.Error(), retirement.Error()) {
		t.Fatalf("ApplyWithOptions error = %v, want primary and retirement failures", err)
	}
	assertEventKinds(t, events, []EventKind{
		EventJournalCaptureStarted,
		EventJournalCaptured,
		EventRollbackStageStarted,
		EventRollbackStageFailed,
		EventJournalCleanupStarted,
		EventJournalCleanupFailed,
	})
	assertHostFileContent(t, fixture.hostPath("CREATE.md"), "external\n")
	assertNoActiveRecoveryOperation(t, fixture.paths.RecoveryDir)
}

func TestApplyWithOptionsActionFailureEmitsRollbackAndCleanup(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	fixture.replacePayloadContent(action.Subject, "mismatch")

	events, err := fixture.applyExpectError(action)

	if err == nil || !strings.Contains(err.Error(), "host payload hash") {
		t.Fatalf("error = %v, want payload hash failure", err)
	}
	if strings.Contains(err.Error(), "failure settlement") ||
		strings.Contains(err.Error(), "effect structure") {
		t.Fatalf("guarded rollback exposed cursor failure: %v", err)
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

func TestApplyWithEffectPlanIndeterminateHostOutcomeRetainsJournal(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	input := fixture.input([]applyEventAction{action})
	wantErr := errors.New("injected indeterminate create failure")
	input.Filesystem = indeterminateCreateFilesystem{
		Store:   input.Filesystem,
		target:  action.Destination.String(),
		failure: wantErr,
	}
	plan, err := PrepareApplyEffectPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	var events []Event
	_, err = ApplyWithEffectPlan(context.Background(), input, plan, ApplyOptions{
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("ApplyWithEffectPlan error = %v, want injected failure", err)
	}
	if !strings.Contains(err.Error(), "outcome is indeterminate") ||
		!strings.Contains(err.Error(), "recovery journal retained") {
		t.Fatalf("ApplyWithEffectPlan error = %v, want indeterminate recovery guidance", err)
	}
	if ApplyHostChangesRolledBack(err) {
		t.Fatalf("ApplyWithEffectPlan error = %v, want no rollback claim", err)
	}
	assertEventKinds(t, events, []EventKind{
		EventJournalCaptureStarted,
		EventJournalCaptured,
		EventRollbackStageStarted,
		EventRollbackStaged,
		EventActionStarted,
		EventActionFailed,
		EventRollbackRestoreStarted,
		EventRollbackRestoreFailed,
	})
	assertNoEventKind(t, events, EventJournalCleanupStarted)
	assertHostFileContent(t, fixture.hostPath("CREATE.md"), "indeterminate\n")
	assertActiveRecoveryOperation(t, fixture.paths.RecoveryDir)
}

func TestApplyWithEffectPlanGuardedRollbackRetirementFailurePreservesErrors(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	input := fixture.input([]applyEventAction{action})
	plan, err := PrepareApplyEffectPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	primary := errors.New("injected forward visibility rejection")
	retirement := errors.New("injected rollback retirement acceptance failure")
	forwardAccepts := 0
	compensationAccepts := 0
	_, err = ApplyWithEffectPlan(context.Background(), input, plan, ApplyOptions{
		AcceptVisibilityChanges: func(context.Context) error {
			forwardAccepts++
			if forwardAccepts == 2 {
				return primary
			}
			return nil
		},
		AcceptCompensationVisibilityChanges: func(context.Context) error {
			compensationAccepts++
			if compensationAccepts == 2 {
				return retirement
			}
			return nil
		},
	})
	if !errors.Is(err, primary) || !errors.Is(err, retirement) {
		t.Fatalf("ApplyWithEffectPlan error = %v, want primary and retirement failures", err)
	}
	if !ApplyHostChangesRolledBack(err) {
		t.Fatalf("ApplyWithEffectPlan error = %v, want rolled-back classification", err)
	}
	if forwardAccepts != 2 {
		t.Fatalf("forward acceptance calls = %d, want 2", forwardAccepts)
	}
	if compensationAccepts != 2 {
		t.Fatalf("compensation acceptance calls = %d, want 2", compensationAccepts)
	}
	assertHostMissing(t, fixture.hostPath("CREATE.md"))
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestApplyWithEffectPlanGuardedRollbackJoinsMutationAuthorityCloseFailure(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	input := fixture.input([]applyEventAction{action})
	plan, err := PrepareApplyEffectPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	primary := errors.New("injected forward visibility rejection")
	closeFailure := errors.New("injected mutation-authority close failure")
	forwardAccepts := 0
	_, err = ApplyWithEffectPlan(context.Background(), input, plan, ApplyOptions{
		AcceptVisibilityChanges: func(context.Context) error {
			forwardAccepts++
			if forwardAccepts == 2 {
				return primary
			}
			return nil
		},
		closeMutationAuthority: func(authority *mutationAuthority) error {
			return errors.Join(authority.close(), closeFailure)
		},
	})
	if !errors.Is(err, primary) || !errors.Is(err, closeFailure) {
		t.Fatalf("ApplyWithEffectPlan error = %v, want primary and close failures", err)
	}
	if !ApplyHostChangesRolledBack(err) {
		t.Fatalf("ApplyWithEffectPlan error = %v, want rolled-back classification", err)
	}
	assertHostMissing(t, fixture.hostPath("CREATE.md"))
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func assertActiveRecoveryOperation(t *testing.T, recoveryDir string) {
	t.Helper()
	entries, err := os.ReadDir(recoveryDir)
	if err != nil {
		t.Fatalf("read recovery directory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("recovery entries = %d, want 1", len(entries))
	}
}

func TestApplyWithOptionsCompensatesAfterForwardVisibilityRejection(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	forwardAccepts := 0
	compensationValidations := 0
	compensationAccepts := 0

	_, err := ApplyWithOptions(
		context.Background(),
		fixture.input([]applyEventAction{action}),
		ApplyOptions{
			AcceptVisibilityChanges: func(context.Context) error {
				forwardAccepts++
				if forwardAccepts == 2 {
					return errors.New("injected forward visibility rejection")
				}
				return nil
			},
			ValidateCompensationAuthority: func(context.Context) error {
				compensationValidations++
				return nil
			},
			AcceptCompensationVisibilityChanges: func(context.Context) error {
				compensationAccepts++
				return nil
			},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "injected forward visibility rejection") ||
		!strings.Contains(err.Error(), "host changes rolled back") {
		t.Fatalf("error = %v, want rejected forward visibility with completed compensation", err)
	}
	if forwardAccepts != 2 {
		t.Fatalf("forward visibility accepts = %d, want journal and host effect", forwardAccepts)
	}
	if compensationValidations == 0 || compensationAccepts == 0 {
		t.Fatalf(
			"compensation gate calls = validate:%d accept:%d, want both",
			compensationValidations,
			compensationAccepts,
		)
	}
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
	content, readErr := os.ReadFile(fixture.hostPath(first.Destination.String()))
	if !errors.Is(readErr, os.ErrNotExist) {
		t.Fatalf("rolled-back new destination content = %q, error = %v, want absent", content, readErr)
	}
	oldDestination := first.PreviousState.Destination
	content, readErr = os.ReadFile(fixture.hostPath(oldDestination.String()))
	if readErr != nil || string(content) != "old\n" {
		t.Fatalf("restored old content = %q, error = %v, want old content", content, readErr)
	}
	info, statErr := os.Stat(fixture.hostPath(oldDestination.String()))
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
	if err := os.Chmod(fixture.hostPath(first.PreviousState.Destination.String()), 0o640); err != nil {
		t.Fatalf("chmod before path: %v", err)
	}
	second := fixture.createAction("second", "SECOND.md", "second\n")
	fixture.replacePayloadContent(second.Subject, "mismatch")

	_, err := ApplyWithOptions(context.Background(), fixture.input([]applyEventAction{first, second}), ApplyOptions{})
	if err == nil || !strings.Contains(err.Error(), "host changes rolled back") {
		t.Fatalf("error = %v, want guarded rollback", err)
	}
	assertHostMissing(t, fixture.hostPath(first.Destination.String()))
	info, statErr := os.Stat(fixture.hostPath(first.PreviousState.Destination.String()))
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
	content, readErr := os.ReadFile(fixture.hostPath(action.Destination.String()))
	if !errors.Is(readErr, os.ErrNotExist) {
		t.Fatalf("rolled-back new destination content = %q, error = %v, want absent", content, readErr)
	}
	oldDestination := action.PreviousState.Destination
	content, readErr = os.ReadFile(fixture.hostPath(oldDestination.String()))
	if readErr != nil || string(content) != "old\n" {
		t.Fatalf("restored old content = %q, error = %v, want old content", content, readErr)
	}
	info, statErr := os.Stat(fixture.hostPath(oldDestination.String()))
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

	if err == nil || !strings.Contains(err.Error(), "retire recovery journal") {
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
