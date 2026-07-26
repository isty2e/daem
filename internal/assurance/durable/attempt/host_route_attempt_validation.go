package attempt

import (
	"fmt"

	assurancepostcondition "github.com/isty2e/daem/internal/assurance/postcondition"
)

func (class HostRouteResultClass) valid() bool {
	switch class {
	case HostRouteResultAttemptedObservedPresent,
		HostRouteResultAttemptedObservedAbsent,
		HostRouteResultAmbiguousObservation,
		HostRouteResultAttemptedUnverified,
		HostRouteResultFailed,
		HostRouteResultBlocked,
		HostRouteResultBlockedPreflight,
		HostRouteResultUnsupportedSource,
		HostRouteResultUnsupportedScope:
		return true
	default:
		return false
	}
}

func (reason HostRouteAttemptReason) valid() bool {
	switch reason {
	case HostRouteAttemptReasonNone,
		HostRouteAttemptReasonMissingEnvRef,
		HostRouteAttemptReasonMissingRunner,
		HostRouteAttemptReasonNonZeroExit,
		HostRouteAttemptReasonTimeout,
		HostRouteAttemptReasonCanceled,
		HostRouteAttemptReasonSignaled,
		HostRouteAttemptReasonRunnerError,
		HostRouteAttemptReasonWorkDirAuthority:
		return true
	default:
		return false
	}
}

func (reason HostRouteResultReason) valid() bool {
	switch reason {
	case HostRouteReasonCommandFailed,
		HostRouteReasonMissingEnvRef,
		HostRouteReasonMissingRunner,
		HostRouteReasonNonZeroExit,
		HostRouteReasonTimeout,
		HostRouteReasonRunnerError,
		HostRouteReasonWorkDirAuthority,
		HostRouteReasonObservedPresent,
		HostRouteReasonObservedAbsent,
		HostRouteReasonUnkeyedSameSubject,
		HostRouteReasonSameSubjectShadow,
		HostRouteReasonManagedKeyDrift,
		HostRouteReasonAmbiguousRelation,
		HostRouteReasonObservationUnavailable,
		HostRouteReasonObservationUnsupported,
		HostRouteReasonObservationStale,
		HostRouteReasonObservationParseFailed,
		HostRouteReasonEffectMissing,
		HostRouteReasonEffectUnsatisfied,
		HostRouteReasonEffectUnavailable,
		HostRouteReasonEffectStale,
		HostRouteReasonEffectMalformed,
		HostRouteReasonEffectUnsafe,
		HostRouteReasonEffectContradictory,
		HostRouteReasonEffectForeign,
		HostRouteReasonPreflightFailed,
		HostRouteReasonUnsupportedAction,
		HostRouteReasonMissingWorkDir,
		HostRouteReasonLockedSubjectMissing,
		HostRouteReasonLockedSubjectAmbiguous,
		HostRouteReasonUnsupportedRoute,
		HostRouteReasonInvalidLockedRecord,
		HostRouteReasonRouteRequestMismatch,
		HostRouteReasonTargetMismatch,
		HostRouteReasonScopeMismatch,
		HostRouteReasonRelationKeyMismatch,
		HostRouteReasonUnsupportedScope,
		HostRouteReasonUnsupportedSource:
		return true
	default:
		return false
	}
}

func validateHostRouteClassification(
	class HostRouteResultClass,
	reason HostRouteResultReason,
	attemptObserved bool,
) error {
	requiresAttempt := true
	reasonAllowed := false
	switch class {
	case HostRouteResultAttemptedObservedPresent:
		reasonAllowed = reason == HostRouteReasonObservedPresent
	case HostRouteResultAttemptedObservedAbsent:
		reasonAllowed = reason == HostRouteReasonObservedAbsent
	case HostRouteResultAmbiguousObservation:
		reasonAllowed = reason == HostRouteReasonAmbiguousRelation
	case HostRouteResultAttemptedUnverified:
		switch reason {
		case HostRouteReasonObservationUnavailable,
			HostRouteReasonObservationUnsupported,
			HostRouteReasonObservationStale,
			HostRouteReasonObservationParseFailed,
			HostRouteReasonEffectMissing,
			HostRouteReasonEffectUnsatisfied,
			HostRouteReasonEffectUnavailable,
			HostRouteReasonEffectStale,
			HostRouteReasonEffectMalformed,
			HostRouteReasonEffectUnsafe,
			HostRouteReasonEffectContradictory,
			HostRouteReasonEffectForeign:
			reasonAllowed = true
		}
	case HostRouteResultFailed:
		switch reason {
		case HostRouteReasonCommandFailed,
			HostRouteReasonMissingEnvRef,
			HostRouteReasonMissingRunner,
			HostRouteReasonNonZeroExit,
			HostRouteReasonTimeout,
			HostRouteReasonRunnerError,
			HostRouteReasonWorkDirAuthority:
			reasonAllowed = true
		}
	case HostRouteResultBlocked:
		switch reason {
		case HostRouteReasonUnkeyedSameSubject,
			HostRouteReasonSameSubjectShadow,
			HostRouteReasonManagedKeyDrift:
			reasonAllowed = true
		}
	case HostRouteResultBlockedPreflight:
		switch reason {
		case HostRouteReasonPreflightFailed,
			HostRouteReasonUnsupportedAction,
			HostRouteReasonMissingWorkDir,
			HostRouteReasonLockedSubjectMissing,
			HostRouteReasonLockedSubjectAmbiguous,
			HostRouteReasonUnsupportedRoute,
			HostRouteReasonInvalidLockedRecord,
			HostRouteReasonRouteRequestMismatch,
			HostRouteReasonTargetMismatch,
			HostRouteReasonScopeMismatch,
			HostRouteReasonRelationKeyMismatch:
			reasonAllowed = true
		}
		requiresAttempt = false
	case HostRouteResultUnsupportedSource:
		reasonAllowed = reason == HostRouteReasonUnsupportedSource
		requiresAttempt = false
	case HostRouteResultUnsupportedScope:
		reasonAllowed = reason == HostRouteReasonUnsupportedScope
		requiresAttempt = false
	}
	if !reasonAllowed {
		return fmt.Errorf(
			"host route classification %q does not admit reason %q",
			class,
			reason,
		)
	}
	if attemptObserved != requiresAttempt {
		if requiresAttempt {
			return fmt.Errorf("host route classification %q requires a current attempt", class)
		}
		return fmt.Errorf("host route classification %q forbids a current attempt", class)
	}
	return nil
}

func validateEffectPostconditionSummaryCorrelation(
	class HostRouteResultClass,
	reason HostRouteResultReason,
	summaries assurancepostcondition.SummarySet,
) error {
	values := summaries.Summaries()
	if class == HostRouteResultAttemptedObservedPresent ||
		class == HostRouteResultAttemptedObservedAbsent {
		for _, summary := range values {
			if summary.State() != assurancepostcondition.SummarySatisfied {
				return fmt.Errorf(
					"verified host route classification cannot carry %q effect postcondition",
					summary.State(),
				)
			}
		}
	}

	expected, coupledReason := effectReasonSummaryState(reason)
	if !coupledReason {
		return nil
	}
	for _, summary := range values {
		if summary.State() == expected {
			return nil
		}
	}
	return fmt.Errorf(
		"host route result reason %q requires a matching effect postcondition summary",
		reason,
	)
}

func effectReasonSummaryState(
	reason HostRouteResultReason,
) (assurancepostcondition.SummaryState, bool) {
	switch reason {
	case HostRouteReasonEffectMissing:
		return assurancepostcondition.SummaryNotObserved, true
	case HostRouteReasonEffectUnsatisfied:
		return assurancepostcondition.SummaryUnsatisfied, true
	case HostRouteReasonEffectUnavailable:
		return assurancepostcondition.SummaryUnavailable, true
	case HostRouteReasonEffectStale:
		return assurancepostcondition.SummaryStale, true
	case HostRouteReasonEffectMalformed:
		return assurancepostcondition.SummaryMalformed, true
	case HostRouteReasonEffectUnsafe:
		return assurancepostcondition.SummaryUnsafe, true
	case HostRouteReasonEffectContradictory:
		return assurancepostcondition.SummaryContradictory, true
	case HostRouteReasonEffectForeign:
		return assurancepostcondition.SummaryNotObserved, true
	default:
		return "", false
	}
}

func validateHostRouteAttemptCorrelation(
	class HostRouteResultClass,
	reason HostRouteResultReason,
	attemptReason HostRouteAttemptReason,
) error {
	if class != HostRouteResultFailed {
		if attemptReason != HostRouteAttemptReasonNone {
			return fmt.Errorf(
				"non-failed classification %q cannot carry failure attempt reason %q",
				class,
				attemptReason,
			)
		}
		return nil
	}

	matches := false
	switch reason {
	case HostRouteReasonCommandFailed:
		matches = attemptReason == HostRouteAttemptReasonNone
	case HostRouteReasonMissingEnvRef:
		matches = attemptReason == HostRouteAttemptReasonMissingEnvRef
	case HostRouteReasonMissingRunner:
		matches = attemptReason == HostRouteAttemptReasonMissingRunner
	case HostRouteReasonNonZeroExit:
		matches = attemptReason == HostRouteAttemptReasonNonZeroExit
	case HostRouteReasonTimeout:
		matches = attemptReason == HostRouteAttemptReasonTimeout
	case HostRouteReasonRunnerError:
		switch attemptReason {
		case HostRouteAttemptReasonCanceled,
			HostRouteAttemptReasonSignaled,
			HostRouteAttemptReasonRunnerError:
			matches = true
		}
	case HostRouteReasonWorkDirAuthority:
		matches = attemptReason == HostRouteAttemptReasonWorkDirAuthority
	}
	if !matches {
		return fmt.Errorf(
			"%s classification requires matching attempt reason, got %q",
			reason,
			attemptReason,
		)
	}
	return nil
}
