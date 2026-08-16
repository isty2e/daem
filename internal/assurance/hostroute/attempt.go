package hostroute

import (
	"fmt"
	"time"
)

// AttemptReason classifies the sanitized mechanical outcome observed at the
// command execution boundary.
type AttemptReason string

const (
	AttemptReasonNone          AttemptReason = ""
	AttemptReasonMissingEnvRef AttemptReason = "missing_env_ref"
	AttemptReasonMissingRunner AttemptReason = "missing_runner"
	AttemptReasonNonZeroExit   AttemptReason = "nonzero_exit"
	AttemptReasonTimeout       AttemptReason = "timeout"
	AttemptReasonCanceled      AttemptReason = "canceled"
	AttemptReasonSignaled      AttemptReason = "signaled"
	AttemptReasonRunnerError   AttemptReason = "runner_error"
)

// AttemptObservation is the narrow sanitized mechanical view accepted from an
// execution boundary. Implementations retain ownership of command execution;
// the classifier copies only these bounded facts.
type AttemptObservation interface {
	AttemptedAt() time.Time
	ExitCode() (int, bool)
	TimedOut() bool
	Canceled() bool
	Signaled() bool
	WorkDirAuthorityFailed() bool
	StdoutTruncated() bool
	StderrTruncated() bool
	Redacted() bool
	ErrorDetail() string
}

// AttemptFact carries the sanitized mechanical result of one route attempt.
type AttemptFact struct {
	observed             bool
	attemptedAt          time.Time
	reason               AttemptReason
	exitCode             int
	hasExitCode          bool
	timedOut             bool
	canceled             bool
	signaled             bool
	stdoutTruncated      bool
	stderrTruncated      bool
	redacted             bool
	errorDetail          string
	workDirAuthorityLost bool
}

// ObservedAttempt constructs an attempt fact from the command execution
// boundary. The classifier validates timestamp and reason coherence.
func ObservedAttempt(observation AttemptObservation, reason AttemptReason) AttemptFact {
	exitCode, hasExitCode := observation.ExitCode()
	workDirAuthorityLost := observation.WorkDirAuthorityFailed()
	if workDirAuthorityLost && reason == AttemptReason("workdir_authority") {
		switch {
		case observation.TimedOut():
			reason = AttemptReasonTimeout
		case observation.Canceled():
			reason = AttemptReasonCanceled
		case observation.Signaled():
			reason = AttemptReasonSignaled
		case hasExitCode && exitCode != 0:
			reason = AttemptReasonNonZeroExit
		default:
			reason = AttemptReasonNone
		}
	}
	return AttemptFact{
		observed:             true,
		attemptedAt:          observation.AttemptedAt(),
		reason:               reason,
		exitCode:             exitCode,
		hasExitCode:          hasExitCode,
		timedOut:             observation.TimedOut(),
		canceled:             observation.Canceled(),
		signaled:             observation.Signaled(),
		stdoutTruncated:      observation.StdoutTruncated(),
		stderrTruncated:      observation.StderrTruncated(),
		redacted:             observation.Redacted(),
		errorDetail:          observation.ErrorDetail(),
		workDirAuthorityLost: workDirAuthorityLost,
	}
}

func (reason AttemptReason) validate() error {
	switch reason {
	case AttemptReasonNone,
		AttemptReasonMissingEnvRef,
		AttemptReasonMissingRunner,
		AttemptReasonNonZeroExit,
		AttemptReasonTimeout,
		AttemptReasonCanceled,
		AttemptReasonSignaled,
		AttemptReasonRunnerError:
		return nil
	default:
		return fmt.Errorf("%s: unsupported mechanical attempt reason %q", ResultReasonCommandFailed, reason)
	}
}

func (attempt AttemptFact) validate() error {
	if err := attempt.reason.validate(); err != nil {
		return err
	}
	if attempt.timedOut && attempt.reason != AttemptReasonTimeout {
		return contradictoryAttemptError(attempt.reason, "timed_out")
	}
	if attempt.canceled && !attempt.timedOut &&
		attempt.reason != AttemptReasonCanceled {
		return contradictoryAttemptError(attempt.reason, "canceled")
	}
	if attempt.signaled && !attempt.timedOut && !attempt.canceled &&
		attempt.reason != AttemptReasonSignaled {
		return contradictoryAttemptError(attempt.reason, "signaled")
	}
	if attempt.hasExitCode && attempt.exitCode != 0 &&
		!attempt.timedOut && !attempt.canceled && !attempt.signaled &&
		attempt.reason != AttemptReasonNonZeroExit {
		return contradictoryAttemptError(attempt.reason, "nonzero_exit")
	}

	switch attempt.reason {
	case AttemptReasonTimeout:
		if !attempt.timedOut {
			return contradictoryAttemptError(attempt.reason, "timed_out=false")
		}
	case AttemptReasonCanceled:
		if !attempt.canceled || attempt.timedOut {
			return contradictoryAttemptError(attempt.reason, "canceled=false or timed_out=true")
		}
	case AttemptReasonSignaled:
		if !attempt.signaled || attempt.timedOut || attempt.canceled {
			return contradictoryAttemptError(attempt.reason, "signaled=false or higher-priority cancellation")
		}
	case AttemptReasonNonZeroExit:
		if !attempt.hasExitCode || attempt.exitCode == 0 ||
			attempt.timedOut || attempt.canceled || attempt.signaled {
			return contradictoryAttemptError(attempt.reason, "missing nonzero exit or higher-priority failure")
		}
	}
	return nil
}

func contradictoryAttemptError(reason AttemptReason, fact string) error {
	return fmt.Errorf(
		"%s: mechanical attempt reason %q contradicts %s",
		ResultReasonCommandFailed,
		reason,
		fact,
	)
}
