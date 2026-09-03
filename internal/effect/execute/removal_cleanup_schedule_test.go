package execute

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/operationplan"
)

func TestRemovalCleanupStructureIsCompactDeterministicAndDemandFree(t *testing.T) {
	first, err := compileRemovalCleanupStructure(recovery.MaximumRemovalIntents)
	if err != nil {
		t.Fatalf("compile maximum removal cleanup structure: %v", err)
	}
	second, err := compileRemovalCleanupStructure(recovery.MaximumRemovalIntents)
	if err != nil {
		t.Fatalf("compile repeated maximum removal cleanup structure: %v", err)
	}
	if !first.Equal(second) {
		t.Fatal("removal cleanup structure changed across identical compilation")
	}
	demand, err := first.LegacyDemand()
	if err != nil {
		t.Fatalf("derive removal cleanup demand: %v", err)
	}
	if demand != (operationplan.Demand{}) {
		t.Fatalf("removal cleanup demand = %#v, want zero", demand)
	}
	alternatives, err := first.DemandAlternatives()
	if err != nil {
		t.Fatalf("derive removal cleanup demand alternatives: %v", err)
	}
	if len(alternatives) != 1 || alternatives[0] != (operationplan.Demand{}) {
		t.Fatalf("removal cleanup alternatives = %#v, want one zero-demand alternative", alternatives)
	}
}

func TestRemovalCleanupExecutionSupportsEmptyIntentSet(t *testing.T) {
	structure, err := compileRemovalCleanupStructure(0)
	if err != nil {
		t.Fatal(err)
	}
	execution := newRemovalCleanupExecution(structure, 0)
	if err := execution.admitCompletion(); err != nil {
		t.Fatal(err)
	}
	if err := execution.settleCompletion(true); err != nil {
		t.Fatal(err)
	}
	if err := execution.finish(nil); err != nil {
		t.Fatal(err)
	}
}

func TestRemovalCleanupExecutionConsumesEveryActionAndCompletion(t *testing.T) {
	structure, err := compileRemovalCleanupStructure(3)
	if err != nil {
		t.Fatal(err)
	}
	execution := newRemovalCleanupExecution(structure, 3)
	for _, action := range []recovery.RemovalCleanupActionKind{
		recovery.RemovalCleanupActionConfirmAbsence,
		recovery.RemovalCleanupActionPromoteResidue,
		recovery.RemovalCleanupActionCleanupProgress,
	} {
		consumeRemovalCleanupIntentSuccess(t, execution, action)
	}
	if err := execution.admitCompletion(); err != nil {
		t.Fatalf("admit completion: %v", err)
	}
	if err := execution.settleCompletion(true); err != nil {
		t.Fatalf("settle completion: %v", err)
	}
	if err := execution.finish(nil); err != nil {
		t.Fatalf("finish removal cleanup: %v", err)
	}
}

func TestRemovalCleanupExecutionObservationFailureStopsLaterIntent(t *testing.T) {
	structure, err := compileRemovalCleanupStructure(2)
	if err != nil {
		t.Fatal(err)
	}
	execution := newRemovalCleanupExecution(structure, 2)
	if err := execution.admitObservation(); err != nil {
		t.Fatal(err)
	}
	if err := execution.settleObservation(false); err != nil {
		t.Fatal(err)
	}
	if err := execution.admitAction(recovery.RemovalCleanupActionConfirmAbsence); err == nil {
		t.Fatal("observation failure admitted an action")
	}
	if err := execution.admitObservation(); err == nil {
		t.Fatal("observation failure admitted a later intent")
	}
	failure := errors.New("observation failed")
	if err := execution.finish(failure); err != failure {
		t.Fatalf("finish error = %v, want original observation failure", err)
	}
}

func TestRemovalCleanupExecutionActionFailureStopsLaterIntent(t *testing.T) {
	structure, err := compileRemovalCleanupStructure(2)
	if err != nil {
		t.Fatal(err)
	}
	execution := newRemovalCleanupExecution(structure, 2)
	if err := execution.admitObservation(); err != nil {
		t.Fatal(err)
	}
	if err := execution.settleObservation(true); err != nil {
		t.Fatal(err)
	}
	if err := execution.admitAction(recovery.RemovalCleanupActionPromoteResidue); err != nil {
		t.Fatal(err)
	}
	if err := execution.settleAction(recovery.RemovalCleanupActionPromoteResidue, false); err != nil {
		t.Fatal(err)
	}
	if err := execution.admitObservation(); err == nil {
		t.Fatal("action failure admitted a later intent")
	}
	if err := execution.admitCompletion(); err == nil {
		t.Fatal("action failure admitted completion")
	}
	failure := errors.New("promotion failed")
	if err := execution.finish(failure); err != failure {
		t.Fatalf("finish error = %v, want original action failure", err)
	}
}

func TestRemovalCleanupExecutionJoinsPrimaryAndStructuralFailure(t *testing.T) {
	structure, err := compileRemovalCleanupStructure(1)
	if err != nil {
		t.Fatal(err)
	}
	execution := newRemovalCleanupExecution(structure, 1)
	primary := errors.New("cleanup preparation failed")
	joined := execution.finish(primary)
	if !errors.Is(joined, primary) {
		t.Fatalf("finish error = %v, want primary failure", joined)
	}
	if joined == primary {
		t.Fatal("under-consumed structure was not joined with the primary failure")
	}
}

func TestRemovalCleanupExecutionMismatchDoesNotConsumeExpectedStep(t *testing.T) {
	structure, err := compileRemovalCleanupStructure(1)
	if err != nil {
		t.Fatal(err)
	}
	execution := newRemovalCleanupExecution(structure, 1)
	if err := execution.admitAction(recovery.RemovalCleanupActionCleanupProgress); err == nil {
		t.Fatal("action was admitted before effect-time observation")
	}
	consumeRemovalCleanupIntentSuccess(
		t,
		execution,
		recovery.RemovalCleanupActionCleanupProgress,
	)
	if err := execution.admitCompletion(); err != nil {
		t.Fatal(err)
	}
	if err := execution.settleCompletion(false); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("retirement authority incomplete")
	if err := execution.finish(failure); err != failure {
		t.Fatalf("finish error = %v, want original completion failure", err)
	}
}

func TestRemovalCleanupExecutionRejectsWrongAndDuplicateSettlement(t *testing.T) {
	structure, err := compileRemovalCleanupStructure(1)
	if err != nil {
		t.Fatal(err)
	}
	execution := newRemovalCleanupExecution(structure, 1)
	if err := execution.admitObservation(); err != nil {
		t.Fatal(err)
	}
	if err := execution.settleObservation(true); err != nil {
		t.Fatal(err)
	}
	if err := execution.admitAction(recovery.RemovalCleanupActionPromoteResidue); err != nil {
		t.Fatal(err)
	}
	if err := execution.settleAction(recovery.RemovalCleanupActionCleanupProgress, true); err == nil {
		t.Fatal("wrong removal cleanup action settled the pending step")
	}
	if err := execution.settleAction(recovery.RemovalCleanupActionPromoteResidue, true); err != nil {
		t.Fatalf("correct settlement failed after mismatch: %v", err)
	}
	if err := execution.settleAction(recovery.RemovalCleanupActionPromoteResidue, true); err == nil {
		t.Fatal("duplicate removal cleanup action settlement succeeded")
	}
	if err := execution.admitCompletion(); err != nil {
		t.Fatal(err)
	}
	if err := execution.settleCompletion(true); err != nil {
		t.Fatal(err)
	}
	if err := execution.finish(nil); err != nil {
		t.Fatal(err)
	}
}

func TestRemovalCleanupStructureRejectsInvalidCardinalityAndAction(t *testing.T) {
	for _, count := range []int{-1, recovery.MaximumRemovalIntents + 1} {
		if _, err := compileRemovalCleanupStructure(count); err == nil {
			t.Fatalf("compile removal cleanup structure accepted count %d", count)
		}
	}

	structure, err := compileRemovalCleanupStructure(1)
	if err != nil {
		t.Fatal(err)
	}
	execution := newRemovalCleanupExecution(structure, 1)
	if err := execution.admitObservation(); err != nil {
		t.Fatal(err)
	}
	if err := execution.settleObservation(true); err != nil {
		t.Fatal(err)
	}
	if err := execution.admitAction(recovery.RemovalCleanupActionKind("unknown")); err == nil {
		t.Fatal("unknown removal cleanup action was admitted")
	}
	if err := execution.admitAction(recovery.RemovalCleanupActionConfirmAbsence); err != nil {
		t.Fatalf("expected action was consumed by an earlier mismatch: %v", err)
	}
	if err := execution.settleAction(recovery.RemovalCleanupActionConfirmAbsence, false); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("confirm absence failed")
	if err := execution.finish(failure); err != failure {
		t.Fatalf("finish error = %v, want original action failure", err)
	}
}

func consumeRemovalCleanupIntentSuccess(
	t *testing.T,
	execution *removalCleanupExecution,
	action recovery.RemovalCleanupActionKind,
) {
	t.Helper()
	if err := execution.admitObservation(); err != nil {
		t.Fatalf("admit observation: %v", err)
	}
	if err := execution.settleObservation(true); err != nil {
		t.Fatalf("settle observation: %v", err)
	}
	if err := execution.admitAction(action); err != nil {
		t.Fatalf("admit action %q: %v", action, err)
	}
	if err := execution.settleAction(action, true); err != nil {
		t.Fatalf("settle action %q: %v", action, err)
	}
}
