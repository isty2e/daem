package hostmutation

import (
	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/operationplan"
)

func ForwardEffectNodes(builder *operationplan.EffectStructureBuilder, managed ManagedPathSchedule, aggregates []effectpreparation.AggregateEffect, promotions *applyprogram.PromotionSchedule) []operationplan.EffectNode {
	var nodes []operationplan.EffectNode
	for operation := range managed.Operations(ManagedPathPublishPhase) {
		effect := operation.Effect()
		prefix := operation.ScheduleReference()
		if effect.Kind() == effectpreparation.ManagedPathEffectRecord {
			nodes = append(nodes, applyprogram.CheckedStep(builder, prefix+"/record", operationplan.EffectStepObservation))
		} else {
			nodes = append(nodes, applyprogram.ForwardObligation(builder, prefix, operationplan.EffectStepPersistence))
		}
		nodes = append(nodes, promotions.ForKeys(builder, ProvisionalAcquireKeysForManagedPath(effect, ManagedPathPublishPhase))...)
	}
	for index, effect := range aggregates {
		prefix := AggregateScheduleReference(index)
		if effect.Kind() == effectpreparation.AggregateEffectRecord {
			nodes = append(nodes, applyprogram.CheckedStep(builder, prefix+"/record", operationplan.EffectStepObservation))
		} else {
			nodes = append(
				nodes,
				applyprogram.CheckedStep(builder, prefix+"/preconditions", operationplan.EffectStepObservation),
				applyprogram.ForwardObligation(builder, prefix, operationplan.EffectStepPersistence),
			)
		}
		nodes = append(nodes, promotions.ForKeys(builder, ProvisionalAcquireKeysForAggregate(effect))...)
	}
	for operation := range managed.Operations(ManagedPathRetirePhase) {
		if operation.Effect().Kind() == effectpreparation.ManagedPathEffectRecord {
			continue
		}
		nodes = append(nodes, applyprogram.ForwardObligation(builder, operation.ScheduleReference(), operationplan.EffectStepPersistence))
	}
	return nodes
}
