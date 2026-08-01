//go:build darwin || linux

package execute

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func TestApplyRejectsProjectRootReplacementBeforeJournalPublication(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	movedRoot := fixture.root + "-moved"
	input := fixture.input([]applyEventAction{action})
	filesystem := &rootSwapBeforeRootedTreeStore{
		Store: input.Filesystem,
		swap: func() {
			replaceSelectedRoot(t, fixture.root, movedRoot)
		},
	}
	input.Filesystem = filesystem

	_, err := ApplyWithOptions(t.Context(), input, ApplyOptions{})
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("ApplyWithOptions error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
	if filesystem.swapCount() != 1 {
		t.Fatalf("project-root swaps = %d, want 1", filesystem.swapCount())
	}
	assertHostMissing(t, fixture.hostPath("CREATE.md"))
	assertHostMissing(t, movedFixturePath(fixture, movedRoot, fixture.hostPath("CREATE.md")))
	assertHostMissing(t, fixture.paths.StatefilePath)
	assertHostMissing(t, filepath.Join(movedRoot, ".daem", "state.json"))
	assertActiveRecoveryOperationCount(t, fixture.paths.RecoveryDir, 0)
	assertActiveRecoveryOperationCount(
		t,
		filepath.Join(movedRoot, ".daem", "recovery"),
		0,
	)
}

func TestApplyRejectsExternalStateRootReplacementBeforeJournalPublication(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	stateRoot := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(stateRoot, 0o700); err != nil {
		t.Fatalf("create external state root: %v", err)
	}
	movedStateRoot := stateRoot + "-moved"
	input := fixture.input([]applyEventAction{action})
	input.Paths.StateDir = stateRoot
	input.Paths.StatefilePath = filepath.Join(stateRoot, "state.json")
	input.Paths.RecoveryDir = filepath.Join(stateRoot, "recovery")
	input.Resolver = destinationResolver(input.Paths)
	filesystem := &rootSwapBeforeRootedTreeStore{
		Store: input.Filesystem,
		swap: func() {
			replaceSelectedRoot(t, stateRoot, movedStateRoot)
		},
	}
	input.Filesystem = filesystem

	_, err := ApplyWithOptions(t.Context(), input, ApplyOptions{})
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("ApplyWithOptions error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
	if filesystem.swapCount() != 1 {
		t.Fatalf("state-root swaps = %d, want 1", filesystem.swapCount())
	}
	assertHostMissing(t, fixture.hostPath("CREATE.md"))
	assertHostMissing(t, input.Paths.StatefilePath)
	assertHostMissing(t, filepath.Join(movedStateRoot, "state.json"))
	assertActiveRecoveryOperationCount(t, input.Paths.RecoveryDir, 0)
	assertActiveRecoveryOperationCount(
		t,
		filepath.Join(movedStateRoot, "recovery"),
		0,
	)
}

func TestApplyCommitsStateThroughCapturedRootAndRejectsSelectedRootReplacement(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	movedRoot := fixture.root + "-moved"
	var events []Event

	_, err := ApplyWithOptions(
		context.Background(),
		fixture.input([]applyEventAction{action}),
		ApplyOptions{Events: func(event Event) {
			events = append(events, event)
			if event.Kind == EventStatefileWriteStarted {
				replaceSelectedRoot(t, fixture.root, movedRoot)
			}
		}},
	)
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("ApplyWithOptions error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
	if !strings.Contains(err.Error(), "recovery journal retained") {
		t.Fatalf("ApplyWithOptions error = %v, want retained-journal guidance", err)
	}
	assertHostFileContent(t, movedFixturePath(fixture, movedRoot, fixture.hostPath("CREATE.md")), "created\n")
	assertHostMissing(t, fixture.hostPath("CREATE.md"))
	assertHostMissing(t, filepath.Join(movedRoot, ".daem", "state.json"))
	assertHostMissing(t, fixture.paths.StatefilePath)
	assertActiveRecoveryOperationCount(
		t,
		filepath.Join(movedRoot, ".daem", "recovery"),
		1,
	)
	if !containsApplyEventKind(events, EventStatefileWriteFailed) {
		t.Fatalf("events = %#v, want refused statefile commit", events)
	}
	assertNoEventKind(t, events, EventStatefileWritten)
	assertNoEventKind(t, events, EventJournalCleanupStarted)
}

func TestApplyRetainsJournalWhenProjectRootIsReplacedBeforeCleanup(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	movedRoot := fixture.root + "-moved"
	var events []Event

	result, err := ApplyWithOptions(
		context.Background(),
		fixture.input([]applyEventAction{action}),
		ApplyOptions{Events: func(event Event) {
			events = append(events, event)
			if event.Kind == EventJournalCleanupStarted {
				replaceSelectedRoot(t, fixture.root, movedRoot)
			}
		}},
	)
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("ApplyWithOptions error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
	if !strings.Contains(err.Error(), "retire recovery journal") {
		t.Fatalf("ApplyWithOptions error = %v, want cleanup refusal", err)
	}
	if !strings.Contains(err.Error(), "run: daem recover --dry-run") {
		t.Fatalf("ApplyWithOptions error = %v, want pre-commit recovery guidance", err)
	}
	assertHostFileContent(t, movedFixturePath(fixture, movedRoot, fixture.hostPath("CREATE.md")), "created\n")
	assertHostMissing(t, fixture.hostPath("CREATE.md"))
	if _, loadErr := statefile.Load(
		t.Context(),
		filepath.Join(movedRoot, ".daem", "state.json"),
	); loadErr != nil {
		t.Fatalf("load statefile committed through captured root: %v", loadErr)
	}
	assertCommittedApplyResult(
		t,
		result,
		fixture.paths.StatefilePath,
		filepath.Join(movedRoot, ".daem", "state.json"),
		1,
	)
	assertHostMissing(t, fixture.paths.StatefilePath)
	assertActiveRecoveryOperationCount(
		t,
		filepath.Join(movedRoot, ".daem", "recovery"),
		1,
	)
	if !containsApplyEventKind(events, EventJournalCleanupFailed) {
		t.Fatalf("events = %#v, want rooted journal cleanup refusal", events)
	}
	assertNoEventKind(t, events, EventJournalCleaned)
}

func TestApplyReportsCommittedResultWhenProjectRootIsReplacedAfterStateCommit(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	movedRoot := fixture.root + "-moved"
	var events []Event

	result, err := ApplyWithOptions(
		context.Background(),
		fixture.input([]applyEventAction{action}),
		ApplyOptions{Events: func(event Event) {
			events = append(events, event)
			if event.Kind == EventStatefileWritten {
				replaceSelectedRoot(t, fixture.root, movedRoot)
			}
		}},
	)
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("ApplyWithOptions error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
	if !strings.Contains(err.Error(), "statefile committed") ||
		!strings.Contains(err.Error(), "recovery journal retained") {
		t.Fatalf("ApplyWithOptions error = %v, want exact post-commit outcome", err)
	}
	assertCommittedApplyResult(
		t,
		result,
		fixture.paths.StatefilePath,
		filepath.Join(movedRoot, ".daem", "state.json"),
		1,
	)
	assertActiveRecoveryOperationCount(
		t,
		filepath.Join(movedRoot, ".daem", "recovery"),
		1,
	)
	assertNoEventKind(t, events, EventJournalCleanupStarted)
}

func TestApplyRejectsProjectRootReplacementAfterJournalCleanup(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	movedRoot := fixture.root + "-moved"
	input := fixture.input([]applyEventAction{action})
	filesystem := &rootSwapAfterProjectJournalGCCleanupStore{
		Store: input.Filesystem,
		swap: func() {
			replaceSelectedRoot(t, fixture.root, movedRoot)
		},
	}
	input.Filesystem = filesystem
	var events []Event

	result, err := ApplyWithOptions(
		context.Background(),
		input,
		ApplyOptions{Events: func(event Event) {
			events = append(events, event)
		}},
	)
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("ApplyWithOptions error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
	if !strings.Contains(err.Error(), "recovery journal retired") {
		t.Fatalf("ApplyWithOptions error = %v, want exact cleanup outcome", err)
	}
	if filesystem.swapCount() != 1 {
		t.Fatalf("project-root swaps = %d, want 1", filesystem.swapCount())
	}
	assertHostFileContent(t, movedFixturePath(fixture, movedRoot, fixture.hostPath("CREATE.md")), "created\n")
	assertHostMissing(t, fixture.hostPath("CREATE.md"))
	if _, loadErr := statefile.Load(
		t.Context(),
		filepath.Join(movedRoot, ".daem", "state.json"),
	); loadErr != nil {
		t.Fatalf("load statefile committed before root replacement: %v", loadErr)
	}
	assertCommittedApplyResult(
		t,
		result,
		fixture.paths.StatefilePath,
		filepath.Join(movedRoot, ".daem", "state.json"),
		1,
	)
	assertHostMissing(t, fixture.paths.StatefilePath)
	assertActiveRecoveryOperationCount(
		t,
		filepath.Join(movedRoot, ".daem", "recovery"),
		0,
	)
	if !containsApplyEventKind(events, EventJournalCleaned) {
		t.Fatalf("events = %#v, want rooted journal cleanup success", events)
	}
	assertNoEventKind(t, events, EventJournalCleanupFailed)
}

func TestApplyReportsFinalizedGCFailureWithoutImpossibleRecoveryAdvice(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	input := fixture.input([]applyEventAction{action})
	filesystem := &failProjectJournalGCCleanupStore{Store: input.Filesystem}
	input.Filesystem = filesystem
	var events []Event

	result, err := ApplyWithOptions(
		t.Context(),
		input,
		ApplyOptions{Events: func(event Event) {
			events = append(events, event)
		}},
	)
	const want = "journal retirement committed; hidden GC cleanup did not complete successfully; no recovery action remains"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("ApplyWithOptions error = %v, want finalized GC failure", err)
	}
	if strings.Contains(err.Error(), "daem recover") ||
		strings.Contains(err.Error(), fixture.paths.RecoveryDir) ||
		strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("ApplyWithOptions exposed impossible or private remediation: %v", err)
	}
	if filesystem.attempts != 1 {
		t.Fatalf("GC cleanup attempts = %d, want 1", filesystem.attempts)
	}
	assertCommittedApplyResult(
		t,
		result,
		fixture.paths.StatefilePath,
		fixture.paths.StatefilePath,
		1,
	)
	assertHostFileContent(t, fixture.hostPath("CREATE.md"), "created\n")
	if !containsApplyEventKind(events, EventJournalCleanupFailed) {
		t.Fatalf("events = %#v, want journal cleanup failure", events)
	}
	assertNoEventKind(t, events, EventJournalCleaned)

	_, loadErr := journal.LoadRecoverablePlanWithOptions(
		t.Context(),
		journal.Paths{RecoveryDir: fixture.paths.RecoveryDir},
		journal.PlanLoadOptions{
			Filesystem: filesystem,
			StateCodec: statefile.Codec{},
		},
	)
	if !errors.Is(loadErr, journal.ErrNoRecoverableJournal) {
		t.Fatalf("LoadRecoverablePlanWithOptions error = %v, want no recoverable journal", loadErr)
	}
	if readinessErr := journal.RequireNoInterruptedApply(
		t.Context(),
		fixture.paths.RecoveryDir,
	); readinessErr != nil {
		t.Fatalf("finalized GC residue blocked later work: %v", readinessErr)
	}
}

func TestApplyReportsPostRetirementAcceptanceFailureWithoutRecoveryAdvice(t *testing.T) {
	fixture := newApplyEventFixture(t)
	action := fixture.createAction("create", "CREATE.md", "created\n")
	input := fixture.input([]applyEventAction{action})
	acceptanceFailure := errors.New("injected post-retirement acceptance failure")
	cleanupStarted := false
	var events []Event

	result, err := ApplyWithOptions(
		t.Context(),
		input,
		ApplyOptions{
			Events: func(event Event) {
				events = append(events, event)
				if event.Kind == EventJournalCleanupStarted {
					cleanupStarted = true
				}
			},
			AcceptVisibilityChanges: func(context.Context) error {
				if cleanupStarted {
					return acceptanceFailure
				}
				return nil
			},
		},
	)
	if !errors.Is(err, acceptanceFailure) ||
		!strings.Contains(err.Error(), "recovery journal retired; no recovery action remains") {
		t.Fatalf("ApplyWithOptions error = %v, want exact post-retirement outcome", err)
	}
	if strings.Contains(err.Error(), "daem recover") {
		t.Fatalf("ApplyWithOptions exposed impossible recovery advice: %v", err)
	}
	assertCommittedApplyResult(
		t,
		result,
		fixture.paths.StatefilePath,
		fixture.paths.StatefilePath,
		1,
	)
	assertHostFileContent(t, fixture.hostPath("CREATE.md"), "created\n")
	if !containsApplyEventKind(events, EventJournalCleanupFailed) {
		t.Fatalf("events = %#v, want post-retirement acceptance failure", events)
	}
	assertNoEventKind(t, events, EventJournalCleaned)

	_, loadErr := journal.LoadRecoverablePlanWithOptions(
		t.Context(),
		journal.Paths{RecoveryDir: fixture.paths.RecoveryDir},
		journal.PlanLoadOptions{
			Filesystem: input.Filesystem,
			StateCodec: statefile.Codec{},
		},
	)
	if !errors.Is(loadErr, journal.ErrNoRecoverableJournal) {
		t.Fatalf("LoadRecoverablePlanWithOptions error = %v, want no recoverable journal", loadErr)
	}
}
