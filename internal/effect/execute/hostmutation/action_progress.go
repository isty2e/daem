package hostmutation

import (
	"fmt"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
)

type ActionProgress struct {
	states []EffectProgress
}

func NewActionProgress(actionCount int) ActionProgress {
	return ActionProgress{states: make([]EffectProgress, actionCount)}
}

func CombineActionProgress(parts ...ActionProgress) ActionProgress {
	count := 0
	for _, part := range parts {
		count += len(part.states)
	}
	combined := ActionProgress{states: make([]EffectProgress, 0, count)}
	for _, part := range parts {
		combined.states = append(combined.states, part.states...)
	}
	return combined
}

func (progress ActionProgress) record(index int, state EffectProgress) {
	progress.states[index] = state
}

func (progress ActionProgress) RequiresRecovery() bool {
	for _, state := range progress.states {
		if state.RequiresRecovery() {
			return true
		}
	}
	return false
}

func (progress ActionProgress) RollbackEntries(selections []journalrecord.EntrySelection) ([]journalrecord.EntrySelection, error) {
	if len(progress.states) != len(selections) {
		return nil, fmt.Errorf("host action progress count %d does not match entry selection count %d", len(progress.states), len(selections))
	}
	selected := make([]journalrecord.EntrySelection, 0, len(selections))
	for index, state := range progress.states {
		if state.RollbackEligible() {
			selected = append(selected, selections[index])
		}
	}
	return selected, nil
}
