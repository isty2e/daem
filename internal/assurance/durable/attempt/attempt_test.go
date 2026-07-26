package attempt

import (
	"strings"
	"testing"
	"time"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	assurancepostcondition "github.com/isty2e/daem/internal/assurance/postcondition"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestAttemptHistoryRejectsTimestampsOutsideDurableRange(t *testing.T) {
	projection, err := topology.NewSubjectID(topology.SubjectProjection, "mcp-server", "context7")
	if err != nil {
		t.Fatal(err)
	}
	relation, err := topology.NewSubjectID(topology.SubjectHostRelation, "plugin-carrier", "context7")
	if err != nil {
		t.Fatal(err)
	}
	invalid := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)

	_, delegateErr := NewDelegateAttempt(DelegateAttemptInput{
		Subject:         projection,
		Target:          target.TargetClaudeCode,
		Scope:           target.ScopeProject,
		PlanIdentityKey: "plan",
		ObservedAt:      invalid,
		Status:          DelegateStatusSucceeded,
		Reason:          DelegateReasonNone,
	})
	if delegateErr == nil || !strings.Contains(delegateErr.Error(), "outside the durable RFC3339Nano range") {
		t.Fatalf("NewDelegateAttempt error = %v, want durable timestamp rejection", delegateErr)
	}

	_, routeErr := NewHostRouteAttempt(HostRouteAttemptInput{
		Subject:          relation,
		Target:           target.TargetClaudeCode,
		Scope:            target.ScopeProject,
		Operation:        lock.OperationInstall,
		RouteID:          "claude-code.plugin-carrier.install",
		RouteRequestHash: testRouteRequestHash,
		ObservedAt:       invalid,
		ResultClass:      HostRouteResultAttemptedUnverified,
		Reason:           HostRouteReasonObservationUnavailable,
		AttemptObserved:  true,
		Observation:      observerelation.ObservationNotObserved,
		Postcondition:    observerelation.PostconditionUnknown,
	})
	if routeErr == nil || !strings.Contains(routeErr.Error(), "outside the durable RFC3339Nano range") {
		t.Fatalf("NewHostRouteAttempt error = %v, want durable timestamp rejection", routeErr)
	}
}

func TestAttemptHistoryRejectsMalformedCorrelationIdentities(t *testing.T) {
	delegateInput := testDelegateAttemptInput(t, time.Now())
	delegateInput.PlanIdentityKey = "delegate:\u202ev1"
	if _, err := NewDelegateAttempt(delegateInput); err == nil ||
		!strings.Contains(err.Error(), "control character") {
		t.Fatalf("NewDelegateAttempt error = %v, want unsafe identity rejection", err)
	}

	hostRouteInput := testHostRouteAttemptInput(t, time.Now())
	hostRouteInput.RouteID = "claude-code.plugin-carrier.\x00install"
	if _, err := NewHostRouteAttempt(hostRouteInput); err == nil ||
		!strings.Contains(err.Error(), "control character") {
		t.Fatalf("NewHostRouteAttempt route error = %v, want unsafe identity rejection", err)
	}

	hostRouteInput = testHostRouteAttemptInput(t, time.Now())
	hostRouteInput.RouteRequestHash = "sha256:short"
	if _, err := NewHostRouteAttempt(hostRouteInput); err == nil ||
		!strings.Contains(err.Error(), "SHA-256 digest") {
		t.Fatalf("NewHostRouteAttempt hash error = %v, want digest rejection", err)
	}

	hostRouteInput = testHostRouteAttemptInput(t, time.Now())
	hostRouteInput.Operation = "upgrade"
	if _, err := NewHostRouteAttempt(hostRouteInput); err == nil ||
		!strings.Contains(err.Error(), "operation kind") {
		t.Fatalf("NewHostRouteAttempt operation error = %v, want closed operation rejection", err)
	}
}

func TestAttemptHistoryUsesCanonicalRelationSummaries(t *testing.T) {
	delegateInput := testDelegateAttemptInput(t, time.Now())
	delegateInput.Observation = ""
	delegateInput.Postcondition = ""
	delegateAttempt, err := NewDelegateAttempt(delegateInput)
	if err != nil {
		t.Fatalf("NewDelegateAttempt with omitted summaries returned error: %v", err)
	}
	if delegateAttempt.ObservationSummary() != observerelation.ObservationNotObserved ||
		delegateAttempt.PostconditionSummary() != observerelation.PostconditionNotObserved {
		t.Fatalf(
			"delegate summaries = %q/%q, want canonical omitted summaries",
			delegateAttempt.ObservationSummary(),
			delegateAttempt.PostconditionSummary(),
		)
	}

	hostRouteInput := testHostRouteAttemptInput(t, time.Now())
	hostRouteInput.Observation = ""
	hostRouteInput.Postcondition = ""
	hostRouteAttempt, err := NewHostRouteAttempt(hostRouteInput)
	if err != nil {
		t.Fatalf("NewHostRouteAttempt with omitted summaries returned error: %v", err)
	}
	if hostRouteAttempt.ObservationSummary() != observerelation.ObservationNotObserved ||
		hostRouteAttempt.PostconditionSummary() != observerelation.PostconditionNotObserved {
		t.Fatalf(
			"host-route summaries = %q/%q, want canonical omitted summaries",
			hostRouteAttempt.ObservationSummary(),
			hostRouteAttempt.PostconditionSummary(),
		)
	}

	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{
			name: "delegate observation",
			run: func() error {
				input := testDelegateAttemptInput(t, time.Now())
				input.Observation = observerelation.ObservationSummary("ready")
				_, err := NewDelegateAttempt(input)
				return err
			},
			want: "relation observation summary",
		},
		{
			name: "delegate postcondition",
			run: func() error {
				input := testDelegateAttemptInput(t, time.Now())
				input.Postcondition = observerelation.PostconditionSummary("installed")
				_, err := NewDelegateAttempt(input)
				return err
			},
			want: "relation postcondition summary",
		},
		{
			name: "host-route observation",
			run: func() error {
				input := testHostRouteAttemptInput(t, time.Now())
				input.Observation = observerelation.ObservationSummary("ready")
				_, err := NewHostRouteAttempt(input)
				return err
			},
			want: "relation observation summary",
		},
		{
			name: "host-route postcondition",
			run: func() error {
				input := testHostRouteAttemptInput(t, time.Now())
				input.Postcondition = observerelation.PostconditionSummary("installed")
				_, err := NewHostRouteAttempt(input)
				return err
			},
			want: "relation postcondition summary",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("constructor error = %v, want %q rejection", err, test.want)
			}
		})
	}
}

func TestDelegateAttemptRejectsContradictoryProcessFacts(t *testing.T) {
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "mcp-server", "context7")
	if err != nil {
		t.Fatal(err)
	}
	base := DelegateAttemptInput{
		Subject:         subject,
		Target:          target.TargetClaudeCode,
		Scope:           target.ScopeProject,
		PlanIdentityKey: "plan",
		ObservedAt:      time.Now(),
		Status:          DelegateStatusFailed,
		Reason:          DelegateReasonTimeout,
	}
	tests := []struct {
		name   string
		mutate func(*DelegateAttemptInput)
		want   string
	}{
		{
			name: "timeout reason without timeout fact",
			want: "timeout reason requires timed_out",
		},
		{
			name: "non-timeout reason with timeout fact",
			mutate: func(input *DelegateAttemptInput) {
				input.Reason = DelegateReasonRunnerError
				input.TimedOut = true
			},
			want: "timed_out requires timeout reason",
		},
		{
			name: "succeeded with nonzero exit",
			mutate: func(input *DelegateAttemptInput) {
				exitCode := 17
				input.Status = DelegateStatusSucceeded
				input.Reason = DelegateReasonNone
				input.ExitCode = &exitCode
			},
			want: "succeeded delegate attempt cannot record nonzero exit code",
		},
		{
			name: "nonzero reason without exit code",
			mutate: func(input *DelegateAttemptInput) {
				input.Reason = DelegateReasonNonZeroExit
			},
			want: "nonzero_exit reason requires a nonzero exit code",
		},
		{
			name: "policy block with process exit",
			mutate: func(input *DelegateAttemptInput) {
				exitCode := 0
				input.Status = DelegateStatusBlocked
				input.Reason = DelegateReasonPolicyBlocked
				input.ExitCode = &exitCode
			},
			want: "policy_blocked cannot record process facts",
		},
		{
			name: "missing environment with truncated process output",
			mutate: func(input *DelegateAttemptInput) {
				input.Reason = DelegateReasonMissingEnvRef
				input.StdoutTruncated = true
			},
			want: "missing_env_ref cannot record process facts",
		},
		{
			name: "missing runner with process exit",
			mutate: func(input *DelegateAttemptInput) {
				exitCode := 127
				input.Reason = DelegateReasonMissingRunner
				input.ExitCode = &exitCode
			},
			want: "missing_runner cannot record process facts",
		},
		{
			name: "runner error with nonzero exit",
			mutate: func(input *DelegateAttemptInput) {
				exitCode := 23
				input.Reason = DelegateReasonRunnerError
				input.ExitCode = &exitCode
			},
			want: "runner_error cannot record a nonzero exit code",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			if test.mutate != nil {
				test.mutate(&input)
			}
			if _, err := NewDelegateAttempt(input); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewDelegateAttempt error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDelegateAttemptWorkDirAuthorityRetainsObservedProcessFacts(t *testing.T) {
	exitCode := 0
	input := testDelegateAttemptInput(t, time.Now())
	input.Status = DelegateStatusFailed
	input.Reason = DelegateReasonWorkDirAuthority
	input.ExitCode = &exitCode

	attempt, err := NewDelegateAttempt(input)
	if err != nil {
		t.Fatalf("NewDelegateAttempt returned error: %v", err)
	}
	if observedExit, ok := attempt.ExitCode(); !ok || observedExit != 0 {
		t.Fatalf("attempt = %#v, want retained post-start process facts", attempt)
	}
}

func TestHostRouteAttemptRejectsContradictoryProcessFacts(t *testing.T) {
	subject, err := topology.NewSubjectID(topology.SubjectHostRelation, "plugin-carrier", "context7")
	if err != nil {
		t.Fatal(err)
	}
	base := HostRouteAttemptInput{
		Subject:          subject,
		Target:           target.TargetClaudeCode,
		Scope:            target.ScopeProject,
		Operation:        lock.OperationInstall,
		RouteID:          "claude-code.plugin-carrier.install",
		RouteRequestHash: testRouteRequestHash,
		ObservedAt:       time.Now(),
		ResultClass:      HostRouteResultAttemptedUnverified,
		Reason:           HostRouteReasonObservationUnavailable,
		Observation:      observerelation.ObservationNotObserved,
		Postcondition:    observerelation.PostconditionUnknown,
	}
	exitCode := 17
	tests := []struct {
		name   string
		mutate func(*HostRouteAttemptInput)
		want   string
	}{
		{
			name: "unobserved attempt with exit code",
			mutate: func(input *HostRouteAttemptInput) {
				input.ExitCode = &exitCode
			},
			want: "unobserved host route attempt cannot record process facts",
		},
		{
			name: "unobserved attempt with reason",
			mutate: func(input *HostRouteAttemptInput) {
				input.AttemptReason = HostRouteAttemptReasonRunnerError
			},
			want: "unobserved host route attempt cannot record process facts",
		},
		{
			name: "timeout reason without timeout fact",
			mutate: func(input *HostRouteAttemptInput) {
				input.AttemptObserved = true
				input.AttemptReason = HostRouteAttemptReasonTimeout
			},
			want: "timeout attempt reason requires timed_out",
		},
		{
			name: "non-timeout reason with timeout fact",
			mutate: func(input *HostRouteAttemptInput) {
				input.AttemptObserved = true
				input.AttemptReason = HostRouteAttemptReasonRunnerError
				input.TimedOut = true
			},
			want: "timed_out requires timeout attempt reason",
		},
		{
			name: "nonzero reason without exit code",
			mutate: func(input *HostRouteAttemptInput) {
				input.AttemptObserved = true
				input.AttemptReason = HostRouteAttemptReasonNonZeroExit
			},
			want: "nonzero_exit attempt reason requires a nonzero exit code",
		},
		{
			name: "timeout result without timeout attempt reason",
			mutate: func(input *HostRouteAttemptInput) {
				input.AttemptObserved = true
				input.ResultClass = HostRouteResultFailed
				input.Reason = HostRouteReasonTimeout
			},
			want: "timeout classification requires matching attempt reason",
		},
		{
			name: "successful classification with failure attempt reason",
			mutate: func(input *HostRouteAttemptInput) {
				input.AttemptObserved = true
				input.AttemptReason = HostRouteAttemptReasonRunnerError
			},
			want: "cannot carry failure attempt reason",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.mutate(&input)
			if _, err := NewHostRouteAttempt(input); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewHostRouteAttempt error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestHostRouteAttemptRejectsContradictoryClassificationFacts(t *testing.T) {
	tests := []struct {
		name            string
		class           HostRouteResultClass
		reason          HostRouteResultReason
		attemptObserved bool
	}{
		{
			name:            "present class with unsupported scope reason",
			class:           HostRouteResultAttemptedObservedPresent,
			reason:          HostRouteReasonUnsupportedScope,
			attemptObserved: true,
		},
		{
			name:            "unsupported source class with unsupported scope reason",
			class:           HostRouteResultUnsupportedSource,
			reason:          HostRouteReasonUnsupportedScope,
			attemptObserved: false,
		},
		{
			name:            "preflight class with current attempt",
			class:           HostRouteResultBlockedPreflight,
			reason:          HostRouteReasonPreflightFailed,
			attemptObserved: true,
		},
		{
			name:            "attempted present without current attempt",
			class:           HostRouteResultAttemptedObservedPresent,
			reason:          HostRouteReasonObservedPresent,
			attemptObserved: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := testHostRouteAttemptInput(t, time.Now())
			input.ResultClass = test.class
			input.Reason = test.reason
			input.AttemptObserved = test.attemptObserved
			if _, err := NewHostRouteAttempt(input); err == nil ||
				!strings.Contains(err.Error(), "classification") {
				t.Fatalf("NewHostRouteAttempt error = %v, want classification rejection", err)
			}
		})
	}
}

func TestHostRouteAttemptRejectsRetiredUnreachableClassifications(t *testing.T) {
	tests := []struct {
		name   string
		class  HostRouteResultClass
		reason HostRouteResultReason
		want   string
	}{
		{
			name:   "history only class",
			class:  "history_only",
			reason: HostRouteReasonObservedPresent,
			want:   "unsupported host route result class",
		},
		{
			name:   "unsupported class",
			class:  "unsupported",
			reason: HostRouteReasonObservedPresent,
			want:   "unsupported host route result class",
		},
		{
			name:   "prior attempt only reason",
			class:  HostRouteResultAttemptedObservedPresent,
			reason: "prior_attempt_only",
			want:   "unsupported host route result reason",
		},
		{
			name:   "attempt missing reason",
			class:  HostRouteResultAttemptedObservedPresent,
			reason: "attempt_missing",
			want:   "unsupported host route result reason",
		},
		{
			name:   "unsupported observation reason",
			class:  HostRouteResultAttemptedObservedPresent,
			reason: "unsupported_observation",
			want:   "unsupported host route result reason",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := testHostRouteAttemptInput(t, time.Now())
			input.ResultClass = test.class
			input.Reason = test.reason
			if _, err := NewHostRouteAttempt(input); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewHostRouteAttempt error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestHostRouteAttemptCorrelatesExactEffectPostconditionSummaries(t *testing.T) {
	unavailable := testEffectSummarySet(
		t,
		effectpostcondition.CarrierArtifactsAbsent,
		assurancepostcondition.SummaryUnavailable,
	)
	input := testHostRouteAttemptInput(t, time.Now())
	input.ResultClass = HostRouteResultAttemptedUnverified
	input.Reason = HostRouteReasonEffectUnavailable
	input.EffectPostconditions = unavailable

	attempt, err := NewHostRouteAttempt(input)
	if err != nil {
		t.Fatalf("NewHostRouteAttempt returned error: %v", err)
	}
	if !attempt.EffectPostconditions().Equal(unavailable) {
		t.Fatal("attempt did not retain exact effect postcondition summaries")
	}

	input.EffectPostconditions = testEffectSummarySet(
		t,
		effectpostcondition.CarrierArtifactsAbsent,
		assurancepostcondition.SummaryStale,
	)
	if _, err := NewHostRouteAttempt(input); err == nil ||
		!strings.Contains(err.Error(), "requires a matching effect postcondition summary") {
		t.Fatalf("mismatched effect reason error = %v", err)
	}

	input = testHostRouteAttemptInput(t, time.Now())
	input.ResultClass = HostRouteResultAttemptedObservedAbsent
	input.Reason = HostRouteReasonObservedAbsent
	input.Observation = observerelation.ObservationMissing
	input.Postcondition = observerelation.PostconditionMissing
	input.EffectPostconditions = testEffectSummarySet(
		t,
		effectpostcondition.CarrierArtifactsAbsent,
		assurancepostcondition.SummaryUnsatisfied,
	)
	if _, err := NewHostRouteAttempt(input); err == nil ||
		!strings.Contains(err.Error(), "verified host route classification") {
		t.Fatalf("verified classification with unsatisfied effect error = %v", err)
	}
}

func TestHostRouteAttemptEqualityIncludesEffectPostconditionSummaries(t *testing.T) {
	input := testHostRouteAttemptInput(t, time.Now())
	input.EffectPostconditions = testEffectSummarySet(
		t,
		effectpostcondition.CarrierArtifactsAbsent,
		assurancepostcondition.SummarySatisfied,
	)
	left, err := NewHostRouteAttempt(input)
	if err != nil {
		t.Fatal(err)
	}

	input.EffectPostconditions = testEffectSummarySet(
		t,
		effectpostcondition.CarrierArtifactsAbsent,
		assurancepostcondition.SummaryUnavailable,
	)
	right, err := NewHostRouteAttempt(input)
	if err != nil {
		t.Fatal(err)
	}
	if left.Equal(right) {
		t.Fatal("HostRouteAttempt.Equal ignored effect postcondition summaries")
	}
}

func testEffectSummarySet(
	t *testing.T,
	requirement effectpostcondition.Requirement,
	state assurancepostcondition.SummaryState,
) assurancepostcondition.SummarySet {
	t.Helper()
	summary, err := assurancepostcondition.NewSummary(requirement, state)
	if err != nil {
		t.Fatal(err)
	}
	set, err := assurancepostcondition.NewSummarySet(
		[]assurancepostcondition.Summary{summary},
	)
	if err != nil {
		t.Fatal(err)
	}
	return set
}
