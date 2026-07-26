package durable_test

import (
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestWithRecordedDelegateAttemptsReplacesBySubjectTargetScope(t *testing.T) {
	old := testDelegateAttempt(t, "context7", "old-plan", durableattempt.DelegateStatusSucceeded, durableattempt.DelegateReasonNone)
	other := testDelegateAttempt(t, "other", "other-plan", durableattempt.DelegateStatusSucceeded, durableattempt.DelegateReasonNone)
	current := testSnapshot(t, durable.SnapshotInput{DelegateAttempts: []durableattempt.DelegateAttempt{old, other}})
	replacement := testDelegateAttempt(
		t,
		"context7",
		"new-plan",
		durableattempt.DelegateStatusFailed,
		durableattempt.DelegateReasonNonZeroExit,
	)

	next, err := current.WithRecordedDelegateAttempts([]durableattempt.DelegateAttempt{replacement})
	if err != nil {
		t.Fatalf("WithRecordedDelegateAttempts returned error: %v", err)
	}

	attempts := next.DelegateAttempts()
	if len(attempts) != 2 {
		t.Fatalf("delegate attempts = %#v, want replacement plus unrelated row", attempts)
	}
	var replaced bool
	var retained bool
	for _, attempt := range attempts {
		switch attempt.Subject().Key() {
		case "context7":
			replaced = attempt.PlanIdentityKey() == "new-plan" &&
				attempt.Status() == durableattempt.DelegateStatusFailed
		case "other":
			retained = attempt.PlanIdentityKey() == "other-plan"
		}
	}
	if !replaced || !retained {
		t.Fatalf("delegate attempts = %#v, want replaced context7 and retained other", attempts)
	}
}

func TestWithRecordedDelegateAttemptsPreservesOtherFamilies(t *testing.T) {
	route := testHostRouteAttempt(
		t,
		"context7",
		target.ScopeProject,
		durableattempt.HostRouteResultAttemptedUnverified,
		durableattempt.HostRouteReasonObservationUnavailable,
		durableattempt.HostRouteAttemptReasonNone,
		observerelation.ObservationNotObserved,
		observerelation.PostconditionUnknown,
	)
	current := testSnapshot(t, durable.SnapshotInput{HostRouteAttempts: []durableattempt.HostRouteAttempt{route}})

	next, err := current.WithRecordedDelegateAttempts(nil)
	if err != nil {
		t.Fatalf("WithRecordedDelegateAttempts returned error: %v", err)
	}
	if len(next.HostRouteAttempts()) != 1 || !next.HostRouteAttempts()[0].Equal(route) {
		t.Fatalf("host route attempts = %#v, want preserved history", next.HostRouteAttempts())
	}
}

func TestWithRecordedDelegateAttemptsUsesLastDuplicateInput(t *testing.T) {
	first := testDelegateAttempt(
		t,
		"context7",
		"first-plan",
		durableattempt.DelegateStatusSucceeded,
		durableattempt.DelegateReasonNone,
	)
	last := testDelegateAttempt(
		t,
		"context7",
		"last-plan",
		durableattempt.DelegateStatusFailed,
		durableattempt.DelegateReasonNonZeroExit,
	)

	next, err := durable.EmptySnapshot().WithRecordedDelegateAttempts(
		[]durableattempt.DelegateAttempt{first, last},
	)
	if err != nil {
		t.Fatalf("WithRecordedDelegateAttempts returned error: %v", err)
	}
	attempts := next.DelegateAttempts()
	if len(attempts) != 1 || !attempts[0].Equal(last) {
		t.Fatalf("delegate attempts = %#v, want last duplicate input", attempts)
	}
}

func testDelegateAttempt(
	t *testing.T,
	name string,
	planIdentity string,
	status durableattempt.DelegateAttemptStatus,
	reason durableattempt.DelegateAttemptReason,
) durableattempt.DelegateAttempt {
	t.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectProjection,
		"claude-code.project.mcp-server",
		name,
	)
	if err != nil {
		t.Fatalf("NewSubjectID: %v", err)
	}
	var exitCode *int
	if reason == durableattempt.DelegateReasonNonZeroExit {
		value := 17
		exitCode = &value
	}
	attempt, err := durableattempt.NewDelegateAttempt(durableattempt.DelegateAttemptInput{
		Subject:         subject,
		Target:          target.TargetClaudeCode,
		Scope:           target.ScopeProject,
		PlanIdentityKey: planIdentity,
		ObservedAt:      time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC),
		Status:          status,
		Reason:          reason,
		ExitCode:        exitCode,
		Observation:     observerelation.ObservationNotObserved,
		Postcondition:   observerelation.PostconditionNotObserved,
	})
	if err != nil {
		t.Fatalf("NewDelegateAttempt: %v", err)
	}
	return attempt
}

func testSnapshot(t *testing.T, input durable.SnapshotInput) durable.Snapshot {
	t.Helper()
	snapshot, err := durable.NewSnapshot(input)
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	return snapshot
}
