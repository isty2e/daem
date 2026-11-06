package execute

import (
	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/hostmutation"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/operationplan"
)

func ordinaryApplyEffectNodes(builder *operationplan.EffectStructureBuilder, managed hostmutation.ManagedPathSchedule, aggregates []effectpreparation.AggregateEffect, promotions *applyprogram.PromotionSchedule) []operationplan.EffectNode {
	var nodes []operationplan.EffectNode
	for operation := range managed.Operations(hostmutation.ManagedPathPublishPhase) {
		effect := operation.Effect()
		prefix := applyManagedPathScheduleReference(hostmutation.ManagedPathPublishPhase, operation.EffectIndex())
		if effect.Kind() == effectpreparation.ManagedPathEffectRecord {
			nodes = append(nodes, applyprogram.CheckedStep(builder, prefix+"/record", operationplan.EffectStepObservation))
		} else {
			nodes = append(nodes, applyprogram.ForwardObligation(builder, prefix, operationplan.EffectStepPersistence))
		}
		nodes = append(nodes, promotions.ForKeys(builder, provisionalAcquireKeysForManagedPath(effect, hostmutation.ManagedPathPublishPhase))...)
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
	for operation := range managed.Operations(hostmutation.ManagedPathRetirePhase) {
		if operation.Effect().Kind() == effectpreparation.ManagedPathEffectRecord {
			continue
		}
		nodes = append(nodes, applyprogram.ForwardObligation(builder, applyManagedPathScheduleReference(hostmutation.ManagedPathRetirePhase, operation.EffectIndex()), operationplan.EffectStepPersistence))
	}
	return nodes
}
