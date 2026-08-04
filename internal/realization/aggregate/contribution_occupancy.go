package aggregate

import (
	"fmt"

	"github.com/isty2e/daem/internal/topology"
)

// ContributionOccupancyState classifies whether fresh projection evidence can
// correlate one subject's canonical contribution with physical content.
type ContributionOccupancyState string

const (
	ContributionAbsent    ContributionOccupancyState = "absent"
	ContributionPresent   ContributionOccupancyState = "present"
	ContributionAmbiguous ContributionOccupancyState = "ambiguous"
)

// ContributionOccupancySet is exact subject coverage for one contribution set.
type ContributionOccupancySet struct {
	states map[topology.SubjectID]ContributionOccupancyState
}

// NewUniformContributionOccupancySet assigns one evidence classification to
// every subject in a canonical contribution set.
func NewUniformContributionOccupancySet(
	contributions ContributionSet,
	state ContributionOccupancyState,
) (ContributionOccupancySet, error) {
	states := make(
		map[topology.SubjectID]ContributionOccupancyState,
		len(contributions.items),
	)
	for _, item := range contributions.items {
		states[item.subject] = state
	}
	return NewContributionOccupancySet(contributions, states)
}

// NewContributionOccupancySet validates exact coverage of a canonical
// contribution set and copies the supplied states.
func NewContributionOccupancySet(
	contributions ContributionSet,
	states map[topology.SubjectID]ContributionOccupancyState,
) (ContributionOccupancySet, error) {
	items := contributions.Contributions()
	if len(items) == 0 {
		return ContributionOccupancySet{}, fmt.Errorf("aggregate contribution occupancy requires contributions")
	}
	if len(states) != len(items) {
		return ContributionOccupancySet{}, fmt.Errorf(
			"aggregate contribution occupancy state count = %d, want %d",
			len(states),
			len(items),
		)
	}

	canonical := make(map[topology.SubjectID]ContributionOccupancyState, len(states))
	for _, item := range items {
		subject := item.SubjectID()
		state, present := states[subject]
		if !present {
			return ContributionOccupancySet{}, fmt.Errorf(
				"aggregate contribution occupancy omits subject %q",
				subject,
			)
		}
		if err := validateContributionOccupancyState(state); err != nil {
			return ContributionOccupancySet{}, fmt.Errorf("subject %q: %w", subject, err)
		}
		canonical[subject] = state
	}
	for subject := range states {
		if _, expected := canonical[subject]; !expected {
			return ContributionOccupancySet{}, fmt.Errorf(
				"aggregate contribution occupancy includes unknown subject %q",
				subject,
			)
		}
	}
	return ContributionOccupancySet{states: canonical}, nil
}

// State returns the evidence classification for one covered subject.
func (set ContributionOccupancySet) State(
	subject topology.SubjectID,
) (ContributionOccupancyState, bool) {
	state, present := set.states[subject]
	return state, present
}

func validateContributionOccupancyState(state ContributionOccupancyState) error {
	switch state {
	case ContributionAbsent, ContributionPresent, ContributionAmbiguous:
		return nil
	default:
		return fmt.Errorf("aggregate contribution occupancy state %q is unsupported", state)
	}
}

// Validate rejects zero and unknown occupancy states.
func (state ContributionOccupancyState) Validate() error {
	return validateContributionOccupancyState(state)
}
