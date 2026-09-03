package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observepostcondition "github.com/isty2e/daem/internal/assurance/observe/postcondition"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	assurancepostcondition "github.com/isty2e/daem/internal/assurance/postcondition"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

func TestRunRetiresProjectClaimOnlyAfterVerifiedAbsence(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	result, err := runCarrierRemovals(context.Background(), fixture.input(t))
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if fixture.executorCalls != 1 {
		t.Fatalf("executor calls = %d, want 1", fixture.executorCalls)
	}
	if result.ActionCount != 1 {
		t.Fatalf("ActionCount = %d, want 1", result.ActionCount)
	}
	assertConvergedProjectRemoval(t, result.State, fixture.claim)
	if len(result.Attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(result.Attempts))
	}
	attempt := result.Attempts[0]
	if attempt.Operation() != "remove" ||
		attempt.ResultClass() != durableattempt.HostRouteResultAttemptedObservedAbsent ||
		attempt.PostconditionSummary() != observerelation.PostconditionObserved {
		t.Fatalf(
			"attempt = operation:%q class:%q postcondition:%q",
			attempt.Operation(),
			attempt.ResultClass(),
			attempt.PostconditionSummary(),
		)
	}
	assertConvergedProjectRemoval(t, fixture.persistedState(t), fixture.claim)
}

func TestRunScheduledCarrierRemovalsJoinsOwnedStatefileAuthorityCloseFailure(
	t *testing.T,
) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	input := fixture.input(t)
	injected := errors.New("injected statefile authority close failure")
	input.CloseStatefileAuthority = func(authority *statefileEffectAuthority) error {
		return errors.Join(authority.Close(), injected)
	}
	plan := scheduledCarrierRemovalTestPlan(t, input.Actions)

	result, err := runScheduledCarrierRemovals(
		t.Context(),
		input,
		plan,
		plan,
	)
	if !errors.Is(err, injected) {
		t.Fatalf("runScheduledCarrierRemovals error = %v, want close failure", err)
	}
	if strings.Contains(err.Error(), "effect structure") {
		t.Fatalf("close failure exposed cursor settlement error: %v", err)
	}
	if result.ActionCount != 1 {
		t.Fatalf("ActionCount = %d, want 1; error = %v", result.ActionCount, err)
	}
	assertConvergedProjectRemoval(t, result.State, fixture.claim)
}

func TestRunScheduledCarrierRemovalsRejectsInputSemanticMismatchBeforeAuthority(
	t *testing.T,
) {
	inputFixture := newWorkflowFixture(t, target.ScopeProject)
	planFixture := newWorkflowFixture(t, target.ScopeProject)
	input := inputFixture.input(t)
	plan := scheduledCarrierRemovalTestPlan(t, planFixture.input(t).Actions)
	reserve := input.ReserveStatefileAuthority
	reserveCalls := 0
	input.ReserveStatefileAuthority = func(
		path string,
		plan statefileEffectPlan,
	) (statefileEffectReservation, error) {
		reserveCalls++
		return reserve(path, plan)
	}

	result, err := runScheduledCarrierRemovals(
		t.Context(),
		input,
		plan,
		plan,
	)
	if err == nil {
		t.Fatal("semantic plan mismatch returned nil error")
	}
	if reserveCalls != 0 {
		t.Fatalf("statefile authority reservations = %d, want 0", reserveCalls)
	}
	if inputFixture.executorCalls != 0 || result.ActionCount != 0 {
		t.Fatalf(
			"mismatched plan executed host work: calls=%d actions=%d",
			inputFixture.executorCalls,
			result.ActionCount,
		)
	}
	if pending := inputFixture.persistedState(t).PendingCarrierRemovals(); len(pending) != 0 {
		t.Fatalf("mismatched plan persisted pending removals: %#v", pending)
	}
}

func TestRunScheduledCarrierRemovalsRejectsUnavailableProjectRootBeforeBinding(
	t *testing.T,
) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	input := fixture.input(t)
	input.ProjectRoot = nil
	plan := scheduledCarrierRemovalTestPlan(t, input.Actions)

	result, err := runScheduledCarrierRemovals(t.Context(), input, plan, plan)
	var failure *rootedpath.Failure
	if !errors.As(err, &failure) || failure.Kind() != rootedpath.FailureRootUnavailable {
		t.Fatalf("runScheduledCarrierRemovals error = %v, want unavailable project root", err)
	}
	if fixture.executorCalls != 0 || result.ActionCount != 0 {
		t.Fatalf(
			"unavailable project root executed removal: calls=%d actions=%d",
			fixture.executorCalls,
			result.ActionCount,
		)
	}
}

func TestScheduledCarrierRemovalCallReturnsCursorMismatchWithoutCleanup(
	t *testing.T,
) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	plan := scheduledCarrierRemovalTestPlan(t, fixture.input(t).Actions).carrierRemovalPlan()
	execution, err := newApplyContinuationExecution(plan, plan)
	if err != nil {
		t.Fatal(err)
	}

	err = scheduledCarrierRemovalCallWithFailureCleanup(
		execution,
		"apply/carrier-removal/not-scheduled",
		operationplan.EffectStepObservation,
		func() error { return nil },
		nil,
	)
	if err == nil {
		t.Fatal("cursor mismatch returned nil error")
	}
}

func TestRunScheduledCarrierRemovalsRejectsUnderConsumedPlan(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	input := fixture.input(t)
	plan := scheduledCarrierRemovalTestPlan(t, input.Actions)
	facts, err := applyCarrierScheduleFacts(input.Actions)
	if err != nil {
		t.Fatal(err)
	}
	var builder operationplan.EffectStructureBuilder
	statefile := &applyStatefileSchedule{builder: &builder}
	segment := compileApplyCarrierRemovalSchedules(&builder, statefile, facts)
	structure, err := builder.Compile(builder.ForwardPhase(
		"apply/carrier-removals",
		operationplan.EffectSequence(
			segment,
			builder.Step("apply/carrier-removals/unconsumed", operationplan.EffectStepNoOp),
		),
	))
	if err != nil {
		t.Fatal(err)
	}
	plan.carrierRemovalStructure = structure

	result, err := runScheduledCarrierRemovals(t.Context(), input, plan, plan)
	if err == nil {
		t.Fatal("under-consumed carrier-removal plan returned nil error")
	}
	if result.ActionCount != 1 || fixture.executorCalls != 1 {
		t.Fatalf(
			"under-consumed result = actions %d calls %d, want completed action before cursor refusal",
			result.ActionCount,
			fixture.executorCalls,
		)
	}
}

func scheduledCarrierRemovalTestPlan(
	t *testing.T,
	actions []carrierabsence.Action,
) applyContinuationPlan {
	t.Helper()
	facts, err := applyCarrierScheduleFacts(actions)
	if err != nil {
		t.Fatal(err)
	}
	input := syntheticApplyScheduleInput(t, 0)
	input.carrierRemovals = facts
	return mustCompileSyntheticApplySchedule(t, input).continuation
}

func TestRunPersistsCarrierRemovalWithControlBearingStateDir(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("control-bearing StateDir semantics are supported on Darwin and Linux")
	}
	root := t.TempDir()
	fixture := newWorkflowFixtureAtPaths(
		t,
		root,
		filepath.Join(root, "state\ncontrol", "state.json"),
		target.ScopeProject,
		effectpostcondition.Set{},
		observepostcondition.EvidenceState(""),
	)
	result, err := runCarrierRemovals(t.Context(), fixture.input(t))
	if err != nil {
		t.Fatalf("control-bearing carrier removal: %v", err)
	}
	assertConvergedProjectRemoval(t, result.State, fixture.claim)
	assertConvergedProjectRemoval(t, fixture.persistedState(t), fixture.claim)
}

func TestRunClassifiesPostAttemptProjectRootReplacementFromDurableRecord(
	t *testing.T,
) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	fixture.runnerResult = subprocess.CommandResult{Started: true, TimedOut: true}
	input := fixture.input(t)
	originalObserver := input.Observer
	movedRoot := fixture.root + "-moved"
	t.Cleanup(func() {
		if err := os.RemoveAll(movedRoot); err != nil {
			t.Errorf("remove moved project root: %v", err)
		}
	})
	input.Observer = func(
		ctx context.Context,
		pending durablecarrier.PendingCarrierRemoval,
		claims []durablecarrier.ManagedCarrierClaim,
	) assurancehostroute.ObservationFact {
		observation := originalObserver(ctx, pending, claims)
		if err := os.Rename(fixture.root, movedRoot); err != nil {
			t.Fatalf("move retained project root: %v", err)
		}
		if err := os.MkdirAll(fixture.root, 0o700); err != nil {
			t.Fatalf("create replacement project root: %v", err)
		}
		return observation
	}

	result, err := runCarrierRemovals(context.Background(), input)
	if err == nil {
		t.Fatal("Run returned nil error after project-root replacement")
	}
	var routeFailure hostRouteExecutionError
	if !errors.As(err, &routeFailure) {
		t.Fatalf("error = %v, want hostRouteExecutionError", err)
	}
	failure := ClassifyFailure(err, CommandResult{ExecutionAttempted: true})
	if failure.Reason() != FailureReasonFileSetAccessUnprovable ||
		failure.Phase() != FailurePhaseExecution ||
		failure.Outcome() != FailureOutcomeIncomplete {
		t.Fatalf(
			"failure = (%q, %q, %q), want (%q, %q, %q)",
			failure.Reason(),
			failure.Phase(),
			failure.Outcome(),
			FailureReasonFileSetAccessUnprovable,
			FailurePhaseExecution,
			FailureOutcomeIncomplete,
		)
	}
	assertRetainedRemoval(t, result.State, fixture.claim)
	if len(result.Attempts) != 0 {
		t.Fatalf("attempts = %d, want no falsely persisted attempt", len(result.Attempts))
	}
	if len(routeFailure.records) != 1 {
		t.Fatalf("host route failure = %#v, want one final attempt record", routeFailure)
	}
	attempt := routeFailure.records[0]
	if attempt.ResultClass() != durableattempt.HostRouteResultFailed ||
		attempt.Reason() != durableattempt.HostRouteReasonWorkDirAuthority ||
		attempt.AttemptReason() != durableattempt.HostRouteAttemptReasonTimeout ||
		!attempt.TimedOut() {
		t.Fatalf(
			"attempt class/reason/attempt_reason = %q/%q/%q",
			attempt.ResultClass(),
			attempt.Reason(),
			attempt.AttemptReason(),
		)
	}
}

func TestRunRetainsClaimAndPendingWhenRelationRemainsPresent(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	fixture.postObservation = exactCorrelation(t, fixture.expected)
	result, err := runCarrierRemovals(context.Background(), fixture.input(t))
	if err == nil {
		t.Fatal("Run returned nil error")
	}
	assertCarrierRemovalHostRouteFailure(t, err)
	if fixture.executorCalls != 1 {
		t.Fatalf("executor calls = %d, want 1", fixture.executorCalls)
	}
	assertRetainedRemoval(t, result.State, fixture.claim)
	attempt := result.Attempts[0]
	if attempt.ResultClass() != durableattempt.HostRouteResultAttemptedObservedPresent ||
		attempt.PostconditionSummary() != observerelation.PostconditionFailed {
		t.Fatalf(
			"attempt class/postcondition = %q/%q",
			attempt.ResultClass(),
			attempt.PostconditionSummary(),
		)
	}
	assertRetainedRemoval(t, fixture.persistedState(t), fixture.claim)
}

func TestRunRetiresRelationOnlyClaimWhenFailedCommandIsFollowedByAbsence(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	fixture.runnerResult = subprocess.CommandResult{
		Started:     true,
		HasExitCode: true,
		ExitCode:    9,
	}
	result, err := runCarrierRemovals(context.Background(), fixture.input(t))
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertConvergedProjectRemoval(t, result.State, fixture.claim)
	if len(result.Attempts) != 1 ||
		result.Attempts[0].ResultClass() != durableattempt.HostRouteResultFailed ||
		result.Attempts[0].Reason() != durableattempt.HostRouteReasonNonZeroExit {
		t.Fatalf("attempts = %#v", result.Attempts)
	}
}

func TestRunRetiresCoupledClaimWhenFailedCommandButAllPostconditionsSatisfied(t *testing.T) {
	fixture := newCoupledWorkflowFixture(t, target.ScopeProject)
	fixture.runnerResult = subprocess.CommandResult{
		Started:     true,
		HasExitCode: true,
		ExitCode:    9,
	}
	input := fixture.input(t)
	plan := scheduledCarrierRemovalTestPlan(t, input.Actions)

	result, err := runScheduledCarrierRemovals(t.Context(), input, plan, plan)
	if err != nil {
		t.Fatalf("runScheduledCarrierRemovals returned error: %v", err)
	}
	assertConvergedProjectRemoval(t, result.State, fixture.claim)
	attempt := result.Attempts[0]
	if attempt.ResultClass() != durableattempt.HostRouteResultFailed ||
		attempt.Reason() != durableattempt.HostRouteReasonNonZeroExit {
		t.Fatalf(
			"attempt class/reason = %q/%q",
			attempt.ResultClass(),
			attempt.Reason(),
		)
	}
	summaries := attempt.EffectPostconditions().Summaries()
	if len(summaries) != 1 ||
		summaries[0].State() != assurancepostcondition.SummarySatisfied {
		t.Fatalf("effect postconditions = %#v", summaries)
	}
}

func TestRunRetainsCoupledClaimWhenArtifactPostconditionIsUnsatisfied(t *testing.T) {
	fixture := newCoupledWorkflowFixture(t, target.ScopeProject)
	fixture.effectEvidence = observepostcondition.EvidenceUnsatisfied
	input := fixture.input(t)
	plan := scheduledCarrierRemovalTestPlan(t, input.Actions)

	result, err := runScheduledCarrierRemovals(t.Context(), input, plan, plan)
	if err == nil {
		t.Fatal("runScheduledCarrierRemovals returned nil error")
	}
	assertRetainedRemoval(t, result.State, fixture.claim)
	attempt := result.Attempts[0]
	if attempt.ResultClass() != durableattempt.HostRouteResultAttemptedUnverified ||
		attempt.Reason() != durableattempt.HostRouteReasonEffectUnsatisfied {
		t.Fatalf(
			"attempt class/reason = %q/%q",
			attempt.ResultClass(),
			attempt.Reason(),
		)
	}
	summaries := attempt.EffectPostconditions().Summaries()
	if len(summaries) != 1 ||
		summaries[0].State() != assurancepostcondition.SummaryUnsatisfied {
		t.Fatalf("effect postconditions = %#v", summaries)
	}
	assertRetainedRemoval(t, fixture.persistedState(t), fixture.claim)
}

func TestRunRetainsCoupledClaimWhenArtifactEvidenceIsMissing(t *testing.T) {
	fixture := newCoupledWorkflowFixture(t, target.ScopeProject)
	fixture.effectEvidence = ""

	result, err := runCarrierRemovals(context.Background(), fixture.input(t))
	if err == nil {
		t.Fatal("Run returned nil error")
	}
	assertRetainedRemoval(t, result.State, fixture.claim)
	attempt := result.Attempts[0]
	if attempt.ResultClass() != durableattempt.HostRouteResultAttemptedUnverified ||
		attempt.Reason() != durableattempt.HostRouteReasonEffectMissing {
		t.Fatalf(
			"attempt class/reason = %q/%q",
			attempt.ResultClass(),
			attempt.Reason(),
		)
	}
	summaries := attempt.EffectPostconditions().Summaries()
	if len(summaries) != 1 ||
		summaries[0].State() != assurancepostcondition.SummaryNotObserved {
		t.Fatalf("effect postconditions = %#v", summaries)
	}
}

func TestRunRetainsClaimWhenPostObservationIsStale(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	fixture.postObservation = staleCorrelation(t, fixture.expected)
	result, err := runCarrierRemovals(context.Background(), fixture.input(t))
	if err == nil {
		t.Fatal("Run returned nil error")
	}
	assertRetainedRemoval(t, result.State, fixture.claim)
	if result.Attempts[0].ResultClass() != durableattempt.HostRouteResultAttemptedUnverified ||
		result.Attempts[0].Reason() != durableattempt.HostRouteReasonObservationStale {
		t.Fatalf(
			"attempt class/reason = %q/%q",
			result.Attempts[0].ResultClass(),
			result.Attempts[0].Reason(),
		)
	}
}

func TestRunRetiresGlobalClaimAfterClearingLocalPendingBoundary(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeGlobal)
	result, err := runCarrierRemovals(context.Background(), fixture.input(t))
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.GlobalClaims.Claims()) != 0 {
		t.Fatalf("global claims = %#v, want empty", result.GlobalClaims.Claims())
	}
	if len(result.State.PendingCarrierRemovals()) != 0 {
		t.Fatalf("pending removals = %#v, want empty", result.State.PendingCarrierRemovals())
	}
	if len(result.State.HostRouteAttempts()) != 1 {
		t.Fatalf("state attempts = %d, want 1", len(result.State.HostRouteAttempts()))
	}
}

func TestRunSettlesPendingRemovalFromFreshCurrentEvidenceWithoutReinvocation(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newCoupledWorkflowFixture(t, scope)
			fixture.preparePendingSettlement(t)
			input := fixture.input(t)
			plan := scheduledCarrierRemovalTestPlan(t, input.Actions)

			result, err := runScheduledCarrierRemovals(t.Context(), input, plan, plan)
			if err != nil {
				t.Fatalf("runScheduledCarrierRemovals returned error: %v", err)
			}
			if fixture.executorCalls != 0 {
				t.Fatalf("executor calls = %d, want no host reinvocation", fixture.executorCalls)
			}
			if len(result.Attempts) != 0 || len(result.State.HostRouteAttempts()) != 0 {
				t.Fatalf("observation-only settlement created attempt history: %#v", result.Attempts)
			}
			if len(result.State.PendingCarrierRemovals()) != 0 {
				t.Fatalf("pending removals = %#v, want empty", result.State.PendingCarrierRemovals())
			}
			switch scope {
			case target.ScopeProject:
				assertConvergedProjectRemoval(t, result.State, fixture.claim)
			case target.ScopeGlobal:
				if len(result.GlobalClaims.Claims()) != 0 {
					t.Fatalf("global claims = %#v, want empty", result.GlobalClaims.Claims())
				}
			}
		})
	}
}

func TestRunRetainsPendingRemovalWhenCurrentEffectsAreNotSatisfied(t *testing.T) {
	fixture := newCoupledWorkflowFixture(t, target.ScopeProject)
	fixture.preparePendingSettlement(t)
	fixture.effectEvidence = observepostcondition.EvidenceUnsatisfied

	result, err := runCarrierRemovals(context.Background(), fixture.input(t))
	if err == nil {
		t.Fatal("Run returned nil error")
	}
	if fixture.executorCalls != 0 {
		t.Fatalf("executor calls = %d, want no host reinvocation", fixture.executorCalls)
	}
	if len(result.Attempts) != 0 {
		t.Fatalf("settlement attempts = %#v, want none", result.Attempts)
	}
	assertRetainedRemoval(t, result.State, fixture.claim)
}

func TestRunRetainsPendingRemovalWithoutCurrentObserver(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	fixture.preparePendingSettlement(t)
	input := fixture.input(t)
	input.Observer = nil

	result, err := runCarrierRemovals(context.Background(), input)
	if err == nil {
		t.Fatal("Run returned nil error")
	}
	if fixture.executorCalls != 0 {
		t.Fatalf("executor calls = %d, want no host reinvocation", fixture.executorCalls)
	}
	assertRetainedRemoval(t, result.State, fixture.claim)
}

func TestRunGlobalRegistryFailureNeverRestoresOrReinvokesHost(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeGlobal)
	input := fixture.input(t)
	input.RemoveGlobalClaim = func(
		context.Context,
		durablecarrier.GlobalCarrierClaims,
		durablecarrier.ManagedCarrierClaim,
	) (durablecarrier.GlobalCarrierClaims, error) {
		return durablecarrier.GlobalCarrierClaims{}, errors.New("registry unavailable")
	}
	result, err := runCarrierRemovals(context.Background(), input)
	if err == nil {
		t.Fatal("Run returned nil error")
	}
	if fixture.executorCalls != 1 {
		t.Fatalf("executor calls = %d, want 1", fixture.executorCalls)
	}
	if len(result.GlobalClaims.Claims()) != 1 {
		t.Fatalf("global claims = %#v, want retained exact claim", result.GlobalClaims.Claims())
	}
	if len(result.State.PendingCarrierRemovals()) != 0 {
		t.Fatalf("pending removals = %#v, want cleared after verified absence", result.State.PendingCarrierRemovals())
	}
}

func TestRunGlobalRegistryIndeterminateFailurePreservesPossibleSuccessor(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeGlobal)
	input := fixture.input(t)
	input.RemoveGlobalClaim = func(
		context.Context,
		durablecarrier.GlobalCarrierClaims,
		durablecarrier.ManagedCarrierClaim,
	) (durablecarrier.GlobalCarrierClaims, error) {
		return durablecarrier.GlobalCarrierClaims{}, globalCarrierSettlementClassifiedError{
			kind: mutationfs.FailureIndeterminateCommit,
		}
	}
	plan := scheduledCarrierRemovalTestPlan(t, input.Actions)

	result, err := runScheduledCarrierRemovals(t.Context(), input, plan, plan)
	if err == nil {
		t.Fatal("runScheduledCarrierRemovals returned nil error")
	}
	if fixture.executorCalls != 1 {
		t.Fatalf("executor calls = %d, want 1", fixture.executorCalls)
	}
	if claims := result.GlobalClaims.Claims(); len(claims) != 0 {
		t.Fatalf("global claims = %#v, want possible committed successor", claims)
	}
	if pending := result.State.PendingCarrierRemovals(); len(pending) != 0 {
		t.Fatalf("pending removals = %#v, want cleared after verified absence", pending)
	}
}

func TestRunCancellationBeforeEntryCausesNoHostEffect(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := runCarrierRemovals(ctx, fixture.input(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if fixture.executorCalls != 0 {
		t.Fatalf("executor calls = %d, want 0", fixture.executorCalls)
	}
	if !result.State.Equal(fixture.current) {
		t.Fatal("pre-entry cancellation changed returned state")
	}
}

func TestRunRetriesRetainedPendingRemovalOnlyAfterFreshReentry(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	fixture.postObservation = exactCorrelation(t, fixture.expected)
	first, err := runCarrierRemovals(context.Background(), fixture.input(t))
	if err == nil {
		t.Fatal("first Run returned nil error")
	}
	if len(first.State.PendingCarrierRemovals()) != 1 {
		t.Fatal("first Run did not retain E4 boundary")
	}

	fixture.current = first.State
	fixture.postObservation = missingCorrelation(t, fixture.expected)
	second, err := runCarrierRemovals(context.Background(), fixture.input(t))
	if err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}
	if fixture.executorCalls != 2 {
		t.Fatalf("total executor calls = %d, want 2", fixture.executorCalls)
	}
	assertConvergedProjectRemoval(t, second.State, fixture.claim)
}

func assertConvergedProjectRemoval(
	t *testing.T,
	snapshot durable.Snapshot,
	claim durablecarrier.ManagedCarrierClaim,
) {
	t.Helper()
	for _, candidate := range snapshot.ManagedCarrierClaims() {
		if candidate.ExactEqual(claim) {
			t.Fatal("exact project claim was retained")
		}
	}
	if len(snapshot.PendingCarrierRemovals()) != 0 {
		t.Fatalf("pending removals = %#v, want empty", snapshot.PendingCarrierRemovals())
	}
}

func assertRetainedRemoval(
	t *testing.T,
	snapshot durable.Snapshot,
	claim durablecarrier.ManagedCarrierClaim,
) {
	t.Helper()
	claimRetained := false
	for _, candidate := range snapshot.ManagedCarrierClaims() {
		if candidate.ExactEqual(claim) {
			claimRetained = true
			break
		}
	}
	if !claimRetained {
		t.Fatal("exact project claim was retired without convergence")
	}
	pending := snapshot.PendingCarrierRemovals()
	if len(pending) != 1 || !pending[0].Claim().ExactEqual(claim) {
		t.Fatalf("pending removals = %#v, want exact retained boundary", pending)
	}
}

func assertCarrierRemovalHostRouteFailure(t *testing.T, err error) {
	t.Helper()
	var routeFailure hostRouteExecutionError
	if !errors.As(err, &routeFailure) {
		t.Fatalf("error = %v, want hostRouteExecutionError", err)
	}
	failure := ClassifyFailure(err, CommandResult{ExecutionAttempted: true})
	if failure.Reason() != FailureReasonHostRouteAttemptFailed ||
		failure.Phase() != FailurePhaseExecution ||
		failure.Outcome() != FailureOutcomeIncomplete {
		t.Fatalf(
			"failure = (%q, %q, %q), want (%q, %q, %q)",
			failure.Reason(),
			failure.Phase(),
			failure.Outcome(),
			FailureReasonHostRouteAttemptFailed,
			FailurePhaseExecution,
			FailureOutcomeIncomplete,
		)
	}
}
