package execute

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type mutateJournalAfterInventoryStore struct {
	mutationfs.Store
	recoveryDir string
	mutated     bool
}

func (store *mutateJournalAfterInventoryStore) CaptureRootedEntryIdentity(
	ctx context.Context,
	capability rootedpath.CommitCapability,
) (mutationfs.EntryIdentity, error) {
	identity, err := store.Store.CaptureRootedEntryIdentity(ctx, capability)
	if err != nil {
		return nil, err
	}
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return nil, err
	}
	if filepath.Dir(path) == store.recoveryDir {
		if err := store.mutatePublishedJournal(filepath.Join(path, "journal.json")); err != nil {
			return nil, err
		}
	}
	return identity, nil
}

func (store *mutateJournalAfterInventoryStore) mutatePublishedJournal(path string) error {
	if store.mutated ||
		filepath.Base(path) != "journal.json" ||
		!strings.HasPrefix(filepath.Clean(path), filepath.Clean(store.recoveryDir)+string(filepath.Separator)) {
		return nil
	}
	if err := mutatePublishedJournalTimestamp(path); err != nil {
		return err
	}
	store.mutated = true
	return nil
}

func mutatePublishedJournalTimestamp(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	marker := []byte(`"created_at": "`)
	start := bytes.Index(content, marker)
	if start < 0 {
		return fmt.Errorf("recovery journal created_at is missing")
	}
	start += len(marker)
	end := bytes.IndexByte(content[start:], '"')
	if end < 0 {
		return fmt.Errorf("recovery journal created_at is unterminated")
	}
	replacement := []byte("2099-01-02T03:04:05Z")
	if end != len(replacement) {
		return fmt.Errorf("recovery journal created_at length = %d", end)
	}
	copy(content[start:start+end], replacement)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return err
	}
	return nil
}

func mutateOnlyPublishedJournal(recoveryDir string) error {
	entries, err := os.ReadDir(recoveryDir)
	if err != nil {
		return err
	}
	var journalPath string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(recoveryDir, entry.Name(), "journal.json")
		if _, err := os.Lstat(candidate); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if journalPath != "" {
			return fmt.Errorf("multiple published recovery journals found")
		}
		journalPath = candidate
	}
	if journalPath == "" {
		return fmt.Errorf("published recovery journal is missing")
	}
	return mutatePublishedJournalTimestamp(journalPath)
}

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
		!result.State.Equal(fixture.current) ||
		result.ExecutionAttempted {
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
	if result.ActionCount != 1 ||
		result.StatePath != fixture.paths.StatefilePath ||
		!result.ExecutionAttempted {
		t.Fatalf("result = %#v, want one action state path", result)
	}
	assertHostFileContent(t, fixture.hostPath("CREATE.md"), "created\n")
	if _, err := statefile.Load(t.Context(), fixture.paths.StatefilePath); err != nil {
		t.Fatalf("load statefile: %v", err)
	}
}

func TestApplyRejectsJournalChangedBetweenPublicationAndAuthorityBinding(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	input := fixture.input([]applyEventAction{action})
	physicalRoot, err := filepath.EvalSymlinks(fixture.root)
	if err != nil {
		t.Fatalf("canonicalize test root: %v", err)
	}
	store := &mutateJournalAfterInventoryStore{
		Store:       input.Filesystem,
		recoveryDir: filepath.Join(physicalRoot, ".daem", "recovery"),
	}
	input.Filesystem = store

	_, err = ApplyWithOptions(context.Background(), input, ApplyOptions{})
	if err == nil || !strings.Contains(err.Error(), "captured recovery journal changed after publication") {
		t.Fatalf(
			"ApplyWithOptions error = %v, mutated=%t, want captured-journal drift rejection",
			err,
			store.mutated,
		)
	}
	if !store.mutated {
		t.Fatal("test store did not mutate the published recovery journal")
	}
	assertHostMissing(t, fixture.hostPath("CREATE.md"))
}

func TestApplyRejectsJournalChangedAfterAuthorityBindingBeforeHostEffects(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	mutated := false
	var events []Event

	_, err := ApplyWithOptions(
		context.Background(),
		fixture.input([]applyEventAction{action}),
		ApplyOptions{
			Events: func(event Event) {
				events = append(events, event)
			},
			AcceptVisibilityChanges: func(context.Context) error {
				if mutated {
					return nil
				}
				if err := mutateOnlyPublishedJournal(fixture.paths.RecoveryDir); err != nil {
					return err
				}
				mutated = true
				return nil
			},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "captured recovery journal changed before prepared effects") {
		t.Fatalf(
			"ApplyWithOptions error = %v, mutated=%t, want pre-effect captured-journal drift rejection",
			err,
			mutated,
		)
	}
	if !mutated {
		t.Fatal("test callback did not mutate the published recovery journal")
	}
	if containsApplyEventKind(events, EventJournalCaptured) ||
		containsApplyEventKind(events, EventRollbackStageStarted) {
		t.Fatalf("journal drift crossed the prepared-effect boundary: %#v", events)
	}
	assertHostMissing(t, fixture.hostPath("CREATE.md"))
}

func TestApplyWithOptionsRecordOnlyEmitsActionEventsWithoutHostWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions are required to prove record does not write the destination")
	}
	fixture := newApplyEventFixture(t)
	action := fixture.recordAction("record", "readonly/RECORD.md", "record me\n")
	readOnlyDir := filepath.Dir(fixture.hostPath(action.Destination.String()))
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
