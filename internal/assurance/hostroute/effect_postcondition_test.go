package hostroute

import (
	"strings"
	"testing"

	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observepostcondition "github.com/isty2e/daem/internal/assurance/observe/postcondition"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/topology"
)

func TestClassifyResultCoupledEffectStateCrossProduct(t *testing.T) {
	tests := []struct {
		state      observepostcondition.EvidenceState
		wantClass  ResultClass
		wantReason ResultReasonCode
		satisfied  bool
	}{
		{
			state:      observepostcondition.EvidenceSatisfied,
			wantClass:  ResultAttemptedObservedAbsent,
			wantReason: ResultReasonObservedAbsent,
			satisfied:  true,
		},
		{
			state:      observepostcondition.EvidenceUnsatisfied,
			wantClass:  ResultAttemptedUnverified,
			wantReason: ResultReasonEffectPostconditionUnsatisfied,
		},
		{
			state:      observepostcondition.EvidenceUnavailable,
			wantClass:  ResultAttemptedUnverified,
			wantReason: ResultReasonEffectPostconditionUnavailable,
		},
		{
			state:      observepostcondition.EvidenceStale,
			wantClass:  ResultAttemptedUnverified,
			wantReason: ResultReasonEffectPostconditionStale,
		},
		{
			state:      observepostcondition.EvidenceMalformed,
			wantClass:  ResultAttemptedUnverified,
			wantReason: ResultReasonEffectPostconditionMalformed,
		},
		{
			state:      observepostcondition.EvidenceUnsafe,
			wantClass:  ResultAttemptedUnverified,
			wantReason: ResultReasonEffectPostconditionUnsafe,
		},
		{
			state:      observepostcondition.EvidenceContradictory,
			wantClass:  ResultAttemptedUnverified,
			wantReason: ResultReasonEffectPostconditionContradictory,
		},
	}
	for _, test := range tests {
		t.Run(string(test.state), func(t *testing.T) {
			fixture, command := resultFixture(t)
			result := classifyCoupledRemovalResult(
				t,
				fixture,
				command,
				successfulAttempt(t),
				effectEvidence(t, command, test.state),
			)

			if result.Class() != test.wantClass ||
				result.StateSummary().Reason() != test.wantReason {
				t.Fatalf(
					"class/reason = %q/%q, want %q/%q",
					result.Class(),
					result.StateSummary().Reason(),
					test.wantClass,
					test.wantReason,
				)
			}
			if result.PostconditionsSatisfied() != test.satisfied {
				t.Fatalf(
					"PostconditionsSatisfied = %t, want %t",
					result.PostconditionsSatisfied(),
					test.satisfied,
				)
			}
			if result.PostconditionSummary() != observerelation.PostconditionObserved {
				t.Fatalf(
					"relation postcondition = %q, want %q",
					result.PostconditionSummary(),
					observerelation.PostconditionObserved,
				)
			}
		})
	}
}

func TestClassifyResultCoupledEffectRequiresExactCurrentEvidence(t *testing.T) {
	fixture, command := resultFixture(t)
	foreignSubject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"claude-code.plugin-carrier",
		"other",
	)
	if err != nil {
		t.Fatal(err)
	}
	foreignRoute, err := realizationdelegate.NewRequest(
		"test.host-route.remove",
		"test-host-route-v1",
		"sha256:"+strings.Repeat("b", 64),
	)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		observation ObservationFact
		wantReason  ResultReasonCode
	}{
		{
			name: "missing effect evidence",
			observation: missingRelationObservation(
				t,
				fixture,
			),
			wantReason: ResultReasonEffectPostconditionMissing,
		},
		{
			name: "foreign subject",
			observation: coupledObservation(
				t,
				fixture,
				effectEvidenceFor(
					t,
					foreignSubject,
					command.RouteRequest(),
					observepostcondition.EvidenceSatisfied,
				),
			),
			wantReason: ResultReasonEffectPostconditionForeign,
		},
		{
			name: "foreign route",
			observation: coupledObservation(
				t,
				fixture,
				effectEvidenceFor(
					t,
					command.Subject(),
					foreignRoute,
					observepostcondition.EvidenceSatisfied,
				),
			),
			wantReason: ResultReasonEffectPostconditionForeign,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := ClassifyResult(ResultInput{
				Subject:      command.Subject(),
				RouteRequest: command.RouteRequest(),
				Attempt:      successfulAttempt(t),
				Observation:  test.observation,
				RequiredPostcondition: RequirePostconditions(
					RelationPostconditionAbsent,
					carrierArtifactAbsenceRequirement(t),
				),
			})
			if err != nil {
				t.Fatalf("ClassifyResult returned error: %v", err)
			}
			if result.Class() != ResultAttemptedUnverified ||
				result.StateSummary().Reason() != test.wantReason ||
				result.PostconditionsSatisfied() {
				t.Fatalf(
					"result = %q/%q satisfied=%t",
					result.Class(),
					result.StateSummary().Reason(),
					result.PostconditionsSatisfied(),
				)
			}
		})
	}
}

func TestClassifyResultKeepsAttemptAndPostconditionAxesOrthogonal(t *testing.T) {
	fixture, command := resultFixture(t)
	result := classifyCoupledRemovalResult(
		t,
		fixture,
		command,
		failedAttempt(t),
		effectEvidence(t, command, observepostcondition.EvidenceSatisfied),
	)

	if result.Class() != ResultFailed ||
		result.StateSummary().Reason() != ResultReasonNonZeroExit {
		t.Fatalf(
			"class/reason = %q/%q, want failed/nonzero_exit",
			result.Class(),
			result.StateSummary().Reason(),
		)
	}
	if !result.PostconditionsSatisfied() {
		t.Fatal("freshly satisfied postconditions were hidden by mechanical failure")
	}
}

func TestVerifyCurrentPostconditionsDoesNotRequireSyntheticAttempt(t *testing.T) {
	fixture, command := resultFixture(t)
	requirement := RequirePostconditions(
		RelationPostconditionAbsent,
		carrierArtifactAbsenceRequirement(t),
	)
	tests := []struct {
		name        string
		observation ObservationFact
		want        bool
		wantReason  ResultReasonCode
	}{
		{
			name: "all current facts satisfied",
			observation: coupledObservation(
				t,
				fixture,
				effectEvidence(t, command, observepostcondition.EvidenceSatisfied),
			),
			want:       true,
			wantReason: ResultReasonObservedAbsent,
		},
		{
			name:        "effect evidence missing",
			observation: missingRelationObservation(t, fixture),
			wantReason:  ResultReasonEffectPostconditionMissing,
		},
		{
			name: "effect unsatisfied",
			observation: coupledObservation(
				t,
				fixture,
				effectEvidence(t, command, observepostcondition.EvidenceUnsatisfied),
			),
			wantReason: ResultReasonEffectPostconditionUnsatisfied,
		},
		{
			name: "observation unavailable",
			observation: ObservationUnavailable(
				ResultReasonObservationUnavailable,
			),
			wantReason: ResultReasonObservationUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verification, err := VerifyCurrentPostconditions(CurrentVerificationInput{
				Subject:               command.Subject(),
				RouteRequest:          command.RouteRequest(),
				Observation:           test.observation,
				RequiredPostcondition: requirement,
			})
			if err != nil {
				t.Fatalf("VerifyCurrentPostconditions returned error: %v", err)
			}
			if verification.Satisfied() != test.want ||
				verification.Reason() != test.wantReason {
				t.Fatalf(
					"verification = satisfied:%t reason:%q, want %t/%q",
					verification.Satisfied(),
					verification.Reason(),
					test.want,
					test.wantReason,
				)
			}
		})
	}
}

func TestClassifyResultRejectsImplicitPostconditionContract(t *testing.T) {
	fixture, command := resultFixture(t)
	_, err := ClassifyResult(ResultInput{
		Subject:      command.Subject(),
		RouteRequest: command.RouteRequest(),
		Attempt:      successfulAttempt(t),
		Observation:  missingRelationObservation(t, fixture),
	})
	if err == nil || !strings.Contains(err.Error(), "postcondition requirement is required") {
		t.Fatalf("ClassifyResult error = %v, want explicit postcondition diagnostic", err)
	}
}

func classifyCoupledRemovalResult(
	t *testing.T,
	fixture resultFixtureData,
	command resultIdentity,
	attempt AttemptFact,
	evidence observepostcondition.Set,
) Result {
	t.Helper()
	result, err := ClassifyResult(ResultInput{
		Subject:      command.Subject(),
		RouteRequest: command.RouteRequest(),
		Attempt:      attempt,
		Observation:  coupledObservation(t, fixture, evidence),
		RequiredPostcondition: RequirePostconditions(
			RelationPostconditionAbsent,
			carrierArtifactAbsenceRequirement(t),
		),
	})
	if err != nil {
		t.Fatalf("ClassifyResult returned error: %v", err)
	}
	return result
}

func coupledObservation(
	t *testing.T,
	fixture resultFixtureData,
	evidence observepostcondition.Set,
) ObservationFact {
	t.Helper()
	relation := missingRelationObservation(t, fixture)
	correlation, ok := relation.Correlation()
	if !ok {
		t.Fatal("missing relation fixture did not produce a correlation")
	}
	return CurrentObservationWithEffectEvidence(correlation, evidence)
}

func missingRelationObservation(
	t *testing.T,
	fixture resultFixtureData,
) ObservationFact {
	t.Helper()
	return observationFact(t, fixture, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
}

func effectEvidence(
	t *testing.T,
	command resultIdentity,
	state observepostcondition.EvidenceState,
) observepostcondition.Set {
	t.Helper()
	return effectEvidenceFor(t, command.Subject(), command.RouteRequest(), state)
}

func effectEvidenceFor(
	t *testing.T,
	subject topology.SubjectID,
	route realizationdelegate.Request,
	state observepostcondition.EvidenceState,
) observepostcondition.Set {
	t.Helper()
	evidence, err := observepostcondition.NewEvidence(
		effectpostcondition.CarrierArtifactsAbsent,
		state,
	)
	if err != nil {
		t.Fatal(err)
	}
	set, err := observepostcondition.NewSet(observepostcondition.SetInput{
		Subject:      subject,
		RouteRequest: route,
		Evidence:     []observepostcondition.Evidence{evidence},
	})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func carrierArtifactAbsenceRequirement(t *testing.T) effectpostcondition.Set {
	t.Helper()
	requirements, err := effectpostcondition.NewSet(
		[]effectpostcondition.Requirement{
			effectpostcondition.CarrierArtifactsAbsent,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return requirements
}
