package mcp

import "fmt"

// LastDelegateAttemptObservationFromInput classifies one sanitized historical
// attempt without granting current projection or runtime authority.
func LastDelegateAttemptObservationFromInput(
	input LastDelegateAttemptInput,
) (LastDelegateAttemptObservation, error) {
	if !input.Observed {
		if input.MatchesPlanIdentity || input.Status != "" || input.Reason != ReasonNone {
			return LastDelegateAttemptObservation{}, fmt.Errorf("unobserved delegate attempt cannot carry identity, status, or reason")
		}
		return LastDelegateAttemptObservation{
			State:  DelegateAttemptNotObserved,
			Reason: ReasonLastDelegateAttemptUnobserved,
		}, nil
	}
	if !input.MatchesPlanIdentity {
		if input.Status != "" || input.Reason != ReasonNone {
			return LastDelegateAttemptObservation{}, fmt.Errorf("plan-mismatched delegate attempt cannot carry status or reason")
		}
		return LastDelegateAttemptObservation{
			State:  DelegateAttemptStale,
			Reason: ReasonLastDelegateAttemptStale,
		}, nil
	}
	switch input.Status {
	case DelegateAttemptSucceeded:
		if input.Reason != ReasonNone {
			return LastDelegateAttemptObservation{}, fmt.Errorf("successful delegate attempt cannot carry reason %q", input.Reason)
		}
		return LastDelegateAttemptObservation{State: DelegateAttemptSucceeded}, nil
	case DelegateAttemptFailed:
		switch input.Reason {
		case ReasonDelegateMissingEnvRef,
			ReasonDelegateMissingRunner,
			ReasonDelegateNonZeroExit,
			ReasonDelegateTimeout,
			ReasonDelegateRunnerError:
		default:
			return LastDelegateAttemptObservation{}, fmt.Errorf("failed delegate attempt has unsupported reason %q", input.Reason)
		}
		return LastDelegateAttemptObservation{
			State:  DelegateAttemptFailed,
			Reason: input.Reason,
		}, nil
	case DelegateAttemptBlocked:
		if input.Reason != ReasonDelegatePolicyBlocked {
			return LastDelegateAttemptObservation{}, fmt.Errorf("blocked delegate attempt requires reason %q", ReasonDelegatePolicyBlocked)
		}
		return LastDelegateAttemptObservation{
			State:  DelegateAttemptBlocked,
			Reason: input.Reason,
		}, nil
	default:
		return LastDelegateAttemptObservation{}, fmt.Errorf("matching delegate attempt has unsupported status %q", input.Status)
	}
}
