package postcondition

import (
	"strings"
	"testing"

	assurancepostcondition "github.com/isty2e/daem/internal/assurance/postcondition"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/topology"
)

func TestAssessEffectPostconditionStateCrossProduct(t *testing.T) {
	subject := testSubject(t, "plugin")
	request := testRequest(t, "remove")
	requirements := testRequirements(t)
	tests := []struct {
		state       EvidenceState
		wantClass   AssessmentClass
		wantReason  AssessmentReason
		wantSummary assurancepostcondition.SummaryState
	}{
		{
			state:       EvidenceSatisfied,
			wantClass:   AssessmentSatisfied,
			wantSummary: assurancepostcondition.SummarySatisfied,
		},
		{
			state:       EvidenceUnsatisfied,
			wantClass:   AssessmentUnsatisfied,
			wantReason:  AssessmentReasonUnsatisfied,
			wantSummary: assurancepostcondition.SummaryUnsatisfied,
		},
		{
			state:       EvidenceUnavailable,
			wantClass:   AssessmentIndeterminate,
			wantReason:  AssessmentReasonUnavailable,
			wantSummary: assurancepostcondition.SummaryUnavailable,
		},
		{
			state:       EvidenceStale,
			wantClass:   AssessmentIndeterminate,
			wantReason:  AssessmentReasonStale,
			wantSummary: assurancepostcondition.SummaryStale,
		},
		{
			state:       EvidenceMalformed,
			wantClass:   AssessmentIndeterminate,
			wantReason:  AssessmentReasonMalformed,
			wantSummary: assurancepostcondition.SummaryMalformed,
		},
		{
			state:       EvidenceUnsafe,
			wantClass:   AssessmentIndeterminate,
			wantReason:  AssessmentReasonUnsafe,
			wantSummary: assurancepostcondition.SummaryUnsafe,
		},
		{
			state:       EvidenceContradictory,
			wantClass:   AssessmentIndeterminate,
			wantReason:  AssessmentReasonContradictory,
			wantSummary: assurancepostcondition.SummaryContradictory,
		},
	}
	for _, test := range tests {
		t.Run(string(test.state), func(t *testing.T) {
			evidence := testEvidenceSet(t, subject, request, test.state)
			assessment, err := Assess(AssessmentInput{
				Subject:         subject,
				RouteRequest:    request,
				Requirements:    requirements,
				Evidence:        evidence,
				EvidencePresent: true,
			})
			if err != nil {
				t.Fatalf("Assess returned error: %v", err)
			}
			if assessment.class != test.wantClass ||
				assessment.Reason() != test.wantReason {
				t.Fatalf(
					"class/reason = %q/%q, want %q/%q",
					assessment.class,
					assessment.Reason(),
					test.wantClass,
					test.wantReason,
				)
			}
			summaries := assessment.SummarySet().Summaries()
			if len(summaries) != 1 ||
				summaries[0].Requirement() != effectpostcondition.CarrierArtifactsAbsent ||
				summaries[0].State() != test.wantSummary {
				t.Fatalf("summaries = %#v", summaries)
			}
			if assessment.Satisfied() != (test.wantClass == AssessmentSatisfied) {
				t.Fatalf("Satisfied = %t", assessment.Satisfied())
			}
		})
	}
}

func TestAssessRequiresExactIdentityAndCompleteEvidence(t *testing.T) {
	subject := testSubject(t, "plugin")
	request := testRequest(t, "remove")
	requirements := testRequirements(t)
	tests := []struct {
		name            string
		evidence        Set
		evidencePresent bool
		wantReason      AssessmentReason
	}{
		{
			name:       "missing evidence set",
			wantReason: AssessmentReasonMissing,
		},
		{
			name: "empty evidence set",
			evidence: testEmptyEvidenceSet(
				t,
				subject,
				request,
			),
			evidencePresent: true,
			wantReason:      AssessmentReasonMissing,
		},
		{
			name: "foreign subject",
			evidence: testEvidenceSet(
				t,
				testSubject(t, "other"),
				request,
				EvidenceSatisfied,
			),
			evidencePresent: true,
			wantReason:      AssessmentReasonForeign,
		},
		{
			name: "foreign route",
			evidence: testEvidenceSet(
				t,
				subject,
				testRequest(t, "other"),
				EvidenceSatisfied,
			),
			evidencePresent: true,
			wantReason:      AssessmentReasonForeign,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assessment, err := Assess(AssessmentInput{
				Subject:         subject,
				RouteRequest:    request,
				Requirements:    requirements,
				Evidence:        test.evidence,
				EvidencePresent: test.evidencePresent,
			})
			if err != nil {
				t.Fatalf("Assess returned error: %v", err)
			}
			if assessment.class != AssessmentIndeterminate ||
				assessment.Reason() != test.wantReason ||
				assessment.Satisfied() {
				t.Fatalf(
					"assessment = %q/%q satisfied=%t",
					assessment.class,
					assessment.Reason(),
					assessment.Satisfied(),
				)
			}
		})
	}
}

func TestAssessExplicitRelationOnlyRequirementNeedsNoEffectEvidence(t *testing.T) {
	subject := testSubject(t, "plugin")
	request := testRequest(t, "remove")
	assessment, err := Assess(AssessmentInput{
		Subject:      subject,
		RouteRequest: request,
	})
	if err != nil {
		t.Fatalf("Assess returned error: %v", err)
	}
	if assessment.class != AssessmentNotRequired ||
		!assessment.Satisfied() ||
		len(assessment.SummarySet().Summaries()) != 0 {
		t.Fatalf("assessment = %#v", assessment)
	}
}

func TestEvidenceSetRejectsDuplicatesAndDefendsCopies(t *testing.T) {
	subject := testSubject(t, "plugin")
	request := testRequest(t, "remove")
	fact, err := NewEvidence(
		effectpostcondition.CarrierArtifactsAbsent,
		EvidenceSatisfied,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSet(SetInput{
		Subject:      subject,
		RouteRequest: request,
		Evidence:     []Evidence{fact, fact},
	}); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("duplicate NewSet error = %v", err)
	}
	set, err := NewSet(SetInput{
		Subject:      subject,
		RouteRequest: request,
		Evidence:     []Evidence{fact},
	})
	if err != nil {
		t.Fatal(err)
	}
	copied := set.Evidence()
	copied[0] = Evidence{}
	if got := set.Evidence(); len(got) != 1 || got[0] != fact {
		t.Fatalf("caller mutation changed evidence set: %#v", got)
	}
}

func testRequirements(t *testing.T) effectpostcondition.Set {
	t.Helper()
	requirements, err := effectpostcondition.NewSet([]effectpostcondition.Requirement{
		effectpostcondition.CarrierArtifactsAbsent,
	})
	if err != nil {
		t.Fatal(err)
	}
	return requirements
}

func testEvidenceSet(
	t *testing.T,
	subject topology.SubjectID,
	request realizationdelegate.Request,
	state EvidenceState,
) Set {
	t.Helper()
	fact, err := NewEvidence(effectpostcondition.CarrierArtifactsAbsent, state)
	if err != nil {
		t.Fatal(err)
	}
	set, err := NewSet(SetInput{
		Subject:      subject,
		RouteRequest: request,
		Evidence:     []Evidence{fact},
	})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func testEmptyEvidenceSet(
	t *testing.T,
	subject topology.SubjectID,
	request realizationdelegate.Request,
) Set {
	t.Helper()
	set, err := NewSet(SetInput{
		Subject:      subject,
		RouteRequest: request,
	})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func testSubject(t *testing.T, key string) topology.SubjectID {
	t.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"test.plugin-carrier",
		key,
	)
	if err != nil {
		t.Fatal(err)
	}
	return subject
}

func testRequest(t *testing.T, seed string) realizationdelegate.Request {
	t.Helper()
	request, err := realizationdelegate.NewRequest(
		"test.plugin.remove."+seed,
		"test-plugin-remove-v1",
		"sha256:"+strings.Repeat("a", 64),
	)
	if err != nil {
		t.Fatal(err)
	}
	return request
}
