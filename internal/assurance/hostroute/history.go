package hostroute

import (
	"fmt"
	"time"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
)

// DurableAttemptInput supplies operation context for one already-classified
// host-route result. The conversion records history only and grants no future
// execution, skip, or relation authority.
type DurableAttemptInput struct {
	Result               Result
	Target               target.Target
	Scope                target.Scope
	Operation            lock.OperationKind
	WorkDirAuthorityLost bool
}

// NewDurableAttempt converts one bounded classification into operation-indexed
// durable history.
func NewDurableAttempt(input DurableAttemptInput) (durableattempt.HostRouteAttempt, error) {
	summary := input.Result.StateSummary()
	subject := input.Result.Subject()
	route := input.Result.RouteRequest()
	attempt := input.Result.Attempt()
	observedAt := time.Now().UTC()
	if !attempt.AttemptedAt().IsZero() {
		observedAt = attempt.AttemptedAt()
	}
	resultClass, err := durableResultClass(summary.Class())
	if err != nil {
		return durableattempt.HostRouteAttempt{}, err
	}
	reason := durableattempt.HostRouteResultReason(summary.Reason())
	attemptReason := durableAttemptReason(summary.AttemptReason())
	if input.WorkDirAuthorityLost &&
		attemptReason != durableattempt.HostRouteAttemptReasonWorkDirAuthority {
		resultClass = durableattempt.HostRouteResultFailed
		reason = durableattempt.HostRouteReasonWorkDirAuthority
		attemptReason = durableattempt.HostRouteAttemptReasonWorkDirAuthority
	}
	exitCode, hasExitCode := summary.ExitCode()
	var optionalExitCode *int
	if hasExitCode {
		optionalExitCode = &exitCode
	}
	return durableattempt.NewHostRouteAttempt(durableattempt.HostRouteAttemptInput{
		Subject:              subject,
		Target:               input.Target,
		Scope:                input.Scope,
		Operation:            input.Operation,
		RouteID:              route.RouteID(),
		RouteRequestHash:     route.CanonicalRequestHash(),
		ObservedAt:           observedAt,
		ResultClass:          resultClass,
		Reason:               reason,
		AttemptObserved:      summary.AttemptObserved(),
		AttemptReason:        attemptReason,
		Observation:          summary.Observation(),
		Postcondition:        summary.Postcondition(),
		EffectPostconditions: summary.EffectPostconditions().SummarySet(),
		ExitCode:             optionalExitCode,
		TimedOut:             summary.TimedOut(),
		Redacted:             summary.Redacted(),
	})
}

func durableResultClass(class ResultClass) (durableattempt.HostRouteResultClass, error) {
	switch class {
	case ResultAttemptedObservedPresent:
		return durableattempt.HostRouteResultAttemptedObservedPresent, nil
	case ResultAttemptedObservedAbsent:
		return durableattempt.HostRouteResultAttemptedObservedAbsent, nil
	case ResultAmbiguousObservation:
		return durableattempt.HostRouteResultAmbiguousObservation, nil
	case ResultAttemptedUnverified:
		return durableattempt.HostRouteResultAttemptedUnverified, nil
	case ResultFailed:
		return durableattempt.HostRouteResultFailed, nil
	case ResultBlocked:
		return durableattempt.HostRouteResultBlocked, nil
	default:
		return "", fmt.Errorf("unsupported host route result class %q", class)
	}
}

func durableAttemptReason(reason AttemptReason) durableattempt.HostRouteAttemptReason {
	switch reason {
	case AttemptReasonMissingEnvRef:
		return durableattempt.HostRouteAttemptReasonMissingEnvRef
	case AttemptReasonMissingRunner:
		return durableattempt.HostRouteAttemptReasonMissingRunner
	case AttemptReasonNonZeroExit:
		return durableattempt.HostRouteAttemptReasonNonZeroExit
	case AttemptReasonTimeout:
		return durableattempt.HostRouteAttemptReasonTimeout
	case AttemptReasonCanceled:
		return durableattempt.HostRouteAttemptReasonCanceled
	case AttemptReasonSignaled:
		return durableattempt.HostRouteAttemptReasonSignaled
	case AttemptReasonRunnerError:
		return durableattempt.HostRouteAttemptReasonRunnerError
	case AttemptReasonWorkDirAuthority:
		return durableattempt.HostRouteAttemptReasonWorkDirAuthority
	default:
		return durableattempt.HostRouteAttemptReasonNone
	}
}
