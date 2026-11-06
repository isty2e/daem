package execute

import (
	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/operationplan"
)

func ordinaryApplyEffectNodes(builder *operationplan.EffectStructureBuilder, managed managedPathExecutionSchedule, aggregates []effectpreparation.AggregateEffect, promotions *applyprogram.PromotionSchedule) []operationplan.EffectNode {
	var nodes []operationplan.EffectNode
	for _, operation := range managed.publish {
		effect := managed.effects[operation.effectIndex]
		prefix := applyManagedPathScheduleReference(managedPathPublishPhase, operation.effectIndex)
		if effect.Kind() == effectpreparation.ManagedPathEffectRecord {
			nodes = append(nodes, applyprogram.CheckedStep(builder, prefix+"/record", operationplan.EffectStepObservation))
		} else {
			nodes = append(nodes, applyprogram.ForwardObligation(builder, prefix, operationplan.EffectStepPersistence))
		}
		nodes = append(nodes, promotions.ForKeys(builder, provisionalAcquireKeysForManagedPath(effect, managedPathPublishPhase))...)
	}
	for index, effect := range aggregates {
		prefix := applyAggregateScheduleReference(index)
		if effect.Kind() == effectpreparation.AggregateEffectRecord {
			nodes = append(nodes, applyprogram.CheckedStep(builder, prefix+"/record", operationplan.EffectStepObservation))
		} else {
			nodes = append(
				nodes,
				applyprogram.CheckedStep(builder, prefix+"/preconditions", operationplan.EffectStepObservation),
				applyprogram.ForwardObligation(builder, prefix, operationplan.EffectStepPersistence),
			)
		}
		nodes = append(nodes, promotions.ForKeys(builder, provisionalAcquireKeysForAggregate(effect))...)
	}
	for _, operation := range managed.retire {
		if managed.effects[operation.effectIndex].Kind() == effectpreparation.ManagedPathEffectRecord {
			continue
		}
		nodes = append(nodes, applyprogram.ForwardObligation(builder, applyManagedPathScheduleReference(managedPathRetirePhase, operation.effectIndex), operationplan.EffectStepPersistence))
	}
	return nodes
}
