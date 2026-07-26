package postcondition

import (
	"fmt"

	assurancepostcondition "github.com/isty2e/daem/internal/assurance/postcondition"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/topology"
)

// AssessmentClass is the aggregate state of the exact required effect set.
type AssessmentClass string

const (
	AssessmentNotRequired   AssessmentClass = "not_required"
	AssessmentSatisfied     AssessmentClass = "satisfied"
	AssessmentUnsatisfied   AssessmentClass = "unsatisfied"
	AssessmentIndeterminate AssessmentClass = "indeterminate"
)

// AssessmentReason explains an aggregate non-satisfied result.
type AssessmentReason string

const (
	AssessmentReasonNone          AssessmentReason = ""
	AssessmentReasonMissing       AssessmentReason = "required_evidence_missing"
	AssessmentReasonUnsatisfied   AssessmentReason = "required_evidence_unsatisfied"
	AssessmentReasonUnavailable   AssessmentReason = "required_evidence_unavailable"
	AssessmentReasonStale         AssessmentReason = "required_evidence_stale"
	AssessmentReasonMalformed     AssessmentReason = "required_evidence_malformed"
	AssessmentReasonUnsafe        AssessmentReason = "required_evidence_unsafe"
	AssessmentReasonContradictory AssessmentReason = "required_evidence_contradictory"
	AssessmentReasonForeign       AssessmentReason = "foreign_evidence"
)

// AssessmentInput composes one exact locked requirement set and current
// identity-bound evidence. EvidencePresent distinguishes missing observation
// from an observed empty set.
type AssessmentInput struct {
	Subject         topology.SubjectID
	RouteRequest    realizationdelegate.Request
	Requirements    effectpostcondition.Set
	Evidence        Set
	EvidencePresent bool
}

// Assessment is the bounded pure evaluation of coupled effect evidence.
type Assessment struct {
	class     AssessmentClass
	reason    AssessmentReason
	summaries assurancepostcondition.SummarySet
}

// Assess evaluates every exact required effect fact without treating missing
// or foreign evidence as an empty satisfied set.
func Assess(input AssessmentInput) (Assessment, error) {
	if err := input.Subject.Validate(); err != nil {
		return Assessment{}, fmt.Errorf("effect postcondition assessment subject: %w", err)
	}
	if err := input.RouteRequest.Validate(); err != nil {
		return Assessment{}, fmt.Errorf("effect postcondition assessment route: %w", err)
	}
	if err := input.Requirements.Validate(); err != nil {
		return Assessment{}, err
	}
	requirements := input.Requirements.Requirements()
	if len(requirements) == 0 {
		if input.EvidencePresent {
			if err := input.Evidence.Validate(); err != nil {
				return Assessment{}, err
			}
			if input.Evidence.Subject() != input.Subject ||
				!input.Evidence.RouteRequest().Equal(input.RouteRequest) ||
				len(input.Evidence.Evidence()) != 0 {
				return Assessment{
					class:  AssessmentIndeterminate,
					reason: AssessmentReasonForeign,
				}, nil
			}
		}
		return Assessment{class: AssessmentNotRequired}, nil
	}

	summaries := notObservedSummaries(requirements)
	if !input.EvidencePresent {
		return Assessment{
			class:     AssessmentIndeterminate,
			reason:    AssessmentReasonMissing,
			summaries: summaries,
		}, nil
	}
	if err := input.Evidence.Validate(); err != nil {
		return Assessment{}, err
	}
	if input.Evidence.Subject() != input.Subject ||
		!input.Evidence.RouteRequest().Equal(input.RouteRequest) {
		return Assessment{
			class:     AssessmentIndeterminate,
			reason:    AssessmentReasonForeign,
			summaries: summaries,
		}, nil
	}

	facts := input.Evidence.Evidence()
	summaryValues := summaries.Summaries()
	required := make(map[effectpostcondition.Requirement]int, len(requirements))
	for index, requirement := range requirements {
		required[requirement] = index
	}
	for _, fact := range facts {
		index, ok := required[fact.Requirement()]
		if !ok {
			return Assessment{
				class:     AssessmentIndeterminate,
				reason:    AssessmentReasonForeign,
				summaries: summaries,
			}, nil
		}
		summaryValues[index] = summarize(fact)
	}
	summaries = mustSummarySet(summaryValues)

	for index, requirement := range requirements {
		fact, present := evidenceFor(facts, requirement)
		if !present {
			return Assessment{
				class:     AssessmentIndeterminate,
				reason:    AssessmentReasonMissing,
				summaries: summaries,
			}, nil
		}
		if reason := indeterminateReason(fact.State()); reason != AssessmentReasonNone {
			return Assessment{
				class:     AssessmentIndeterminate,
				reason:    reason,
				summaries: summaries,
			}, nil
		}
		if summaryValues[index].State() == assurancepostcondition.SummaryUnsatisfied {
			return Assessment{
				class:     AssessmentUnsatisfied,
				reason:    AssessmentReasonUnsatisfied,
				summaries: summaries,
			}, nil
		}
	}
	return Assessment{
		class:     AssessmentSatisfied,
		summaries: summaries,
	}, nil
}

// Reason returns the bounded aggregate explanation.
func (assessment Assessment) Reason() AssessmentReason {
	return assessment.reason
}

// SummarySet returns the canonical immutable history-safe summaries.
func (assessment Assessment) SummarySet() assurancepostcondition.SummarySet {
	return assessment.summaries
}

// Satisfied reports whether all required effects are freshly satisfied, or no
// coupled effect was required.
func (assessment Assessment) Satisfied() bool {
	return assessment.class == AssessmentNotRequired ||
		assessment.class == AssessmentSatisfied
}

func notObservedSummaries(
	requirements []effectpostcondition.Requirement,
) assurancepostcondition.SummarySet {
	summaries := make([]assurancepostcondition.Summary, len(requirements))
	for index, requirement := range requirements {
		summary, err := assurancepostcondition.NewSummary(
			requirement,
			assurancepostcondition.SummaryNotObserved,
		)
		if err != nil {
			panic(err)
		}
		summaries[index] = summary
	}
	return mustSummarySet(summaries)
}

func summarize(evidence Evidence) assurancepostcondition.Summary {
	var state assurancepostcondition.SummaryState
	switch evidence.State() {
	case EvidenceSatisfied:
		state = assurancepostcondition.SummarySatisfied
	case EvidenceUnsatisfied:
		state = assurancepostcondition.SummaryUnsatisfied
	case EvidenceUnavailable:
		state = assurancepostcondition.SummaryUnavailable
	case EvidenceStale:
		state = assurancepostcondition.SummaryStale
	case EvidenceMalformed:
		state = assurancepostcondition.SummaryMalformed
	case EvidenceUnsafe:
		state = assurancepostcondition.SummaryUnsafe
	case EvidenceContradictory:
		state = assurancepostcondition.SummaryContradictory
	default:
		panic(fmt.Sprintf(
			"unsupported current effect postcondition state %q",
			evidence.State(),
		))
	}
	summary, err := assurancepostcondition.NewSummary(evidence.Requirement(), state)
	if err != nil {
		panic(err)
	}
	return summary
}

func mustSummarySet(
	summaries []assurancepostcondition.Summary,
) assurancepostcondition.SummarySet {
	set, err := assurancepostcondition.NewSummarySet(summaries)
	if err != nil {
		panic(err)
	}
	return set
}

func evidenceFor(
	facts []Evidence,
	requirement effectpostcondition.Requirement,
) (Evidence, bool) {
	for _, fact := range facts {
		if fact.Requirement() == requirement {
			return fact, true
		}
	}
	return Evidence{}, false
}

func indeterminateReason(state EvidenceState) AssessmentReason {
	switch state {
	case EvidenceUnavailable:
		return AssessmentReasonUnavailable
	case EvidenceStale:
		return AssessmentReasonStale
	case EvidenceMalformed:
		return AssessmentReasonMalformed
	case EvidenceUnsafe:
		return AssessmentReasonUnsafe
	case EvidenceContradictory:
		return AssessmentReasonContradictory
	default:
		return AssessmentReasonNone
	}
}
