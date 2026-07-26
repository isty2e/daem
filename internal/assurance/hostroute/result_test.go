package hostroute

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/subprocess"

	"github.com/isty2e/daem/internal/realization"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestClassifyResultSuccessWithPresentRelation(t *testing.T) {
	fixture, command := resultFixture(t)
	result := classifyResult(t, command, successfulAttempt(t), observationFact(t, fixture, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			mustClaudeRow(t, "context7@market", string(fixture.subject.ExpectedRelation().ManagedInstanceKey()), true),
		},
	}))

	if result.Class() != ResultAttemptedObservedPresent {
		t.Fatalf("class = %q, want %q", result.Class(), ResultAttemptedObservedPresent)
	}
	if result.Subject() != command.Subject() || result.RouteRequest() != command.RouteRequest() {
		t.Fatalf("result identity = %#v/%#v, want command identity", result.Subject(), result.RouteRequest())
	}
	assertSummaries(
		t,
		result,
		observerelation.ObservationPresent,
		observerelation.PostconditionObserved,
	)
}

func TestClassifyResultSuccessWithoutObservationIsAttemptedUnverified(t *testing.T) {
	_, command := resultFixture(t)
	result := classifyResult(
		t,
		command,
		successfulAttempt(t),
		ObservationUnavailable(ResultReasonObservationUnavailable),
	)

	if result.Class() != ResultAttemptedUnverified {
		t.Fatalf("class = %q, want %q", result.Class(), ResultAttemptedUnverified)
	}
	if result.StateSummary().Reason() != ResultReasonObservationUnavailable {
		t.Fatalf("reason = %q, want %q", result.StateSummary().Reason(), ResultReasonObservationUnavailable)
	}
	assertSummaries(
		t,
		result,
		observerelation.ObservationNotObserved,
		observerelation.PostconditionUnknown,
	)
}

func TestClassifyResultSuccessWithUnmanagedSameNameBlocksAdoption(t *testing.T) {
	fixture, command := resultFixture(t)
	result := classifyResult(t, command, successfulAttempt(t), observationFact(t, fixture, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			mustClaudeRow(t, "context7@market", "", false),
		},
	}))

	if result.Class() != ResultBlocked {
		t.Fatalf("class = %q, want %q", result.Class(), ResultBlocked)
	}
	if result.StateSummary().Reason() != ResultReasonUnkeyedSameSubject {
		t.Fatalf("reason = %q, want %q", result.StateSummary().Reason(), ResultReasonUnkeyedSameSubject)
	}
	assertSummaries(
		t,
		result,
		observerelation.ObservationUnknown,
		observerelation.PostconditionUnknown,
	)
}

func TestClassifyResultPresentRelationPostconditionAcceptsExternalRelation(t *testing.T) {
	fixture, command := resultFixture(t)
	result, err := ClassifyResult(ResultInput{
		Subject:      command.Subject(),
		RouteRequest: command.RouteRequest(),
		Attempt:      successfulAttempt(t),
		Observation: observationFact(t, fixture, observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows: []observeclaudeplugin.Row{
				mustClaudeRow(t, "context7@market", "", false),
			},
		}),
		RequiredPostcondition: RequireRelationPostcondition(RelationPostconditionPresent),
	})
	if err != nil {
		t.Fatalf("ClassifyResult returned error: %v", err)
	}
	if result.Class() != ResultAttemptedObservedPresent ||
		result.StateSummary().Reason() != ResultReasonObservedPresent {
		t.Fatalf("result class/reason = %q/%q", result.Class(), result.StateSummary().Reason())
	}
	assertSummaries(
		t,
		result,
		observerelation.ObservationPresent,
		observerelation.PostconditionObserved,
	)
}

func TestClassifyResultRejectsUnknownRelationPostcondition(t *testing.T) {
	fixture, command := resultFixture(t)
	_, err := ClassifyResult(ResultInput{
		Subject:      command.Subject(),
		RouteRequest: command.RouteRequest(),
		Attempt:      successfulAttempt(t),
		Observation: observationFact(t, fixture, observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
		}),
		RequiredPostcondition: RequireRelationPostcondition(RelationPostcondition(255)),
	})
	if err == nil || !strings.Contains(err.Error(), "postcondition 255 is unsupported") {
		t.Fatalf("ClassifyResult error = %v, want unsupported postcondition", err)
	}
}

func TestClassifyResultSuccessWithMissingRelationIsObservedAbsent(t *testing.T) {
	fixture, command := resultFixture(t)
	result := classifyResult(t, command, successfulAttempt(t), observationFact(t, fixture, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	}))

	if result.Class() != ResultAttemptedObservedAbsent {
		t.Fatalf("class = %q, want %q", result.Class(), ResultAttemptedObservedAbsent)
	}
	if result.StateSummary().Reason() != ResultReasonObservedAbsent {
		t.Fatalf("reason = %q, want %q", result.StateSummary().Reason(), ResultReasonObservedAbsent)
	}
	assertSummaries(
		t,
		result,
		observerelation.ObservationMissing,
		observerelation.PostconditionMissing,
	)
}

func TestClassifyResultSuccessWithConflictStatesNeverAdoptsByName(t *testing.T) {
	tests := []struct {
		name       string
		rows       []observeclaudeplugin.Row
		wantClass  ResultClass
		wantReason ResultReasonCode
	}{
		{
			name: "same name shadow",
			rows: []observeclaudeplugin.Row{
				mustClaudeRow(t, "context7@market", managedKeyForDifferentSource(t), true),
			},
			wantClass:  ResultBlocked,
			wantReason: ResultReasonSameSubjectShadow,
		},
		{
			name: "managed key drift",
			rows: []observeclaudeplugin.Row{
				mustClaudeRow(t, "renamed-context7", managedKeyForFixture(t), true),
			},
			wantClass:  ResultBlocked,
			wantReason: ResultReasonManagedKeyDrift,
		},
		{
			name: "ambiguous managed rows",
			rows: []observeclaudeplugin.Row{
				mustClaudeRow(t, "context7@market", managedKeyForFixture(t), true),
				mustClaudeRow(t, "context7-copy", managedKeyForFixture(t), true),
			},
			wantClass:  ResultAmbiguousObservation,
			wantReason: ResultReasonAmbiguousRelation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture, command := resultFixture(t)
			result := classifyResult(t, command, successfulAttempt(t), observationFact(t, fixture, observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows:         tt.rows,
			}))
			if result.Class() != tt.wantClass {
				t.Fatalf("class = %q, want %q", result.Class(), tt.wantClass)
			}
			if result.StateSummary().Reason() != tt.wantReason {
				t.Fatalf("reason = %q, want %q", result.StateSummary().Reason(), tt.wantReason)
			}
			assertSummaries(
				t,
				result,
				observerelation.ObservationUnknown,
				observerelation.PostconditionUnknown,
			)
		})
	}
}

func TestClassifyResultFailedAttemptPreservesPartialObservation(t *testing.T) {
	fixture, command := resultFixture(t)
	result := classifyResult(t, command, failedAttempt(t), observationFact(t, fixture, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			mustClaudeRow(t, "context7@market", string(fixture.subject.ExpectedRelation().ManagedInstanceKey()), true),
		},
	}))

	if result.Class() != ResultFailed {
		t.Fatalf("class = %q, want %q", result.Class(), ResultFailed)
	}
	if result.StateSummary().Reason() != ResultReasonNonZeroExit {
		t.Fatalf("reason = %q, want %q", result.StateSummary().Reason(), ResultReasonNonZeroExit)
	}
	assertSummaries(
		t,
		result,
		observerelation.ObservationPresent,
		observerelation.PostconditionObserved,
	)
}

func TestClassifyResultFailedAttemptWithoutObservationKeepsDiagnosticsOnly(t *testing.T) {
	_, command := resultFixture(t)
	result := classifyResult(
		t,
		command,
		failedAttempt(t),
		ObservationUnavailable(ResultReasonObservationUnavailable),
	)
	if result.Class() != ResultFailed {
		t.Fatalf("class = %q, want %q", result.Class(), ResultFailed)
	}
	if result.StateSummary().Reason() != ResultReasonNonZeroExit {
		t.Fatalf("reason = %q, want %q", result.StateSummary().Reason(), ResultReasonNonZeroExit)
	}
	assertSummaries(
		t,
		result,
		observerelation.ObservationNotObserved,
		observerelation.PostconditionNotObserved,
	)
	exitCode, ok := result.StateSummary().ExitCode()
	if !ok || exitCode != 2 {
		t.Fatalf("state summary exit = %d/%v, want 2/true", exitCode, ok)
	}
}

func TestClassifyResultMechanicalFailureReasonsStayMechanical(t *testing.T) {
	tests := []struct {
		name       string
		raw        subprocess.CommandResult
		wantReason ResultReasonCode
	}{
		{
			name:       "missing runner",
			raw:        subprocess.CommandResult{MissingRunner: true},
			wantReason: ResultReasonMissingRunner,
		},
		{
			name:       "timeout",
			raw:        subprocess.CommandResult{Started: true, TimedOut: true},
			wantReason: ResultReasonTimeout,
		},
		{
			name:       "canceled",
			raw:        subprocess.CommandResult{Started: true, Canceled: true},
			wantReason: ResultReasonRunnerError,
		},
		{
			name:       "signaled",
			raw:        subprocess.CommandResult{Started: true, Signaled: true},
			wantReason: ResultReasonRunnerError,
		},
		{
			name:       "runner error",
			raw:        subprocess.CommandResult{Started: true, Err: errSyntheticRunner},
			wantReason: ResultReasonRunnerError,
		},
		{
			name:       "workdir authority",
			raw:        subprocess.CommandResult{Started: true, WorkDirAuthorityFailed: true},
			wantReason: ResultReasonWorkDirAuthority,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture, command := resultFixture(t)
			result := classifyResult(t, command, attemptFromCommandResult(t, tt.raw, subprocess.CommandAttemptRequest{Command: "claude"}), observationFact(t, fixture, observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows: []observeclaudeplugin.Row{
					mustClaudeRow(t, "context7@market", string(fixture.subject.ExpectedRelation().ManagedInstanceKey()), true),
				},
			}))
			if result.Class() != ResultFailed {
				t.Fatalf("class = %q, want %q", result.Class(), ResultFailed)
			}
			if result.StateSummary().Reason() != tt.wantReason {
				t.Fatalf("reason = %q, want %q", result.StateSummary().Reason(), tt.wantReason)
			}
		})
	}
}

func TestClassifyResultObservationParseFailureIsAttemptedUnverified(t *testing.T) {
	_, command := resultFixture(t)
	result := classifyResult(
		t,
		command,
		successfulAttempt(t),
		ObservationUnavailable(ResultReasonObservationParseFailed),
	)

	if result.Class() != ResultAttemptedUnverified {
		t.Fatalf("class = %q, want %q", result.Class(), ResultAttemptedUnverified)
	}
	if result.StateSummary().Reason() != ResultReasonObservationParseFailed {
		t.Fatalf("reason = %q, want %q", result.StateSummary().Reason(), ResultReasonObservationParseFailed)
	}
}

func TestClassifyResultUnsupportedAndUnavailableObservationAreUnverified(t *testing.T) {
	tests := []struct {
		name       string
		spec       observeclaudeplugin.InventorySpec
		wantReason ResultReasonCode
	}{
		{
			name: "unsupported",
			spec: observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventoryUnsupported,
				Freshness:    observerelation.EvidenceFresh,
			},
			wantReason: ResultReasonObservationUnsupported,
		},
		{
			name: "unavailable",
			spec: observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventoryUnavailable,
				Freshness:    observerelation.EvidenceFresh,
			},
			wantReason: ResultReasonObservationUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture, command := resultFixture(t)
			result := classifyResult(t, command, successfulAttempt(t), observationFact(t, fixture, tt.spec))
			if result.Class() != ResultAttemptedUnverified {
				t.Fatalf("class = %q, want %q", result.Class(), ResultAttemptedUnverified)
			}
			if result.StateSummary().Reason() != tt.wantReason {
				t.Fatalf("reason = %q, want %q", result.StateSummary().Reason(), tt.wantReason)
			}
		})
	}
}

func TestObservationUnavailableCanonicalizesNonObservationReason(t *testing.T) {
	_, command := resultFixture(t)
	result := classifyResult(
		t,
		command,
		successfulAttempt(t),
		ObservationUnavailable(ResultReasonNonZeroExit),
	)
	if result.Class() != ResultAttemptedUnverified {
		t.Fatalf("class = %q, want %q", result.Class(), ResultAttemptedUnverified)
	}
	if result.StateSummary().Reason() != ResultReasonObservationUnavailable {
		t.Fatalf("reason = %q, want canonical observation unavailable", result.StateSummary().Reason())
	}
}

func TestClassifyResultStaleObservationIsAttemptedUnverified(t *testing.T) {
	fixture, command := resultFixture(t)
	result := classifyResult(t, command, successfulAttempt(t), observationFact(t, fixture, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceStale,
		Rows: []observeclaudeplugin.Row{
			mustClaudeRow(t, "context7@market", string(fixture.subject.ExpectedRelation().ManagedInstanceKey()), true),
		},
	}))

	if result.Class() != ResultAttemptedUnverified {
		t.Fatalf("class = %q, want %q", result.Class(), ResultAttemptedUnverified)
	}
	if result.StateSummary().Reason() != ResultReasonObservationStale {
		t.Fatalf("reason = %q, want %q", result.StateSummary().Reason(), ResultReasonObservationStale)
	}
	assertSummaries(
		t,
		result,
		observerelation.ObservationUnknown,
		observerelation.PostconditionUnknown,
	)
}

func TestClassifyResultExplicitUnavailableStaleReasonStaysObservationScoped(t *testing.T) {
	_, command := resultFixture(t)
	result := classifyResult(
		t,
		command,
		successfulAttempt(t),
		ObservationUnavailable(ResultReasonObservationStale),
	)
	if result.Class() != ResultAttemptedUnverified {
		t.Fatalf("class = %q, want %q", result.Class(), ResultAttemptedUnverified)
	}
	if result.StateSummary().Reason() != ResultReasonObservationStale {
		t.Fatalf("reason = %q, want %q", result.StateSummary().Reason(), ResultReasonObservationStale)
	}
}

func TestClassifyResultMissingEnvRefDoesNotBorrowObservationAuthority(t *testing.T) {
	fixture, command := resultFixture(t)
	result := classifyResult(t, command, attemptFromCommandResult(t, subprocess.CommandResult{
		Started:     true,
		HasExitCode: true,
		ExitCode:    0,
	}, subprocess.CommandAttemptRequest{
		Command: "claude",
		EnvRefs: []subprocess.CommandEnvRef{
			{Name: "MISSING_TOKEN", SourceName: "MISSING_TOKEN"},
		},
	}), observationFact(t, fixture, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			mustClaudeRow(t, "context7@market", string(fixture.subject.ExpectedRelation().ManagedInstanceKey()), true),
		},
	}))

	if result.Class() != ResultFailed {
		t.Fatalf("class = %q, want %q", result.Class(), ResultFailed)
	}
	if result.StateSummary().Reason() != ResultReasonMissingEnvRef {
		t.Fatalf("reason = %q, want %q", result.StateSummary().Reason(), ResultReasonMissingEnvRef)
	}
	assertSummaries(
		t,
		result,
		observerelation.ObservationPresent,
		observerelation.PostconditionObserved,
	)
}

func TestClassifyResultRejectsUnknownObservationState(t *testing.T) {
	_, command := resultFixture(t)
	for _, test := range []struct {
		name    string
		attempt AttemptFact
	}{
		{name: "succeeded", attempt: successfulAttempt(t)},
		{name: "failed", attempt: failedAttempt(t)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ClassifyResult(ResultInput{
				Subject:               command.Subject(),
				RouteRequest:          command.RouteRequest(),
				Attempt:               test.attempt,
				Observation:           CurrentObservation(observerelation.CorrelationResult{}),
				RequiredPostcondition: RequireRelationPostcondition(RelationPostconditionExact),
			})
			if err == nil || !strings.Contains(err.Error(), string(ResultReasonUnsupportedObservation)) {
				t.Fatalf("error = %v, want unsupported observation diagnostic", err)
			}
		})
	}
}

func TestClassifyResultRejectsMissingCurrentAttempt(t *testing.T) {
	_, command := resultFixture(t)
	_, err := ClassifyResult(ResultInput{
		Subject:               command.Subject(),
		RouteRequest:          command.RouteRequest(),
		Observation:           ObservationUnavailable(""),
		RequiredPostcondition: RequireRelationPostcondition(RelationPostconditionExact),
	})
	if err == nil || !strings.Contains(err.Error(), string(ResultReasonAttemptMissing)) {
		t.Fatalf("error = %v, want missing current attempt diagnostic", err)
	}
}

func TestClassifyResultRejectsZeroCommandIdentity(t *testing.T) {
	_, err := ClassifyResult(ResultInput{
		Attempt: successfulAttempt(t),
	})
	if err == nil || !strings.Contains(err.Error(), string(ResultReasonUnsupportedObservation)) {
		t.Fatalf("error = %v, want command identity diagnostic", err)
	}
}

func TestClassifyResultRejectsZeroRouteIdentity(t *testing.T) {
	fixture, _ := resultFixture(t)
	_, err := ClassifyResult(ResultInput{
		Subject: fixture.subjectID,
		Attempt: successfulAttempt(t),
	})
	if err == nil || !strings.Contains(err.Error(), string(ResultReasonUnsupportedObservation)) {
		t.Fatalf("error = %v, want route identity diagnostic", err)
	}
}

func TestClassifyResultRejectsUnknownAndContradictoryAttemptFacts(t *testing.T) {
	_, command := resultFixture(t)
	tests := []struct {
		name   string
		mutate func(*AttemptFact)
	}{
		{
			name: "unknown reason",
			mutate: func(attempt *AttemptFact) {
				attempt.reason = AttemptReason("future_reason")
			},
		},
		{
			name: "success marked timed out",
			mutate: func(attempt *AttemptFact) {
				attempt.timedOut = true
			},
		},
		{
			name: "timeout reason without timeout fact",
			mutate: func(attempt *AttemptFact) {
				attempt.reason = AttemptReasonTimeout
			},
		},
		{
			name: "nonzero reason without exit code",
			mutate: func(attempt *AttemptFact) {
				attempt.reason = AttemptReasonNonZeroExit
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			attempt := successfulAttempt(t)
			test.mutate(&attempt)
			_, err := ClassifyResult(ResultInput{
				Subject:               command.Subject(),
				RouteRequest:          command.RouteRequest(),
				Attempt:               attempt,
				RequiredPostcondition: RequireRelationPostcondition(RelationPostconditionExact),
			})
			if err == nil || !strings.Contains(err.Error(), string(ResultReasonCommandFailed)) {
				t.Fatalf("error = %v, want contradictory attempt diagnostic", err)
			}
		})
	}
}

var errSyntheticRunner = errors.New("synthetic runner failure")

func TestClassifyResultStateSummaryExcludesDiagnosticTextAuthority(t *testing.T) {
	fixture, command := resultFixture(t)
	attempt := attemptFromCommandResult(t, subprocess.CommandResult{
		Started:     true,
		Stdout:      "installed token=super-secret cache=/Users/alice/.claude/plugins/context7",
		Stderr:      "stderr api_key=super-secret artifact=sha256:abc",
		HasExitCode: true,
		ExitCode:    0,
	}, subprocess.CommandAttemptRequest{
		Command: "claude",
		EnvRefs: []subprocess.CommandEnvRef{
			{Name: "CLAUDE_TOKEN", SourceName: "CLAUDE_TOKEN"},
		},
	})
	result := classifyResult(t, command, attempt, observationFact(t, fixture, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			mustClaudeRow(t, "context7@market", string(fixture.subject.ExpectedRelation().ManagedInstanceKey()), true),
		},
	}))

	state := result.StateSummary()
	if state.Class() != ResultAttemptedObservedPresent ||
		state.Observation() != observerelation.ObservationPresent ||
		state.Postcondition() != observerelation.PostconditionObserved {
		t.Fatalf("state summary = %#v", state)
	}
	renderedState := fmt.Sprintf("%#v", state)
	for _, forbidden := range []string{"installed token", "cache=/Users", "artifact=sha256", "stdout", "stderr"} {
		if strings.Contains(renderedState, forbidden) {
			t.Fatalf("state summary leaked diagnostic text marker %q: %s", forbidden, renderedState)
		}
	}
}

func TestClassifyResultRejectsObservedAttemptWithoutTimestamp(t *testing.T) {
	_, command := resultFixture(t)
	_, err := ClassifyResult(ResultInput{
		Subject:               command.Subject(),
		RouteRequest:          command.RouteRequest(),
		Attempt:               ObservedAttempt(subprocess.CommandAttemptResult{}, AttemptReasonNone),
		RequiredPostcondition: RequireRelationPostcondition(RelationPostconditionExact),
	})
	if err == nil || !strings.Contains(err.Error(), string(ResultReasonAttemptTimestampMissing)) {
		t.Fatalf("error = %v, want attempted_at diagnostic", err)
	}
}

type resultFixtureData struct {
	subjectID topology.SubjectID
	subject   realization.DelegatedRelation
}

type resultIdentity struct {
	subject topology.SubjectID
	route   realizationdelegate.Request
}

func (identity resultIdentity) Subject() topology.SubjectID {
	return identity.subject
}

func (identity resultIdentity) RouteRequest() realizationdelegate.Request {
	return identity.route
}

func resultFixture(t *testing.T) (resultFixtureData, resultIdentity) {
	t.Helper()
	subject, subjectID := mustResultCarrierRelation(t, desiredextension.SourceKindMarketplace, "context7@market")
	route, err := realizationdelegate.NewRequest(
		"test.host-route.install",
		"test-host-route-v1",
		"sha256:"+strings.Repeat("a", 64),
	)
	if err != nil {
		t.Fatalf("NewDelegatedRouteRequest returned error: %v", err)
	}
	return resultFixtureData{subjectID: subjectID, subject: subject}, resultIdentity{
		subject: subjectID,
		route:   route,
	}
}

func mustResultCarrierRelation(
	t *testing.T,
	sourceKind desiredextension.SourceKind,
	sourceRef string,
) (realization.DelegatedRelation, topology.SubjectID) {
	t.Helper()
	source, err := desiredextension.NewSourceRef(sourceKind, sourceRef)
	if err != nil {
		t.Fatalf("NewSourceRef returned error: %v", err)
	}
	subjectID, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"claude-code.plugin-carrier",
		"context7",
	)
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	subjectKey, err := hostrelation.NewSubjectKey("context7@market")
	if err != nil {
		t.Fatalf("NewSubjectKey returned error: %v", err)
	}
	managedKey, err := hostrelation.NewManagedInstanceKey("test:" + source.String())
	if err != nil {
		t.Fatalf("NewManagedInstanceKey returned error: %v", err)
	}
	expected, err := hostrelation.NewExpectedRelation(subjectKey, managedKey)
	if err != nil {
		t.Fatalf("NewExpectedRelation returned error: %v", err)
	}
	realization, err := realization.NewDelegatedRelation(realization.DelegatedRelationInput{
		PlacementID:          "test-carrier",
		Target:               target.TargetClaudeCode,
		Scope:                target.ScopeProject,
		SourceNamespace:      source.String(),
		ExpectedRelation:     expected,
		RouteID:              "test.host-route.install",
		RouteContractVersion: "test-host-route-v1",
		CanonicalRequestHash: "sha256:" + strings.Repeat("a", 64),
		VerifiedRelationFields: []string{
			"managed_instance_key",
			"relation_subject_key",
		},
	})
	if err != nil {
		t.Fatalf("NewDelegatedRelation returned error: %v", err)
	}
	relation, ok := realization.DelegatedRelation()
	if !ok {
		t.Fatalf("NewDelegatedRelation returned %q realization", realization.Kind())
	}
	return relation, subjectID
}

func managedKeyForFixture(t *testing.T) string {
	t.Helper()
	fixture, _ := resultFixture(t)
	return string(fixture.subject.ExpectedRelation().ManagedInstanceKey())
}

func managedKeyForDifferentSource(t *testing.T) string {
	t.Helper()
	other, _ := mustResultCarrierRelation(t, desiredextension.SourceKindHostSource, "https://github.com/acme/context7.git")
	return string(other.ExpectedRelation().ManagedInstanceKey())
}

func observationFact(
	t *testing.T,
	fixture resultFixtureData,
	spec observeclaudeplugin.InventorySpec,
) ObservationFact {
	t.Helper()
	inventory, err := observeclaudeplugin.NewInventory(spec)
	if err != nil {
		t.Fatalf("NewInventory returned error: %v", err)
	}
	correlation := observeclaudeplugin.Correlate(fixture.subject, inventory)
	return CurrentObservation(correlation)
}

func successfulAttempt(t *testing.T) AttemptFact {
	t.Helper()
	return attemptFromCommandResult(t, subprocess.CommandResult{
		Started:     true,
		Stdout:      "success",
		HasExitCode: true,
		ExitCode:    0,
	}, subprocess.CommandAttemptRequest{Command: "claude"})
}

func failedAttempt(t *testing.T) AttemptFact {
	t.Helper()
	return attemptFromCommandResult(t, subprocess.CommandResult{
		Started:     true,
		Stderr:      "failed",
		HasExitCode: true,
		ExitCode:    2,
	}, subprocess.CommandAttemptRequest{Command: "claude"})
}

func attemptFromCommandResult(
	t *testing.T,
	raw subprocess.CommandResult,
	request subprocess.CommandAttemptRequest,
) AttemptFact {
	t.Helper()
	result := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Clock: func() time.Time { return time.Unix(123, 0).UTC() },
		LookupEnv: func(name string) (string, bool) {
			if name == "CLAUDE_TOKEN" {
				return "super-secret", true
			}
			return "", false
		},
		Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
			return raw
		},
	}).ExecuteInWorkingDirectory(
		context.Background(),
		request,
		attemptWorkingDirectoryBinder(t),
	)
	return ObservedAttempt(result, AttemptReason(result.Reason()))
}

type attemptWorkingDirectoryBinding struct {
	root string
}

func (binding attemptWorkingDirectoryBinding) Validate() error {
	info, err := os.Stat(binding.root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("test working directory %q is not a directory", binding.root)
	}
	return nil
}

func (binding attemptWorkingDirectoryBinding) OpenDirectory() (*os.File, error) {
	return os.Open(binding.root)
}

func (attemptWorkingDirectoryBinding) Close() error {
	return nil
}

func attemptWorkingDirectoryBinder(t *testing.T) subprocess.WorkingDirectoryBinder {
	t.Helper()
	root := t.TempDir()
	return func() (subprocess.WorkingDirectoryBinding, error) {
		return attemptWorkingDirectoryBinding{root: root}, nil
	}
}

func classifyResult(
	t *testing.T,
	command resultIdentity,
	attempt AttemptFact,
	observation ObservationFact,
) Result {
	t.Helper()
	result, err := ClassifyResult(ResultInput{
		Subject:               command.Subject(),
		RouteRequest:          command.RouteRequest(),
		Attempt:               attempt,
		Observation:           observation,
		RequiredPostcondition: RequireRelationPostcondition(RelationPostconditionExact),
	})
	if err != nil {
		t.Fatalf("ClassifyResult returned error: %v", err)
	}
	return result
}

func mustClaudeRow(
	t *testing.T,
	subjectKey string,
	managedKey string,
	hasManagedKey bool,
) observeclaudeplugin.Row {
	t.Helper()
	row, err := observeclaudeplugin.NewRow(observeclaudeplugin.RowSpec{
		SubjectKey:            subjectKey,
		HasManagedInstanceKey: hasManagedKey,
		ManagedInstanceKey:    managedKey,
		Scope:                 observeclaudeplugin.HostScopeProject,
	})
	if err != nil {
		t.Fatalf("NewRow returned error: %v", err)
	}
	return row
}

func assertSummaries(
	t *testing.T,
	result Result,
	wantObservation observerelation.ObservationSummary,
	wantPostcondition observerelation.PostconditionSummary,
) {
	t.Helper()
	if result.ObservationSummary() != wantObservation {
		t.Fatalf("observation summary = %q, want %q", result.ObservationSummary(), wantObservation)
	}
	if result.PostconditionSummary() != wantPostcondition {
		t.Fatalf("postcondition summary = %q, want %q", result.PostconditionSummary(), wantPostcondition)
	}
}
