// Package postcondition owns current route-coupled effect evidence and its
// bounded assessment. It does not observe or classify host relations.
package postcondition

import (
	"fmt"
	"sort"

	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/topology"
)

// EvidenceState identifies the current state of one required effect fact.
type EvidenceState string

const (
	EvidenceSatisfied     EvidenceState = "satisfied"
	EvidenceUnsatisfied   EvidenceState = "unsatisfied"
	EvidenceUnavailable   EvidenceState = "unavailable"
	EvidenceStale         EvidenceState = "stale"
	EvidenceMalformed     EvidenceState = "malformed"
	EvidenceUnsafe        EvidenceState = "unsafe"
	EvidenceContradictory EvidenceState = "contradictory"
)

// EvidenceReason is the bounded explanation derived from one evidence state.
type EvidenceReason string

const (
	ReasonObservedAbsent         EvidenceReason = "observed_absent"
	ReasonObservedPresent        EvidenceReason = "observed_present"
	ReasonObservationUnavailable EvidenceReason = "observation_unavailable"
	ReasonObservationStale       EvidenceReason = "observation_stale"
	ReasonObservationMalformed   EvidenceReason = "observation_malformed"
	ReasonObservationUnsafe      EvidenceReason = "observation_unsafe"
	ReasonContradictoryEvidence  EvidenceReason = "contradictory_evidence"
)

// Evidence is one current bounded fact for one route-coupled requirement.
// Host paths and raw adapter diagnostics remain private to the observer.
type Evidence struct {
	requirement effectpostcondition.Requirement
	state       EvidenceState
}

// NewEvidence constructs one current effect-postcondition fact.
func NewEvidence(
	requirement effectpostcondition.Requirement,
	state EvidenceState,
) (Evidence, error) {
	if _, err := effectpostcondition.NewSet([]effectpostcondition.Requirement{requirement}); err != nil {
		return Evidence{}, err
	}
	if _, err := reasonForState(state); err != nil {
		return Evidence{}, err
	}
	return Evidence{requirement: requirement, state: state}, nil
}

// Requirement returns the exact locked predicate this fact addresses.
func (evidence Evidence) Requirement() effectpostcondition.Requirement {
	return evidence.requirement
}

// State returns the current bounded evidence state.
func (evidence Evidence) State() EvidenceState {
	return evidence.state
}

// Reason returns the canonical bounded reason implied by State.
func (evidence Evidence) Reason() EvidenceReason {
	reason, err := reasonForState(evidence.state)
	if err != nil {
		return ""
	}
	return reason
}

func (evidence Evidence) validate() error {
	expected, err := NewEvidence(evidence.requirement, evidence.state)
	if err != nil {
		return err
	}
	if evidence != expected {
		return fmt.Errorf("effect postcondition evidence is not canonical")
	}
	return nil
}

func reasonForState(state EvidenceState) (EvidenceReason, error) {
	switch state {
	case EvidenceSatisfied:
		return ReasonObservedAbsent, nil
	case EvidenceUnsatisfied:
		return ReasonObservedPresent, nil
	case EvidenceUnavailable:
		return ReasonObservationUnavailable, nil
	case EvidenceStale:
		return ReasonObservationStale, nil
	case EvidenceMalformed:
		return ReasonObservationMalformed, nil
	case EvidenceUnsafe:
		return ReasonObservationUnsafe, nil
	case EvidenceContradictory:
		return ReasonContradictoryEvidence, nil
	default:
		return "", fmt.Errorf("effect postcondition evidence state %q is unsupported", state)
	}
}

// SetInput binds current evidence to one exact subject and route request.
type SetInput struct {
	Subject      topology.SubjectID
	RouteRequest realizationdelegate.Request
	Evidence     []Evidence
}

// Set is one exact operation-local evidence collection.
type Set struct {
	subject      topology.SubjectID
	routeRequest realizationdelegate.Request
	evidence     []Evidence
}

// NewSet constructs a canonical identity-bound evidence set.
func NewSet(input SetInput) (Set, error) {
	if err := input.Subject.Validate(); err != nil {
		return Set{}, fmt.Errorf("effect postcondition subject: %w", err)
	}
	if err := input.RouteRequest.Validate(); err != nil {
		return Set{}, fmt.Errorf("effect postcondition route request: %w", err)
	}
	evidence := append([]Evidence(nil), input.Evidence...)
	for index, fact := range evidence {
		if err := fact.validate(); err != nil {
			return Set{}, fmt.Errorf("effect postcondition evidence[%d]: %w", index, err)
		}
	}
	sort.Slice(evidence, func(left int, right int) bool {
		return evidence[left].requirement < evidence[right].requirement
	})
	for index := 1; index < len(evidence); index++ {
		if evidence[index-1].requirement == evidence[index].requirement {
			return Set{}, fmt.Errorf(
				"effect postcondition evidence for %q is duplicated",
				evidence[index].requirement,
			)
		}
	}
	return Set{
		subject:      input.Subject,
		routeRequest: input.RouteRequest,
		evidence:     evidence,
	}, nil
}

// Validate rejects zero, forged, and non-canonical current evidence sets.
func (set Set) Validate() error {
	expected, err := NewSet(SetInput{
		Subject:      set.subject,
		RouteRequest: set.routeRequest,
		Evidence:     set.evidence,
	})
	if err != nil {
		return err
	}
	if !set.Equal(expected) {
		return fmt.Errorf("effect postcondition evidence set is not canonical")
	}
	return nil
}

// Subject returns the exact structural subject observed.
func (set Set) Subject() topology.SubjectID {
	return set.subject
}

// RouteRequest returns the exact operation route observed.
func (set Set) RouteRequest() realizationdelegate.Request {
	return set.routeRequest
}

// Evidence returns a defensive copy in canonical requirement order.
func (set Set) Evidence() []Evidence {
	return append([]Evidence(nil), set.evidence...)
}

// Equal reports complete identity and evidence equality.
func (set Set) Equal(other Set) bool {
	if set.subject != other.subject ||
		!set.routeRequest.Equal(other.routeRequest) ||
		len(set.evidence) != len(other.evidence) {
		return false
	}
	for index := range set.evidence {
		if set.evidence[index] != other.evidence[index] {
			return false
		}
	}
	return true
}
