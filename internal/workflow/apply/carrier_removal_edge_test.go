package apply

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

type rejectingReplaceStore struct {
	storagecommit.Adapter
	err error
}

func (store rejectingReplaceStore) ReplaceRootedFile(
	_ context.Context,
	capability rootedpath.CommitCapability,
	_ []byte,
	_ fs.FileMode,
	_ mutationfs.EntryIdentity,
) error {
	return errors.Join(store.err, capability.Close())
}

type failNthReplaceStore struct {
	storagecommit.Adapter
	err     error
	failOn  int
	replace int
}

func (store *failNthReplaceStore) ReplaceRootedFile(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	content []byte,
	mode fs.FileMode,
	expected mutationfs.EntryIdentity,
) error {
	store.replace++
	if store.replace == store.failOn {
		return errors.Join(store.err, capability.Close())
	}
	return store.Adapter.ReplaceRootedFile(ctx, capability, content, mode, expected)
}

type cancelAfterNthReplaceStore struct {
	storagecommit.Adapter
	cancel      context.CancelFunc
	cancelAfter int
	replace     int
}

func (store *cancelAfterNthReplaceStore) ReplaceRootedFile(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	content []byte,
	mode fs.FileMode,
	expected mutationfs.EntryIdentity,
) error {
	store.replace++
	if err := store.Adapter.ReplaceRootedFile(
		ctx,
		capability,
		content,
		mode,
		expected,
	); err != nil {
		return err
	}
	if store.replace == store.cancelAfter {
		store.cancel()
	}
	return nil
}

func TestRunNeverInvokesHostWhenPendingBoundaryCommitFails(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	input := fixture.input(t)
	input.Filesystem = rejectingReplaceStore{err: errors.New("injected E4 failure")}

	result, err := runCarrierRemovals(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "injected E4 failure") {
		t.Fatalf("error = %v, want E4 failure", err)
	}
	if fixture.executorCalls != 0 {
		t.Fatalf("executor calls = %d, want 0", fixture.executorCalls)
	}
	if !result.State.Equal(fixture.current) {
		t.Fatal("failed E4 commit changed returned state")
	}
	if !fixture.persistedState(t).Equal(fixture.current) {
		t.Fatal("failed E4 commit changed durable state")
	}
}

func TestRunRetainsClaimAndPendingWhenAttemptHistoryCommitFails(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	input := fixture.input(t)
	store := &failNthReplaceStore{
		err:    errors.New("injected attempt-history failure"),
		failOn: 2,
	}
	input.Filesystem = store

	result, err := runCarrierRemovals(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "injected attempt-history failure") {
		t.Fatalf("error = %v, want attempt-history failure", err)
	}
	if fixture.executorCalls != 1 {
		t.Fatalf("executor calls = %d, want 1", fixture.executorCalls)
	}
	assertRetainedRemoval(t, result.State, fixture.claim)
	persisted := fixture.persistedState(t)
	assertRetainedRemoval(t, persisted, fixture.claim)
	if len(result.Attempts) != 0 || len(persisted.HostRouteAttempts()) != 0 {
		t.Fatal("failed attempt-history commit exposed an uncommitted attempt")
	}
}

func TestRunRetainsClaimAndPendingWhenRetirementCommitFails(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	input := fixture.input(t)
	store := &failNthReplaceStore{
		err:    errors.New("injected claim-retirement failure"),
		failOn: 3,
	}
	input.Filesystem = store

	result, err := runCarrierRemovals(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "injected claim-retirement failure") {
		t.Fatalf("error = %v, want claim-retirement failure", err)
	}
	if fixture.executorCalls != 1 {
		t.Fatalf("executor calls = %d, want 1", fixture.executorCalls)
	}
	assertRetainedRemoval(t, result.State, fixture.claim)
	persisted := fixture.persistedState(t)
	assertRetainedRemoval(t, persisted, fixture.claim)
	if len(result.Attempts) != 1 || len(persisted.HostRouteAttempts()) != 1 {
		t.Fatal("retirement failure lost the durably committed attempt history")
	}
	if !result.Attempts[0].Equal(persisted.HostRouteAttempts()[0]) {
		t.Fatal("returned and durable attempt history diverged after retirement failure")
	}
}

func TestRunRetiresClaimAfterTimeoutWhenFreshPostconditionsAreSatisfied(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	fixture.runnerResult = subprocess.CommandResult{
		Started:  true,
		TimedOut: true,
	}

	result, err := runCarrierRemovals(context.Background(), fixture.input(t))
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertConvergedProjectRemoval(t, result.State, fixture.claim)
	if fixture.executorCalls != 1 {
		t.Fatalf("executor calls = %d, want 1", fixture.executorCalls)
	}
	attempt := result.Attempts[0]
	if !attempt.TimedOut() ||
		attempt.ResultClass() != durableattempt.HostRouteResultFailed ||
		attempt.Reason() != durableattempt.HostRouteReasonTimeout {
		t.Fatalf(
			"attempt timeout/class/reason = %t/%q/%q",
			attempt.TimedOut(),
			attempt.ResultClass(),
			attempt.Reason(),
		)
	}
	assertConvergedProjectRemoval(t, fixture.persistedState(t), fixture.claim)
}

func TestRunRetainsClaimWhenPostObservationIsUnavailable(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	input := fixture.input(t)
	input.Observer = nil

	result, err := runCarrierRemovals(context.Background(), input)
	if err == nil {
		t.Fatal("Run returned nil error")
	}
	assertRetainedRemoval(t, result.State, fixture.claim)
	attempt := result.Attempts[0]
	if attempt.ResultClass() != durableattempt.HostRouteResultAttemptedUnverified ||
		attempt.Reason() != durableattempt.HostRouteReasonObservationUnavailable ||
		attempt.ObservationSummary() != observerelation.ObservationNotObserved {
		t.Fatalf(
			"attempt class/reason/observation = %q/%q/%q",
			attempt.ResultClass(),
			attempt.Reason(),
			attempt.ObservationSummary(),
		)
	}
}

func TestRunRetainsPendingBoundaryWhenContextCancelsDuringHostAttempt(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	ctx, cancel := context.WithCancel(context.Background())
	input := fixture.input(t)
	input.Executor = subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			fixture.executorCalls++
			cancel()
			return subprocess.CommandResult{
				Started:  true,
				Canceled: true,
			}
		},
	})

	result, err := runCarrierRemovals(ctx, input)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if fixture.executorCalls != 1 {
		t.Fatalf("executor calls = %d, want 1", fixture.executorCalls)
	}
	assertRetainedRemoval(t, result.State, fixture.claim)
	assertRetainedRemoval(t, fixture.persistedState(t), fixture.claim)
}

func TestRunRecordsAdapterPreflightFailureWithoutHostEffect(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	input := fixture.input(t)
	input.Adapter = func(executehostroute.RemovalRequest) (
		subprocess.CommandAttemptRequest,
		error,
	) {
		return subprocess.CommandAttemptRequest{}, errors.New("unsupported fake route")
	}

	result, err := runCarrierRemovals(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "unsupported fake route") {
		t.Fatalf("error = %v, want adapter preflight failure", err)
	}
	assertCarrierRemovalHostRouteFailure(t, err)
	if fixture.executorCalls != 0 {
		t.Fatalf("executor calls = %d, want 0", fixture.executorCalls)
	}
	if len(result.Attempts) != 1 ||
		result.Attempts[0].Operation() != "remove" ||
		result.Attempts[0].ResultClass() != durableattempt.HostRouteResultBlockedPreflight {
		t.Fatalf("attempts = %#v, want one remove preflight record", result.Attempts)
	}
	if len(result.State.PendingCarrierRemovals()) != 0 {
		t.Fatal("adapter preflight failure created an E4 boundary")
	}
}

func TestRunPersistsOnlyBoundedRedactionFactsForHostileOutput(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	const secret = "credential-that-must-not-reach-state"
	input := fixture.input(t)
	input.Adapter = func(request executehostroute.RemovalRequest) (
		subprocess.CommandAttemptRequest,
		error,
	) {
		return subprocess.CommandAttemptRequest{
			Command: "fake-host",
			Args:    []string{"remove"},
			EnvRefs: []subprocess.CommandEnvRef{{
				Name:       "TOKEN",
				SourceName: "DAEM_TEST_TOKEN",
			}},
			WorkDir: request.WorkDir(),
		}, nil
	}
	input.Executor = subprocess.NewCommandExecutor(subprocess.CommandOptions{
		LookupEnv: func(name string) (string, bool) {
			if name == "DAEM_TEST_TOKEN" {
				return secret, true
			}
			return "", false
		},
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			fixture.executorCalls++
			return subprocess.CommandResult{
				Started:     true,
				HasExitCode: true,
				Stdout:      "stdout:" + secret,
				Stderr:      "stderr:" + secret,
			}
		},
	})

	result, err := runCarrierRemovals(context.Background(), input)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.Attempts) != 1 || !result.Attempts[0].Redacted() {
		t.Fatalf("attempts = %#v, want one redacted record", result.Attempts)
	}
	content, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), secret) {
		t.Fatal("durable state contains hostile command output or secret material")
	}
}

func TestRunStopsAfterFirstRemovalFailure(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	fixture.runnerResult = subprocess.CommandResult{
		Started:  true,
		TimedOut: true,
	}
	input := fixture.input(t)
	input.Actions = append(input.Actions, fixture.action)

	result, err := runCarrierRemovals(context.Background(), input)
	if err == nil {
		t.Fatal("Run returned nil error")
	}
	if fixture.executorCalls != 1 {
		t.Fatalf("executor calls = %d, want fail-fast count 1", fixture.executorCalls)
	}
	if len(result.Attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(result.Attempts))
	}
}

func TestRunRejectsInexactGlobalRegistrySuccessor(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeGlobal)
	other := newWorkflowFixture(t, target.ScopeGlobal)
	input := fixture.input(t)
	input.RemoveGlobalClaim = func(
		context.Context,
		durablecarrier.GlobalCarrierClaims,
		durablecarrier.ManagedCarrierClaim,
	) (durablecarrier.GlobalCarrierClaims, error) {
		return other.globalClaims, nil
	}

	result, err := runCarrierRemovals(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "inexact successor") {
		t.Fatalf("error = %v, want inexact successor rejection", err)
	}
	if fixture.executorCalls != 1 {
		t.Fatalf("executor calls = %d, want 1", fixture.executorCalls)
	}
	if !result.GlobalClaims.Equal(fixture.globalClaims) {
		t.Fatal("inexact registry successor replaced the retained registry")
	}
	if len(result.State.PendingCarrierRemovals()) != 0 {
		t.Fatal("verified global absence retained a stale local pending boundary")
	}
}
