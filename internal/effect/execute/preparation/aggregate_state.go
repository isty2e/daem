package preparation

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/topology"
)

func AfterAggregates(snapshot durable.Snapshot, effects []AggregateEffect) (durable.Snapshot, error) {
	removeSubjects := make(map[topology.SubjectID]struct{})
	replacements := make(map[topology.SubjectID]durable.ManagedAggregateState)
	for index, effect := range effects {
		if err := effect.Validate(); err != nil {
			return durable.Snapshot{}, fmt.Errorf("aggregate effect[%d]: %w", index, err)
		}
		for _, previous := range effect.PreviousStates() {
			removeSubjects[previous.Subject()] = struct{}{}
		}
		for _, item := range effect.DesiredContributions() {
			if _, duplicate := replacements[item.SubjectID()]; duplicate {
				return durable.Snapshot{}, fmt.Errorf("duplicate aggregate state replacement for subject %q", item.SubjectID())
			}
			state, err := durable.NewManagedAggregateState(item.SubjectID(), item.Contribution())
			if err != nil {
				return durable.Snapshot{}, fmt.Errorf("aggregate effect[%d] state subject %q: %w", index, item.SubjectID(), err)
			}
			replacements[item.SubjectID()] = state
			delete(removeSubjects, item.SubjectID())
		}
	}

	current := snapshot.ManagedAggregates()
	next := make([]durable.ManagedAggregateState, 0, len(current)+len(replacements))
	for _, existing := range current {
		subject := existing.Subject()
		if replacement, replace := replacements[subject]; replace {
			next = append(next, replacement)
			delete(replacements, subject)
			continue
		}
		if _, remove := removeSubjects[subject]; remove {
			continue
		}
		next = append(next, existing)
	}
	for _, replacement := range replacements {
		next = append(next, replacement)
	}
	return snapshot.WithManagedAggregates(next)
}
