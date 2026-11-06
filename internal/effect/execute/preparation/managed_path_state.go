package preparation

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/topology"
)

func AfterManagedPaths(snapshot durable.Snapshot, effects []ManagedPathEffect) (durable.Snapshot, error) {
	replacements := make(map[topology.SubjectID]durable.ManagedPathState, len(effects))
	removals := make(map[topology.SubjectID]struct{}, len(effects))
	for index, effect := range effects {
		if err := effect.Validate(); err != nil {
			return durable.Snapshot{}, fmt.Errorf("managed path effect[%d]: %w", index, err)
		}
		if previous, present := effect.PreviousState(); present {
			removals[previous.Subject()] = struct{}{}
		}
		if effect.Kind() == ManagedPathEffectRemove {
			continue
		}
		if _, duplicate := replacements[effect.Subject()]; duplicate {
			return durable.Snapshot{}, fmt.Errorf("duplicate managed path state replacement for subject %q", effect.Subject())
		}
		state, err := durable.NewManagedPathState(
			effect.Subject(), effect.ConsumerTargets(), effect.Scope(), effect.Destination(), effect.DesiredHash(),
			effect.ContentKind(), effect.PermissionPolicy(), effect.StateFileMode(),
		)
		if err != nil {
			return durable.Snapshot{}, fmt.Errorf("managed path effect[%d] state: %w", index, err)
		}
		replacements[effect.Subject()] = state
		delete(removals, effect.Subject())
	}

	current := snapshot.ManagedPaths()
	next := make([]durable.ManagedPathState, 0, len(current)+len(replacements))
	for _, existing := range current {
		subject := existing.Subject()
		if replacement, replace := replacements[subject]; replace {
			next = append(next, replacement)
			delete(replacements, subject)
			continue
		}
		if _, remove := removals[subject]; remove {
			continue
		}
		next = append(next, existing)
	}
	for _, replacement := range replacements {
		next = append(next, replacement)
	}
	return snapshot.WithManagedPaths(next)
}
