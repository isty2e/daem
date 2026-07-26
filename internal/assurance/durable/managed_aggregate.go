package durable

import (
	"fmt"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
)

// ManagedAggregateState is the durable baseline for one subject contribution.
type ManagedAggregateState struct {
	subject      topology.SubjectID
	contribution aggregate.ManagedContribution
}

// NewManagedAggregateState constructs one validated aggregate baseline.
func NewManagedAggregateState(
	subject topology.SubjectID,
	contribution aggregate.ManagedContribution,
) (ManagedAggregateState, error) {
	if err := subject.Validate(); err != nil {
		return ManagedAggregateState{}, fmt.Errorf("managed aggregate state subject: %w", err)
	}
	if subject.Kind() != topology.SubjectProjection {
		return ManagedAggregateState{}, fmt.Errorf("managed aggregate state requires projection subject")
	}
	if err := contribution.Validate(); err != nil {
		return ManagedAggregateState{}, err
	}
	if err := aggregate.ValidateSubjectContract(subject, contribution.Contract()); err != nil {
		return ManagedAggregateState{}, err
	}
	return ManagedAggregateState{
		subject:      subject,
		contribution: contribution.Clone(),
	}, nil
}

// Subject returns the projection subject that owns this contribution.
func (state ManagedAggregateState) Subject() topology.SubjectID { return state.subject }

// Contribution returns a defensive copy of the persisted contribution.
func (state ManagedAggregateState) Contribution() aggregate.ManagedContribution {
	return state.contribution.Clone()
}

// Equal reports semantic equality between aggregate baselines.
func (state ManagedAggregateState) Equal(other ManagedAggregateState) bool {
	return state.subject == other.subject && state.contribution.Equal(other.contribution)
}
