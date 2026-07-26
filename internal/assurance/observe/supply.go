package observe

import (
	"fmt"

	"github.com/isty2e/daem/internal/topology"
)

// ExactSupplyObservation records fresh source verification for one exact-Supply
// resource subject. It carries no desired, realization, or host-path fact.
type ExactSupplyObservation struct {
	subject topology.SubjectID
	stale   bool
}

// NewExactSupplyObservation constructs one subject-keyed Supply observation.
func NewExactSupplyObservation(subject topology.SubjectID, stale bool) (ExactSupplyObservation, error) {
	if err := subject.Validate(); err != nil {
		return ExactSupplyObservation{}, fmt.Errorf("exact Supply observation subject: %w", err)
	}
	if subject.Kind() != topology.SubjectResource {
		return ExactSupplyObservation{}, fmt.Errorf("exact Supply observation requires resource subject")
	}
	return ExactSupplyObservation{subject: subject, stale: stale}, nil
}

// Subject returns the exact-Supply resource subject that was observed.
func (observation ExactSupplyObservation) Subject() topology.SubjectID { return observation.subject }

// Stale reports whether fresh source evidence disagrees with the locked Supply.
func (observation ExactSupplyObservation) Stale() bool { return observation.stale }
