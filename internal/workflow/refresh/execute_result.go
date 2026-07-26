package refresh

import (
	"context"
	"errors"

	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/subprocess"
)

func processOutcome(attempt subprocess.CommandAttemptResult) *ProcessOutcome {
	outcome := &ProcessOutcome{
		Started:   attempt.Started(),
		Reason:    attempt.Reason(),
		TimedOut:  attempt.TimedOut(),
		Cancelled: attempt.Canceled(),
		Signaled:  attempt.Signaled(),
		Redacted:  attempt.Redacted(),
	}
	if exitCode, present := attempt.ExitCode(); present {
		outcome.ExitCode = &exitCode
	}
	return outcome
}

func applyClassification(
	result CommandResult,
	classified assurancehostroute.Result,
	attempt subprocess.CommandAttemptResult,
) CommandResult {
	switch {
	case attempt.Failed():
		result.ResultClass = ResultFailed
		result.ReasonCode = ReasonCommandFailed
		result.Remediation = []string{
			"inspect the host CLI and retry the explicit refresh when safe",
		}
	case classified.Class() == assurancehostroute.ResultAttemptedObservedPresent:
		result.ResultClass = ResultObservedRelation
		result.ReasonCode = ReasonNone
	case classified.Class() == assurancehostroute.ResultAttemptedUnverified:
		result.ResultClass = ResultAttemptedUnverified
		result.ReasonCode = ReasonObservationUnavailable
	default:
		result.ResultClass = ResultPartial
		result.ReasonCode = ReasonPostObservationFailed
		result.Remediation = []string{
			"run daem status and inspect the exact extension relation before retrying",
		}
	}
	return result
}

func resultClassAfterClassificationFailure(
	attempt subprocess.CommandAttemptResult,
) ResultClass {
	if attempt.Started() && attempt.Succeeded() {
		return ResultPartial
	}
	return ResultFailed
}

func sameObservationAuthorityPaths(
	left []observerelation.AuthorityPath,
	right []observerelation.AuthorityPath,
) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Path() != right[index].Path() ||
			left[index].Target() != right[index].Target() ||
			left[index].Scope() != right[index].Scope() {
			return false
		}
	}
	return true
}

func cancelledBeforeAttempt(
	result CommandResult,
	err error,
) (CommandResult, error) {
	result.ResultClass = ResultCancelled
	result.ReasonCode = ReasonCancelled
	result.Attempted = false
	result.Remediation = []string{"rerun refresh when ready"}
	return result, err
}

func staleBeforeAttempt(
	result CommandResult,
	err error,
) (CommandResult, error) {
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return cancelledBeforeAttempt(result, err)
	}
	result.ResultClass = ResultRefused
	result.ReasonCode = ReasonStalePlan
	result.Attempted = false
	result.Remediation = []string{"review a new dry-run plan before retrying"}
	return result, err
}

func refusedBeforeAttempt(
	result CommandResult,
	reason ReasonCode,
	err error,
) (CommandResult, error) {
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return cancelledBeforeAttempt(result, err)
	}
	result.ResultClass = ResultRefused
	result.ReasonCode = reason
	result.Attempted = false
	result.Remediation = []string{"restore the required authority and retry"}
	return result, err
}

func resultWithCleanupFailure(
	result CommandResult,
	attemptStarted bool,
) CommandResult {
	if attemptStarted {
		result.ResultClass = ResultPartial
		result.ReasonCode = ReasonMutationAuthority
	} else {
		result.ResultClass = ResultRefused
		result.ReasonCode = ReasonMutationAuthority
	}
	result.Remediation = []string{
		"inspect workspace authority and current host state before retrying",
	}
	return result
}
