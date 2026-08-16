package hostroute

import (
	"fmt"
	"time"

	observepostcondition "github.com/isty2e/daem/internal/assurance/observe/postcondition"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/topology"
)

// ResultClass is the post-attempt host-route result class. It is intentionally
// weaker than install/convergence/readiness.
type ResultClass string

const (
	ResultAttemptedObservedPresent ResultClass = "attempted_observed_present"
	ResultAttemptedObservedAbsent  ResultClass = "attempted_observed_absent"
	ResultAmbiguousObservation     ResultClass = "ambiguous_observation"
	ResultAttemptedUnverified      ResultClass = "attempted_unverified"
	ResultFailed                   ResultClass = "failed"
	ResultBlocked                  ResultClass = "blocked"
)

// ResultReasonCode records why a post-attempt result received its class.
type ResultReasonCode string

const (
	ResultReasonAttemptMissing                   ResultReasonCode = "attempt_missing"
	ResultReasonAttemptTimestampMissing          ResultReasonCode = "attempt_timestamp_missing"
	ResultReasonCommandFailed                    ResultReasonCode = "command_failed"
	ResultReasonMissingEnvRef                    ResultReasonCode = "missing_env_ref"
	ResultReasonMissingRunner                    ResultReasonCode = "missing_runner"
	ResultReasonNonZeroExit                      ResultReasonCode = "nonzero_exit"
	ResultReasonTimeout                          ResultReasonCode = "timeout"
	ResultReasonRunnerError                      ResultReasonCode = "runner_error"
	ResultReasonWorkDirAuthority                 ResultReasonCode = "workdir_authority"
	ResultReasonObservedPresent                  ResultReasonCode = "observed_present"
	ResultReasonObservedAbsent                   ResultReasonCode = "observed_absent"
	ResultReasonUnkeyedSameSubject               ResultReasonCode = "unkeyed_same_subject"
	ResultReasonSameSubjectShadow                ResultReasonCode = "same_name_shadow"
	ResultReasonManagedKeyDrift                  ResultReasonCode = "managed_key_drift"
	ResultReasonAmbiguousRelation                ResultReasonCode = "ambiguous_relation"
	ResultReasonObservationUnavailable           ResultReasonCode = "observation_unavailable"
	ResultReasonObservationUnsupported           ResultReasonCode = "observation_unsupported"
	ResultReasonObservationStale                 ResultReasonCode = "observation_stale"
	ResultReasonObservationParseFailed           ResultReasonCode = "observation_parse_failed"
	ResultReasonEffectPostconditionMissing       ResultReasonCode = "effect_postcondition_missing"
	ResultReasonEffectPostconditionUnsatisfied   ResultReasonCode = "effect_postcondition_unsatisfied"
	ResultReasonEffectPostconditionUnavailable   ResultReasonCode = "effect_postcondition_unavailable"
	ResultReasonEffectPostconditionStale         ResultReasonCode = "effect_postcondition_stale"
	ResultReasonEffectPostconditionMalformed     ResultReasonCode = "effect_postcondition_malformed"
	ResultReasonEffectPostconditionUnsafe        ResultReasonCode = "effect_postcondition_unsafe"
	ResultReasonEffectPostconditionContradictory ResultReasonCode = "effect_postcondition_contradictory"
	ResultReasonEffectPostconditionForeign       ResultReasonCode = "effect_postcondition_foreign"
	ResultReasonUnsupportedObservation           ResultReasonCode = "unsupported_observation"
)

// RelationPostcondition identifies the relation fact required after a
// successful host attempt. Exact requires source-exact correlation; present
// also accepts bounded unkeyed same-subject evidence; absent requires fresh
// evidence that the exact relation is missing. None grants durable authority.
type RelationPostcondition uint8

const (
	RelationPostconditionExact RelationPostcondition = iota
	RelationPostconditionPresent
	RelationPostconditionAbsent
)

// Accepts reports whether the current relation state satisfies this
// postcondition without granting ownership or future skip authority.
func (postcondition RelationPostcondition) Accepts(
	state observerelation.CorrelationState,
) bool {
	switch postcondition {
	case RelationPostconditionExact:
		return state == observerelation.StateExactCorrelation
	case RelationPostconditionPresent:
		return state == observerelation.StateExactCorrelation ||
			state == observerelation.StateUnkeyedSameSubject
	case RelationPostconditionAbsent:
		return state == observerelation.StateMissing
	default:
		return false
	}
}

func (postcondition RelationPostcondition) validate() error {
	switch postcondition {
	case RelationPostconditionExact, RelationPostconditionPresent,
		RelationPostconditionAbsent:
		return nil
	default:
		return fmt.Errorf("relation postcondition %d is unsupported", postcondition)
	}
}

func (postcondition RelationPostcondition) satisfiedResult() (
	ResultClass,
	ResultReasonCode,
	observerelation.ObservationSummary,
) {
	switch postcondition {
	case RelationPostconditionAbsent:
		return ResultAttemptedObservedAbsent,
			ResultReasonObservedAbsent,
			observerelation.ObservationMissing
	case RelationPostconditionExact, RelationPostconditionPresent:
		return ResultAttemptedObservedPresent,
			ResultReasonObservedPresent,
			observerelation.ObservationPresent
	default:
		panic(fmt.Sprintf("validated relation postcondition %d is unsupported", postcondition))
	}
}

// ObservationFact carries the current passive relation observation, or a typed
// reason explaining why no usable current observation exists.
type ObservationFact struct {
	observed              bool
	result                observerelation.CorrelationResult
	reason                ResultReasonCode
	effectEvidence        observepostcondition.Set
	effectEvidencePresent bool
}

// CurrentObservation constructs a fact from a fresh passive correlation result.
// The classifier still honors stale, unsupported, unavailable, and ambiguous
// states carried by the correlation.
func CurrentObservation(result observerelation.CorrelationResult) ObservationFact {
	return ObservationFact{
		observed: true,
		result:   result,
	}
}

// CurrentObservationWithEffectEvidence constructs one fresh relation fact plus
// identity-bound route-coupled effect evidence.
func CurrentObservationWithEffectEvidence(
	result observerelation.CorrelationResult,
	evidence observepostcondition.Set,
) ObservationFact {
	return ObservationFact{
		observed:              true,
		result:                result,
		effectEvidence:        evidence,
		effectEvidencePresent: true,
	}
}

// ObservationUnavailable records that post-attempt observation could not produce
// a usable correlation result.
func ObservationUnavailable(reason ResultReasonCode) ObservationFact {
	return ObservationFact{reason: canonicalUnavailableObservationReason(reason)}
}

// Correlation returns the complete current passive fact when observation
// succeeded. An unavailable observation returns false.
func (observation ObservationFact) Correlation() (observerelation.CorrelationResult, bool) {
	if !observation.observed {
		return observerelation.CorrelationResult{}, false
	}
	return observation.result, true
}

// ResultInput composes route identity, mechanical attempt facts, and current
// observation facts. It does not grant execution or persistence authority.
type ResultInput struct {
	Subject               topology.SubjectID
	RouteRequest          realizationdelegate.Request
	Attempt               AttemptFact
	Observation           ObservationFact
	RequiredPostcondition PostconditionRequirement
}

// Result is a bounded post-attempt classification. The class, observation
// summary, and postcondition summary may be persisted as history-only
// diagnostics; none of them grants future apply skip authority.
type Result struct {
	subject              topology.SubjectID
	routeRequest         realizationdelegate.Request
	class                ResultClass
	reason               ResultReasonCode
	attempt              AttemptSummary
	observation          observerelation.ObservationSummary
	postcondition        observerelation.PostconditionSummary
	effectPostconditions observepostcondition.Assessment
}

// AttemptSummary is an allow-listed diagnostic summary of already-sanitized,
// bounded command-attempt facts.
type AttemptSummary struct {
	observed    bool
	attemptedAt time.Time
	reason      AttemptReason
	exitCode    int
	hasExitCode bool
	timedOut    bool
	redacted    bool
}

// StateSummary is the allow-list intended for history/state authority checks.
// It excludes command output text, host paths, package/cache identifiers, and
// raw observation rows.
type StateSummary struct {
	class                ResultClass
	reason               ResultReasonCode
	attemptObserved      bool
	attemptReason        AttemptReason
	exitCode             int
	hasExitCode          bool
	timedOut             bool
	redacted             bool
	observation          observerelation.ObservationSummary
	postcondition        observerelation.PostconditionSummary
	effectPostconditions observepostcondition.Assessment
}

// ClassifyResult composes one mechanical attempt result with one current
// relation observation without treating process success as convergence.
func ClassifyResult(input ResultInput) (Result, error) {
	if err := input.Subject.Validate(); err != nil {
		return Result{}, fmt.Errorf("%s: host route subject identity: %w", ResultReasonUnsupportedObservation, err)
	}
	if err := input.RouteRequest.Validate(); err != nil {
		return Result{}, fmt.Errorf("%s: host route request identity: %w", ResultReasonUnsupportedObservation, err)
	}
	if err := input.RequiredPostcondition.validate(); err != nil {
		return Result{}, fmt.Errorf("%s: %w", ResultReasonUnsupportedObservation, err)
	}
	if !input.Attempt.observed {
		return Result{}, fmt.Errorf("%s: current host route attempt is required", ResultReasonAttemptMissing)
	}
	if input.Attempt.attemptedAt.IsZero() {
		return Result{}, fmt.Errorf("%s: observed attempt requires attempted_at", ResultReasonAttemptTimestampMissing)
	}
	if err := input.Attempt.validate(); err != nil {
		return Result{}, err
	}
	if input.Observation.observed && input.Observation.result.State() == "" {
		return Result{}, fmt.Errorf("%s: current relation observation state is required", ResultReasonUnsupportedObservation)
	}
	effectAssessment, err := assessEffectPostconditions(
		input.Subject,
		input.RouteRequest,
		input.Observation,
		input.RequiredPostcondition,
	)
	if err != nil {
		return Result{}, err
	}
	relationPostcondition := input.RequiredPostcondition.relationPostcondition()
	result := Result{
		subject:              input.Subject,
		routeRequest:         input.RouteRequest,
		attempt:              attemptSummary(input.Attempt),
		effectPostconditions: effectAssessment,
		observation: observationSummary(
			input.Observation,
			relationPostcondition,
		),
		postcondition: observerelation.PostconditionNotObserved,
	}
	if input.Attempt.workDirAuthorityLost {
		result.class = ResultFailed
		result.reason = ResultReasonWorkDirAuthority
		result.postcondition = postconditionSummary(
			input.Observation,
			relationPostcondition,
		)
		return result, nil
	}
	if input.Attempt.reason != AttemptReasonNone {
		result.class = ResultFailed
		result.reason = attemptFailureReason(input.Attempt.reason)
		result.postcondition = postconditionSummary(
			input.Observation,
			relationPostcondition,
		)
		return result, nil
	}
	return classifySucceededAttempt(
		result,
		input.Observation,
		relationPostcondition,
	)
}

func classifySucceededAttempt(
	result Result,
	observation ObservationFact,
	requiredPostcondition RelationPostcondition,
) (Result, error) {
	if !observation.observed {
		result.class = ResultAttemptedUnverified
		result.reason = observation.reason
		if result.reason == "" {
			result.reason = ResultReasonObservationUnavailable
		}
		result.postcondition = observerelation.PostconditionUnknown
		return result, nil
	}
	if requiredPostcondition.Accepts(observation.result.State()) {
		if !result.effectPostconditions.Satisfied() {
			_, _, result.observation = requiredPostcondition.satisfiedResult()
			result.class = ResultAttemptedUnverified
			result.reason = effectAssessmentReason(result.effectPostconditions.Reason())
			result.postcondition = observerelation.PostconditionObserved
			return result, nil
		}
		result.class, result.reason, result.observation = requiredPostcondition.satisfiedResult()
		result.postcondition = observerelation.PostconditionObserved
		return result, nil
	}
	switch observation.result.State() {
	case observerelation.StateExactCorrelation:
		result.class = ResultAttemptedObservedPresent
		result.reason = ResultReasonObservedPresent
		result.observation = observerelation.ObservationPresent
		result.postcondition = observerelation.PostconditionFailed
	case observerelation.StateMissing:
		result.class = ResultAttemptedObservedAbsent
		result.reason = ResultReasonObservedAbsent
		result.postcondition = observerelation.PostconditionMissing
	case observerelation.StateUnkeyedSameSubject:
		result.class = ResultBlocked
		result.reason = ResultReasonUnkeyedSameSubject
		result.postcondition = observerelation.PostconditionUnknown
	case observerelation.StateSameSubjectShadow:
		result.class = ResultBlocked
		result.reason = ResultReasonSameSubjectShadow
		result.postcondition = observerelation.PostconditionUnknown
	case observerelation.StateManagedKeyDrift:
		result.class = ResultBlocked
		result.reason = ResultReasonManagedKeyDrift
		result.postcondition = observerelation.PostconditionUnknown
	case observerelation.StateAmbiguous:
		result.class = ResultAmbiguousObservation
		result.reason = ResultReasonAmbiguousRelation
		result.postcondition = observerelation.PostconditionUnknown
	case observerelation.StateStaleEvidence:
		result.class = ResultAttemptedUnverified
		result.reason = ResultReasonObservationStale
		result.postcondition = observerelation.PostconditionUnknown
	case observerelation.StateUnsupported:
		result.class = ResultAttemptedUnverified
		result.reason = ResultReasonObservationUnsupported
		result.postcondition = observerelation.PostconditionUnknown
	case observerelation.StateUnavailableEvidence:
		result.class = ResultAttemptedUnverified
		result.reason = ResultReasonObservationUnavailable
		result.postcondition = observerelation.PostconditionUnknown
	default:
		return Result{}, fmt.Errorf(
			"%s: relation observation state %q is unsupported",
			ResultReasonUnsupportedObservation,
			observation.result.State(),
		)
	}
	return result, nil
}

func effectAssessmentReason(
	reason observepostcondition.AssessmentReason,
) ResultReasonCode {
	switch reason {
	case observepostcondition.AssessmentReasonMissing:
		return ResultReasonEffectPostconditionMissing
	case observepostcondition.AssessmentReasonUnsatisfied:
		return ResultReasonEffectPostconditionUnsatisfied
	case observepostcondition.AssessmentReasonUnavailable:
		return ResultReasonEffectPostconditionUnavailable
	case observepostcondition.AssessmentReasonStale:
		return ResultReasonEffectPostconditionStale
	case observepostcondition.AssessmentReasonMalformed:
		return ResultReasonEffectPostconditionMalformed
	case observepostcondition.AssessmentReasonUnsafe:
		return ResultReasonEffectPostconditionUnsafe
	case observepostcondition.AssessmentReasonContradictory:
		return ResultReasonEffectPostconditionContradictory
	case observepostcondition.AssessmentReasonForeign:
		return ResultReasonEffectPostconditionForeign
	default:
		return ResultReasonEffectPostconditionMissing
	}
}

func attemptSummary(attempt AttemptFact) AttemptSummary {
	if !attempt.observed {
		return AttemptSummary{}
	}
	return AttemptSummary{
		observed:    true,
		attemptedAt: attempt.attemptedAt,
		reason:      attempt.reason,
		exitCode:    attempt.exitCode,
		hasExitCode: attempt.hasExitCode,
		timedOut:    attempt.timedOut,
		redacted:    attempt.redacted,
	}
}

func observationSummary(
	observation ObservationFact,
	requiredPostcondition RelationPostcondition,
) observerelation.ObservationSummary {
	if !observation.observed {
		return observerelation.ObservationNotObserved
	}
	if requiredPostcondition.Accepts(observation.result.State()) {
		_, _, summary := requiredPostcondition.satisfiedResult()
		return summary
	}
	return observerelation.SummarizeObservation(observation.result.State())
}

func postconditionSummary(
	observation ObservationFact,
	requiredPostcondition RelationPostcondition,
) observerelation.PostconditionSummary {
	if !observation.observed {
		return observerelation.PostconditionNotObserved
	}
	if requiredPostcondition.Accepts(observation.result.State()) {
		return observerelation.PostconditionObserved
	}
	return observerelation.SummarizePostcondition(observation.result.State())
}

func attemptFailureReason(reason AttemptReason) ResultReasonCode {
	switch reason {
	case AttemptReasonMissingEnvRef:
		return ResultReasonMissingEnvRef
	case AttemptReasonMissingRunner:
		return ResultReasonMissingRunner
	case AttemptReasonNonZeroExit:
		return ResultReasonNonZeroExit
	case AttemptReasonTimeout:
		return ResultReasonTimeout
	case AttemptReasonCanceled,
		AttemptReasonSignaled,
		AttemptReasonRunnerError:
		return ResultReasonRunnerError
	default:
		return ResultReasonCommandFailed
	}
}

func canonicalUnavailableObservationReason(reason ResultReasonCode) ResultReasonCode {
	switch reason {
	case ResultReasonObservationParseFailed,
		ResultReasonObservationStale,
		ResultReasonObservationUnsupported,
		ResultReasonObservationUnavailable:
		return reason
	default:
		return ResultReasonObservationUnavailable
	}
}
