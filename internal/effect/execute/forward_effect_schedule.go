package execute

import (
	"fmt"

	"github.com/isty2e/daem/internal/operationplan"
)

const applyOwnershipPromotionTrigger = "apply/ownership-promotion-finalization"

// ApplyEffectSegment returns Effect-owned apply obligations without assigning a
// StateDir forward phase. The operation workflow composes this segment with its
// later carrier, route, order, delegate, and registry obligations before the
// State Barrier lowers the complete phase.
func ApplyEffectSegment(input ApplyInput) (operationplan.EffectNode, error) {
	transition, err := deriveApplyStateTransition(input)
	if err != nil {
		return operationplan.EffectNode{}, err
	}
	if !transition.changed {
		return operationplan.EffectSequence(), nil
	}
	managedSchedule, err := newManagedPathExecutionSchedule(input.ManagedPathEffects)
	if err != nil {
		return operationplan.EffectNode{}, err
	}
	operationID := forwardEffectPlanningOperationID()
	managedOwnership, err := ownershipPlanForManagedPathEffects(
		input.ManagedPathEffects,
		input.Owner,
		input.Ownership,
		operationID,
	)
	if err != nil {
		return operationplan.EffectNode{}, fmt.Errorf(
			"derive managed path ownership effect schedule: %w",
			err,
		)
	}
	aggregateOwnership, err := ownershipPlanForAggregateEffects(
		input.AggregateEffects,
		input.Owner,
		input.Ownership,
		operationID,
	)
	if err != nil {
		return operationplan.EffectNode{}, fmt.Errorf(
			"derive managed aggregate ownership effect schedule: %w",
			err,
		)
	}
	ownershipState := newOwnershipMutationState(managedOwnership, aggregateOwnership)
	promotions, err := newProvisionalPromotionSchedule(ownershipState)
	if err != nil {
		return operationplan.EffectNode{}, err
	}

	var builder operationplan.EffectStructureBuilder
	nodes := []operationplan.EffectNode{
		applyForwardObligation(
			&builder,
			"apply/journal-publication",
			operationplan.EffectStepPersistence,
		),
	}
	if len(ownershipState.transitions) != 0 {
		nodes = append(nodes, applyForwardObligation(
			&builder,
			"apply/ownership-preparation",
			operationplan.EffectStepPersistence,
		))
	}
	for _, operation := range managedSchedule.publish {
		effect := managedSchedule.effects[operation.effectIndex]
		if effect.Kind() != ManagedPathEffectRecord {
			nodes = append(nodes, applyForwardObligation(
				&builder,
				fmt.Sprintf("apply/managed-publish/%d", operation.effectIndex),
				operationplan.EffectStepPersistence,
			))
		}
		nodes = append(
			nodes,
			promotions.forKeys(
				&builder,
				provisionalAcquireKeysForManagedPath(effect, managedPathPublishPhase),
			)...,
		)
	}
	for index, effect := range input.AggregateEffects {
		if effect.Kind() != AggregateEffectRecord {
			nodes = append(nodes, applyForwardObligation(
				&builder,
				fmt.Sprintf("apply/aggregate/%d", index),
				operationplan.EffectStepPersistence,
			))
		}
		nodes = append(
			nodes,
			promotions.forKeys(
				&builder,
				provisionalAcquireKeysForAggregate(effect),
			)...,
		)
	}
	for _, operation := range managedSchedule.retire {
		if managedSchedule.effects[operation.effectIndex].Kind() == ManagedPathEffectRecord {
			continue
		}
		nodes = append(nodes, applyForwardObligation(
			&builder,
			fmt.Sprintf("apply/managed-retire/%d", operation.effectIndex),
			operationplan.EffectStepPersistence,
		))
	}
	if err := promotions.requireComplete(); err != nil {
		return operationplan.EffectNode{}, err
	}
	nodes = append(nodes, applyForwardObligation(
		&builder,
		"apply/statefile-publication",
		operationplan.EffectStepPersistence,
	))
	switch {
	case len(ownershipState.transitions) != 0:
		nodes = append(nodes, applyForwardObligation(
			&builder,
			"apply/ownership-finalization",
			operationplan.EffectStepPersistence,
		))
	case len(ownershipState.provisional) != 0:
		nodes = append(nodes, builder.Conditional(
			applyOwnershipPromotionTrigger,
			applyForwardObligation(
				&builder,
				"apply/ownership-finalization",
				operationplan.EffectStepPersistence,
			),
		))
	}
	nodes = append(nodes, applyForwardObligation(
		&builder,
		"apply/journal-retirement",
		operationplan.EffectStepRetirement,
	))
	segment := operationplan.EffectSequence(nodes...)
	if _, err := builder.Compile(builder.ForwardPhase("apply-effect-shadow", segment)); err != nil {
		return operationplan.EffectNode{}, err
	}
	return segment, nil
}

func applyForwardObligation(
	builder *operationplan.EffectStructureBuilder,
	id string,
	settlement operationplan.EffectStepKind,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		builder.Step(id+"/forward", operationplan.EffectStepForwardEffect),
		builder.Step(id+"/settlement", settlement),
	)
}

type provisionalPromotionSchedule struct {
	intentByKey map[ownershipOutputKey]int
	keys        []ownershipOutputKey
	scheduled   []bool
	triggered   bool
}

func newProvisionalPromotionSchedule(
	state ownershipMutationState,
) (*provisionalPromotionSchedule, error) {
	schedule := &provisionalPromotionSchedule{
		intentByKey: make(map[ownershipOutputKey]int, len(state.provisional)),
		keys:        make([]ownershipOutputKey, len(state.provisional)),
		scheduled:   make([]bool, len(state.provisional)),
		triggered:   len(state.transitions) == 0 && len(state.provisional) != 0,
	}
	for index, intent := range state.provisional {
		key := ownershipOutputKey{
			destination: intent.Destination(),
			contentPath: intent.ContentPath(),
		}
		if _, duplicate := schedule.intentByKey[key]; duplicate {
			return nil, fmt.Errorf(
				"duplicate provisional ownership effect key %q/%q",
				key.destination,
				key.contentPath,
			)
		}
		schedule.intentByKey[key] = index
		schedule.keys[index] = key
	}
	return schedule, nil
}

func (schedule *provisionalPromotionSchedule) forKeys(
	builder *operationplan.EffectStructureBuilder,
	keys []ownershipOutputKey,
) []operationplan.EffectNode {
	if schedule == nil || len(keys) == 0 {
		return nil
	}
	result := make([]operationplan.EffectNode, 0)
	seen := make(map[ownershipOutputKey]struct{}, len(keys))
	for _, key := range keys {
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		index, present := schedule.intentByKey[key]
		if !present || schedule.scheduled[index] {
			continue
		}
		schedule.scheduled[index] = true
		result = append(result, schedule.choice(builder, index))
	}
	return result
}

func (schedule *provisionalPromotionSchedule) requireComplete() error {
	if schedule == nil {
		return nil
	}
	for index, scheduled := range schedule.scheduled {
		if scheduled {
			continue
		}
		key := schedule.keys[index]
		return fmt.Errorf(
			"provisional ownership effect %q/%q has no producing apply effect",
			key.destination,
			key.contentPath,
		)
	}
	return nil
}

func (schedule *provisionalPromotionSchedule) choice(
	builder *operationplan.EffectStructureBuilder,
	index int,
) operationplan.EffectNode {
	prefix := fmt.Sprintf("apply/ownership-promotion/%d", index)
	active := operationplan.EffectSequence(
		applyForwardObligation(
			builder,
			prefix+"/journal",
			operationplan.EffectStepPersistence,
		),
		applyForwardObligation(
			builder,
			prefix+"/claim",
			operationplan.EffectStepPersistence,
		),
	)
	if schedule.triggered {
		active = builder.Trigger(applyOwnershipPromotionTrigger, active)
	}
	return operationplan.EffectSequence(
		builder.Step(prefix+"/observation", operationplan.EffectStepObservation),
		builder.Choice(
			prefix+"/choice",
			builder.Step(prefix+"/noop", operationplan.EffectStepNoOp),
			active,
		),
	)
}
