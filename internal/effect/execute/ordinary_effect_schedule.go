package execute

import "github.com/isty2e/daem/internal/operationplan"

func ordinaryApplyEffectNodes(builder *operationplan.EffectStructureBuilder, managed managedPathExecutionSchedule, aggregates []AggregateEffect, promotions *provisionalPromotionSchedule) []operationplan.EffectNode {
	var nodes []operationplan.EffectNode
	for _, operation := range managed.publish {
		effect := managed.effects[operation.effectIndex]
		prefix := applyManagedPathScheduleReference(managedPathPublishPhase, operation.effectIndex)
		if effect.Kind() == ManagedPathEffectRecord {
			nodes = append(nodes, applyCheckedStep(builder, prefix+"/record", operationplan.EffectStepObservation))
		} else {
			nodes = append(nodes, applyForwardObligation(builder, prefix, operationplan.EffectStepPersistence))
		}
		nodes = append(nodes, promotions.forKeys(builder, provisionalAcquireKeysForManagedPath(effect, managedPathPublishPhase))...)
	}
	for index, effect := range aggregates {
		prefix := applyAggregateScheduleReference(index)
		if effect.Kind() == AggregateEffectRecord {
			nodes = append(nodes, applyCheckedStep(builder, prefix+"/record", operationplan.EffectStepObservation))
		} else {
			nodes = append(
				nodes,
				applyCheckedStep(builder, prefix+"/preconditions", operationplan.EffectStepObservation),
				applyForwardObligation(builder, prefix, operationplan.EffectStepPersistence),
			)
		}
		nodes = append(nodes, promotions.forKeys(builder, provisionalAcquireKeysForAggregate(effect))...)
	}
	for _, operation := range managed.retire {
		if managed.effects[operation.effectIndex].Kind() == ManagedPathEffectRecord {
			continue
		}
		nodes = append(nodes, applyForwardObligation(builder, applyManagedPathScheduleReference(managedPathRetirePhase, operation.effectIndex), operationplan.EffectStepPersistence))
	}
	return nodes
}
