package hostmutation

import (
	"fmt"
	"iter"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
)

type ManagedPathPhase uint8

const (
	ManagedPathPublishPhase ManagedPathPhase = iota + 1
	ManagedPathRetirePhase
)

type ManagedPathOperation struct {
	effect        effectpreparation.ManagedPathEffect
	effectIndex   int
	mutationIndex int
	relocation    bool
	phase         ManagedPathPhase
}

func (operation ManagedPathOperation) Effect() effectpreparation.ManagedPathEffect {
	return operation.effect
}

func (operation ManagedPathOperation) EffectIndex() int { return operation.effectIndex }

func (operation ManagedPathOperation) MutationIndex() int { return operation.mutationIndex }

func (operation ManagedPathOperation) Relocates() bool { return operation.relocation }

// ManagedPathSchedule retains publication order, not execution authority.
type ManagedPathSchedule struct {
	publish       []ManagedPathOperation
	retire        []ManagedPathOperation
	mutationCount int
}

func PlanManagedPaths(effects []effectpreparation.ManagedPathEffect) (ManagedPathSchedule, error) {
	mutations, err := effectpreparation.ManagedPathJournalMutations(effects)
	if err != nil {
		return ManagedPathSchedule{}, err
	}
	schedule := ManagedPathSchedule{mutationCount: len(mutations)}
	mutationIndex := 0
	for effectIndex, effect := range effects {
		operation := ManagedPathOperation{
			effect: effect, effectIndex: effectIndex, mutationIndex: mutationIndex, phase: ManagedPathPublishPhase,
		}
		if effect.Kind() == effectpreparation.ManagedPathEffectReplace {
			previous, _ := effect.PreviousState()
			if previous.Scope() != effect.Scope() || previous.Destination() != effect.Destination() {
				operation.relocation = true
				schedule.publish = append(schedule.publish, operation)
				operation.mutationIndex++
				operation.phase = ManagedPathRetirePhase
				schedule.retire = append(schedule.retire, operation)
				mutationIndex += 2
				continue
			}
		}
		if effect.Kind() == effectpreparation.ManagedPathEffectRemove {
			operation.phase = ManagedPathRetirePhase
			schedule.retire = append(schedule.retire, operation)
		} else {
			schedule.publish = append(schedule.publish, operation)
		}
		mutationIndex++
	}
	if mutationIndex != len(mutations) {
		return ManagedPathSchedule{}, fmt.Errorf("managed path effect expansion produced %d operations, want %d", mutationIndex, len(mutations))
	}
	return schedule, nil
}

func (schedule ManagedPathSchedule) Operations(phase ManagedPathPhase) iter.Seq[ManagedPathOperation] {
	return func(yield func(ManagedPathOperation) bool) {
		operations := schedule.publish
		if phase == ManagedPathRetirePhase {
			operations = schedule.retire
		}
		for _, operation := range operations {
			if !yield(operation) {
				return
			}
		}
	}
}

func (schedule ManagedPathSchedule) MutationCount() int { return schedule.mutationCount }

func (schedule ManagedPathSchedule) WriteCount() int {
	count := 0
	for _, phase := range []ManagedPathPhase{ManagedPathPublishPhase, ManagedPathRetirePhase} {
		for operation := range schedule.Operations(phase) {
			if operation.effect.Kind() != effectpreparation.ManagedPathEffectRecord {
				count++
			}
		}
	}
	return count
}
