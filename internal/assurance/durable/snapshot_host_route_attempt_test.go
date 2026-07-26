package durable_test

import (
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestWithRecordedHostRouteAttemptsReplacesByCanonicalRouteIdentity(t *testing.T) {
	project := testHostRouteAttempt(
		t,
		"context7",
		target.ScopeProject,
		durableattempt.HostRouteResultAttemptedUnverified,
		durableattempt.HostRouteReasonObservationUnavailable,
		durableattempt.HostRouteAttemptReasonNone,
		observerelation.ObservationNotObserved,
		observerelation.PostconditionUnknown,
	)
	oldGlobal := testHostRouteAttempt(
		t,
		"context7",
		target.ScopeGlobal,
		durableattempt.HostRouteResultAttemptedUnverified,
		durableattempt.HostRouteReasonObservationUnavailable,
		durableattempt.HostRouteAttemptReasonNone,
		observerelation.ObservationNotObserved,
		observerelation.PostconditionUnknown,
	)
	replacement := testHostRouteAttempt(
		t,
		"context7",
		target.ScopeGlobal,
		durableattempt.HostRouteResultFailed,
		durableattempt.HostRouteReasonNonZeroExit,
		durableattempt.HostRouteAttemptReasonNonZeroExit,
		observerelation.ObservationNotObserved,
		observerelation.PostconditionFailed,
	)
	current := testSnapshot(t, durable.SnapshotInput{
		HostRouteAttempts: []durableattempt.HostRouteAttempt{project, oldGlobal},
	})

	next, err := current.WithRecordedHostRouteAttempts([]durableattempt.HostRouteAttempt{replacement})
	if err != nil {
		t.Fatalf("WithRecordedHostRouteAttempts returned error: %v", err)
	}
	attempts := next.HostRouteAttempts()
	if len(attempts) != 2 {
		t.Fatalf("host route attempts = %#v, want replacement plus scope-distinct row", attempts)
	}
	var retainedProject bool
	var replacedGlobal bool
	for _, attempt := range attempts {
		switch attempt.Scope() {
		case target.ScopeProject:
			retainedProject = attempt.Equal(project)
		case target.ScopeGlobal:
			replacedGlobal = attempt.Equal(replacement)
		}
	}
	if !retainedProject || !replacedGlobal {
		t.Fatalf("host route attempts = %#v, want project retained and global replaced", attempts)
	}
}

func TestWithRecordedHostRouteAttemptsReplacesPriorRequestForSameRoute(t *testing.T) {
	previous := testHostRouteAttemptWithHash(
		t,
		"context7",
		target.ScopeGlobal,
		"sha256:1111111111111111111111111111111111111111111111111111111111111111",
		durableattempt.HostRouteResultAttemptedUnverified,
		durableattempt.HostRouteReasonObservationUnavailable,
		durableattempt.HostRouteAttemptReasonNone,
		observerelation.ObservationNotObserved,
		observerelation.PostconditionUnknown,
	)
	replacement := testHostRouteAttemptWithHash(
		t,
		"context7",
		target.ScopeGlobal,
		"sha256:2222222222222222222222222222222222222222222222222222222222222222",
		durableattempt.HostRouteResultFailed,
		durableattempt.HostRouteReasonNonZeroExit,
		durableattempt.HostRouteAttemptReasonNonZeroExit,
		observerelation.ObservationNotObserved,
		observerelation.PostconditionFailed,
	)
	current := testSnapshot(t, durable.SnapshotInput{
		HostRouteAttempts: []durableattempt.HostRouteAttempt{previous},
	})

	next, err := current.WithRecordedHostRouteAttempts([]durableattempt.HostRouteAttempt{replacement})
	if err != nil {
		t.Fatalf("WithRecordedHostRouteAttempts returned error: %v", err)
	}
	attempts := next.HostRouteAttempts()
	if len(attempts) != 1 || !attempts[0].Equal(replacement) {
		t.Fatalf("host route attempts = %#v, want only latest request for the route", attempts)
	}
}

func TestWithRecordedHostRouteAttemptsKeepsOperationsAsDistinctHistory(t *testing.T) {
	install := testHostRouteAttempt(
		t,
		"context7",
		target.ScopeGlobal,
		durableattempt.HostRouteResultAttemptedUnverified,
		durableattempt.HostRouteReasonObservationUnavailable,
		durableattempt.HostRouteAttemptReasonNone,
		observerelation.ObservationNotObserved,
		observerelation.PostconditionUnknown,
	)
	refresh := testHostRouteAttemptForOperation(
		t,
		"context7",
		target.ScopeGlobal,
		lock.OperationRefresh,
		install.RouteID(),
		install.RouteRequestHash(),
		durableattempt.HostRouteResultAttemptedUnverified,
		durableattempt.HostRouteReasonObservationUnavailable,
		durableattempt.HostRouteAttemptReasonNone,
		observerelation.ObservationNotObserved,
		observerelation.PostconditionUnknown,
	)
	remove := testHostRouteAttemptForOperation(
		t,
		"context7",
		target.ScopeGlobal,
		lock.OperationRemove,
		install.RouteID(),
		install.RouteRequestHash(),
		durableattempt.HostRouteResultAttemptedUnverified,
		durableattempt.HostRouteReasonObservationUnavailable,
		durableattempt.HostRouteAttemptReasonNone,
		observerelation.ObservationNotObserved,
		observerelation.PostconditionUnknown,
	)

	snapshot, err := durable.EmptySnapshot().WithRecordedHostRouteAttempts(
		[]durableattempt.HostRouteAttempt{install, refresh, remove},
	)
	if err != nil {
		t.Fatalf("WithRecordedHostRouteAttempts returned error: %v", err)
	}
	attempts := snapshot.HostRouteAttempts()
	if len(attempts) != 3 ||
		attempts[0].Operation() != lock.OperationInstall ||
		attempts[1].Operation() != lock.OperationRefresh ||
		attempts[2].Operation() != lock.OperationRemove {
		t.Fatalf("host route attempts = %#v, want operation-distinct install, refresh, and remove rows", attempts)
	}
}

func TestWithRecordedHostRouteAttemptsNeverChangesCarrierFacts(t *testing.T) {
	root := t.TempDir()
	owner := mustStateAuthority(t, root, "daem.toml")
	pendingFixture := carrierFixtureFor(
		t,
		"pending",
		"pending@official",
		target.ScopeProject,
	)
	claimedFixture := carrierFixtureFor(
		t,
		"claimed",
		"claimed@official",
		target.ScopeProject,
	)
	pending, err := durablecarrier.NewPendingCarrierInstall(
		owner,
		pendingFixture.identity,
		pendingFixture.installRequest,
	)
	if err != nil {
		t.Fatal(err)
	}
	claim := claimForFixture(t, claimedFixture, owner)
	current := testSnapshot(t, durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending},
		ManagedCarrierClaims:   []durablecarrier.ManagedCarrierClaim{claim},
	})

	tests := []struct {
		name          string
		resultClass   durableattempt.HostRouteResultClass
		reason        durableattempt.HostRouteResultReason
		attemptReason durableattempt.HostRouteAttemptReason
		observation   observerelation.ObservationSummary
		postcondition observerelation.PostconditionSummary
	}{
		{
			name:          "observed present",
			resultClass:   durableattempt.HostRouteResultAttemptedObservedPresent,
			reason:        durableattempt.HostRouteReasonObservedPresent,
			observation:   observerelation.ObservationPresent,
			postcondition: observerelation.PostconditionObserved,
		},
		{
			name:          "attempted unverified",
			resultClass:   durableattempt.HostRouteResultAttemptedUnverified,
			reason:        durableattempt.HostRouteReasonObservationUnavailable,
			observation:   observerelation.ObservationNotObserved,
			postcondition: observerelation.PostconditionUnknown,
		},
		{
			name:          "observed absent",
			resultClass:   durableattempt.HostRouteResultAttemptedObservedAbsent,
			reason:        durableattempt.HostRouteReasonObservedAbsent,
			observation:   observerelation.ObservationMissing,
			postcondition: observerelation.PostconditionFailed,
		},
		{
			name:          "mechanical failure despite present observation",
			resultClass:   durableattempt.HostRouteResultFailed,
			reason:        durableattempt.HostRouteReasonNonZeroExit,
			attemptReason: durableattempt.HostRouteAttemptReasonNonZeroExit,
			observation:   observerelation.ObservationPresent,
			postcondition: observerelation.PostconditionObserved,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			attempt := testHostRouteAttempt(
				t,
				"context7",
				target.ScopeGlobal,
				test.resultClass,
				test.reason,
				test.attemptReason,
				test.observation,
				test.postcondition,
			)
			next, err := current.WithRecordedHostRouteAttempts(
				[]durableattempt.HostRouteAttempt{attempt},
			)
			if err != nil {
				t.Fatalf("WithRecordedHostRouteAttempts returned error: %v", err)
			}
			if got := next.PendingCarrierInstalls(); len(got) != 1 ||
				!got[0].ExactEqual(pending) {
				t.Fatalf("pending facts changed from attempt history: %#v", got)
			}
			if got := next.ManagedCarrierClaims(); len(got) != 1 ||
				!got[0].ExactEqual(claim) {
				t.Fatalf("managed claims changed from attempt history: %#v", got)
			}
		})
	}
}

func testHostRouteAttempt(
	t *testing.T,
	name string,
	scope target.Scope,
	resultClass durableattempt.HostRouteResultClass,
	reason durableattempt.HostRouteResultReason,
	attemptReason durableattempt.HostRouteAttemptReason,
	observation observerelation.ObservationSummary,
	postcondition observerelation.PostconditionSummary,
) durableattempt.HostRouteAttempt {
	t.Helper()
	return testHostRouteAttemptWithHash(
		t,
		name,
		scope,
		"sha256:1111111111111111111111111111111111111111111111111111111111111111",
		resultClass,
		reason,
		attemptReason,
		observation,
		postcondition,
	)
}

func testHostRouteAttemptWithHash(
	t *testing.T,
	name string,
	scope target.Scope,
	routeRequestHash string,
	resultClass durableattempt.HostRouteResultClass,
	reason durableattempt.HostRouteResultReason,
	attemptReason durableattempt.HostRouteAttemptReason,
	observation observerelation.ObservationSummary,
	postcondition observerelation.PostconditionSummary,
) durableattempt.HostRouteAttempt {
	return testHostRouteAttemptForOperation(
		t,
		name,
		scope,
		lock.OperationInstall,
		"claude-code.plugin-carrier.install",
		routeRequestHash,
		resultClass,
		reason,
		attemptReason,
		observation,
		postcondition,
	)
}

func testHostRouteAttemptForOperation(
	t *testing.T,
	name string,
	scope target.Scope,
	operation lock.OperationKind,
	routeID string,
	routeRequestHash string,
	resultClass durableattempt.HostRouteResultClass,
	reason durableattempt.HostRouteResultReason,
	attemptReason durableattempt.HostRouteAttemptReason,
	observation observerelation.ObservationSummary,
	postcondition observerelation.PostconditionSummary,
) durableattempt.HostRouteAttempt {
	t.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"claude-code.plugin-carrier",
		name,
	)
	if err != nil {
		t.Fatalf("NewSubjectID: %v", err)
	}
	var exitCode *int
	if attemptReason == durableattempt.HostRouteAttemptReasonNonZeroExit {
		value := 17
		exitCode = &value
	}
	attempt, err := durableattempt.NewHostRouteAttempt(durableattempt.HostRouteAttemptInput{
		Subject:          subject,
		Target:           target.TargetClaudeCode,
		Scope:            scope,
		Operation:        operation,
		RouteID:          routeID,
		RouteRequestHash: routeRequestHash,
		ObservedAt:       time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC),
		ResultClass:      resultClass,
		Reason:           reason,
		AttemptObserved:  true,
		AttemptReason:    attemptReason,
		ExitCode:         exitCode,
		Observation:      observation,
		Postcondition:    postcondition,
	})
	if err != nil {
		t.Fatalf("NewHostRouteAttempt: %v", err)
	}
	return attempt
}
