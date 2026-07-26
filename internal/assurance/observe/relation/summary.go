package relation

import "fmt"

// ObservationSummary is the bounded history-safe class of one relation observation.
type ObservationSummary string

const (
	ObservationNotObserved ObservationSummary = "not_observed"
	ObservationPresent     ObservationSummary = "present"
	ObservationMissing     ObservationSummary = "missing"
	ObservationUnknown     ObservationSummary = "unknown"
)

// PostconditionSummary is the bounded history-safe class of one relation postcondition.
type PostconditionSummary string

const (
	PostconditionNotObserved PostconditionSummary = "not_observed"
	PostconditionObserved    PostconditionSummary = "observed"
	PostconditionMissing     PostconditionSummary = "missing"
	PostconditionFailed      PostconditionSummary = "failed"
	PostconditionUnknown     PostconditionSummary = "unknown"
)

// ParseObservationSummary normalizes an omitted persisted summary to not-observed.
func ParseObservationSummary(value string) (ObservationSummary, error) {
	switch summary := ObservationSummary(value); summary {
	case "":
		return ObservationNotObserved, nil
	case ObservationNotObserved, ObservationPresent, ObservationMissing, ObservationUnknown:
		return summary, nil
	default:
		return "", fmt.Errorf("relation observation summary %q is unsupported", value)
	}
}

// ParsePostconditionSummary normalizes an omitted persisted summary to not-observed.
func ParsePostconditionSummary(value string) (PostconditionSummary, error) {
	switch summary := PostconditionSummary(value); summary {
	case "":
		return PostconditionNotObserved, nil
	case PostconditionNotObserved, PostconditionObserved, PostconditionMissing,
		PostconditionFailed, PostconditionUnknown:
		return summary, nil
	default:
		return "", fmt.Errorf("relation postcondition summary %q is unsupported", value)
	}
}

// SummarizeObservation bounds a current correlation state for diagnostic persistence.
func SummarizeObservation(state CorrelationState) ObservationSummary {
	switch state {
	case StateExactCorrelation:
		return ObservationPresent
	case StateMissing:
		return ObservationMissing
	case StateUnkeyedSameSubject,
		StateSameSubjectShadow,
		StateManagedKeyDrift,
		StateAmbiguous,
		StateStaleEvidence,
		StateUnsupported,
		StateUnavailableEvidence:
		return ObservationUnknown
	default:
		return ObservationNotObserved
	}
}

// SummarizePostcondition bounds a current correlation state as operation evidence.
func SummarizePostcondition(state CorrelationState) PostconditionSummary {
	switch state {
	case StateExactCorrelation:
		return PostconditionObserved
	case StateMissing:
		return PostconditionMissing
	case StateUnkeyedSameSubject,
		StateSameSubjectShadow,
		StateManagedKeyDrift,
		StateAmbiguous,
		StateStaleEvidence,
		StateUnsupported,
		StateUnavailableEvidence:
		return PostconditionUnknown
	default:
		return PostconditionNotObserved
	}
}
