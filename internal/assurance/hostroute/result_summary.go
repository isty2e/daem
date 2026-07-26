package hostroute

import (
	"time"

	observepostcondition "github.com/isty2e/daem/internal/assurance/observe/postcondition"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/topology"
)

// Subject returns the locked subject associated with this classification.
func (result Result) Subject() topology.SubjectID {
	return result.subject
}

// RouteRequest returns the locked route identity associated with this result.
func (result Result) RouteRequest() realizationdelegate.Request {
	return result.routeRequest
}

// Class returns the post-attempt result class.
func (result Result) Class() ResultClass {
	return result.class
}

// Attempt returns the allow-listed mechanical attempt summary.
func (result Result) Attempt() AttemptSummary {
	return result.attempt
}

// ObservationSummary returns the history-safe observation summary class.
func (result Result) ObservationSummary() observerelation.ObservationSummary {
	return result.observation
}

// PostconditionSummary returns the history-safe postcondition summary class.
func (result Result) PostconditionSummary() observerelation.PostconditionSummary {
	return result.postcondition
}

// EffectPostconditionAssessment returns the bounded route-coupled effect
// assessment. Its summaries contain no host paths or raw adapter data.
func (result Result) EffectPostconditionAssessment() observepostcondition.Assessment {
	return result.effectPostconditions
}

// PostconditionsSatisfied reports current satisfaction of the primary
// relation fact and every exact route-coupled effect fact. Mechanical attempt
// success remains an independent diagnostic axis.
func (result Result) PostconditionsSatisfied() bool {
	return result.postcondition == observerelation.PostconditionObserved &&
		result.effectPostconditions.Satisfied()
}

// StateSummary returns the state/history authority allow-list for this result.
func (result Result) StateSummary() StateSummary {
	exitCode, hasExitCode := result.attempt.ExitCode()
	return StateSummary{
		class:                result.class,
		reason:               result.reason,
		attemptObserved:      result.attempt.Observed(),
		attemptReason:        result.attempt.Reason(),
		exitCode:             exitCode,
		hasExitCode:          hasExitCode,
		timedOut:             result.attempt.TimedOut(),
		redacted:             result.attempt.Redacted(),
		observation:          result.ObservationSummary(),
		postcondition:        result.PostconditionSummary(),
		effectPostconditions: result.EffectPostconditionAssessment(),
	}
}

func (summary AttemptSummary) Observed() bool { return summary.observed }
func (summary AttemptSummary) AttemptedAt() time.Time {
	return summary.attemptedAt
}
func (summary AttemptSummary) Reason() AttemptReason { return summary.reason }
func (summary AttemptSummary) ExitCode() (int, bool) {
	return summary.exitCode, summary.hasExitCode
}
func (summary AttemptSummary) TimedOut() bool { return summary.timedOut }
func (summary AttemptSummary) Redacted() bool { return summary.redacted }

func (summary StateSummary) Class() ResultClass           { return summary.class }
func (summary StateSummary) Reason() ResultReasonCode     { return summary.reason }
func (summary StateSummary) AttemptObserved() bool        { return summary.attemptObserved }
func (summary StateSummary) AttemptReason() AttemptReason { return summary.attemptReason }
func (summary StateSummary) ExitCode() (int, bool)        { return summary.exitCode, summary.hasExitCode }
func (summary StateSummary) TimedOut() bool               { return summary.timedOut }
func (summary StateSummary) Redacted() bool               { return summary.redacted }
func (summary StateSummary) Observation() observerelation.ObservationSummary {
	return summary.observation
}

func (summary StateSummary) Postcondition() observerelation.PostconditionSummary {
	return summary.postcondition
}

func (summary StateSummary) EffectPostconditions() observepostcondition.Assessment {
	return summary.effectPostconditions
}
