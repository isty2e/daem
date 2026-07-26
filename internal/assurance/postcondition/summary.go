// Package postcondition owns bounded route-coupled postcondition summaries
// shared by current assessment, durable history, codecs, and presentation.
package postcondition

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/realization/effectpostcondition"
)

// SummaryState is the bounded history-safe state of one required effect fact.
type SummaryState string

const (
	SummaryNotObserved   SummaryState = "not_observed"
	SummarySatisfied     SummaryState = "satisfied"
	SummaryUnsatisfied   SummaryState = "unsatisfied"
	SummaryUnavailable   SummaryState = "unavailable"
	SummaryStale         SummaryState = "stale"
	SummaryMalformed     SummaryState = "malformed"
	SummaryUnsafe        SummaryState = "unsafe"
	SummaryContradictory SummaryState = "contradictory"
)

// Summary is one bounded requirement/state pair safe for durable diagnostics.
type Summary struct {
	requirement effectpostcondition.Requirement
	state       SummaryState
}

// NewSummary validates one history-safe effect-postcondition summary.
func NewSummary(
	requirement effectpostcondition.Requirement,
	state SummaryState,
) (Summary, error) {
	if _, err := effectpostcondition.NewSet([]effectpostcondition.Requirement{requirement}); err != nil {
		return Summary{}, err
	}
	switch state {
	case SummaryNotObserved,
		SummarySatisfied,
		SummaryUnsatisfied,
		SummaryUnavailable,
		SummaryStale,
		SummaryMalformed,
		SummaryUnsafe,
		SummaryContradictory:
	default:
		return Summary{}, fmt.Errorf("effect postcondition summary state %q is unsupported", state)
	}
	return Summary{requirement: requirement, state: state}, nil
}

// Requirement returns the exact locked predicate summarized.
func (summary Summary) Requirement() effectpostcondition.Requirement {
	return summary.requirement
}

// State returns the bounded evidence summary.
func (summary Summary) State() SummaryState {
	return summary.state
}

// Equal reports exact summary equality.
func (summary Summary) Equal(other Summary) bool {
	return summary == other
}

func (summary Summary) validate() error {
	expected, err := NewSummary(summary.requirement, summary.state)
	if err != nil {
		return err
	}
	if summary != expected {
		return fmt.Errorf("effect postcondition summary is not canonical")
	}
	return nil
}

// SummarySet is one canonical sorted unique collection of bounded summaries.
// Its zero value is the explicit empty set used by relation-only routes.
type SummarySet struct {
	summaries []Summary
}

// NewSummarySet validates, sorts, and defensively copies bounded summaries.
func NewSummarySet(values []Summary) (SummarySet, error) {
	summaries := append([]Summary(nil), values...)
	for index, summary := range summaries {
		if err := summary.validate(); err != nil {
			return SummarySet{}, fmt.Errorf(
				"effect postcondition summary[%d]: %w",
				index,
				err,
			)
		}
	}
	sort.Slice(summaries, func(left int, right int) bool {
		return summaries[left].requirement < summaries[right].requirement
	})
	for index := 1; index < len(summaries); index++ {
		if summaries[index-1].requirement == summaries[index].requirement {
			return SummarySet{}, fmt.Errorf(
				"effect postcondition summary for %q is duplicated",
				summaries[index].requirement,
			)
		}
	}
	return SummarySet{summaries: summaries}, nil
}

// Validate rejects forged or non-canonical summary sets.
func (set SummarySet) Validate() error {
	expected, err := NewSummarySet(set.summaries)
	if err != nil {
		return err
	}
	if !set.Equal(expected) {
		return fmt.Errorf("effect postcondition summary set is not canonical")
	}
	return nil
}

// Summaries returns a defensive copy in canonical requirement order.
func (set SummarySet) Summaries() []Summary {
	return append([]Summary(nil), set.summaries...)
}

// Equal reports exact canonical summary equality.
func (set SummarySet) Equal(other SummarySet) bool {
	if len(set.summaries) != len(other.summaries) {
		return false
	}
	for index := range set.summaries {
		if !set.summaries[index].Equal(other.summaries[index]) {
			return false
		}
	}
	return true
}
