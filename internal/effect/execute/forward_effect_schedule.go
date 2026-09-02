package execute

import (
	"fmt"

	"github.com/isty2e/daem/internal/operationplan"
)

const applyOwnershipPromotionTrigger = "apply/ownership-promotion-finalization"

// ApplyEffectPlan retains the immutable Effect-owned Apply segment together
// with the exact operation-local cursor structure compiled from that segment.
// It carries no filesystem or mutation authority.
type ApplyEffectPlan struct {
	segment          operationplan.EffectNode
	structure        operationplan.EffectStructure
	failureStructure operationplan.EffectStructure
	promotionIndexes map[ownershipOutputKey]int
	changed          bool
	valid            bool
}

// PrepareApplyEffectPlan compiles the Effect-owned structural Apply plan.
func PrepareApplyEffectPlan(input ApplyInput) (ApplyEffectPlan, error) {
	transition, err := deriveApplyStateTransition(input)
	if err != nil {
		return ApplyEffectPlan{}, err
	}
	if !transition.changed {
		return compileApplyEffectPlan(
			transition,
			managedPathExecutionSchedule{},
			ownershipMutationState{},
			nil,
		)
	}
	managedSchedule, err := newManagedPathExecutionSchedule(input.ManagedPathEffects)
	if err != nil {
		return ApplyEffectPlan{}, err
	}
	operationID := forwardEffectPlanningOperationID()
	managedOwnership, err := ownershipPlanForManagedPathEffects(
		input.ManagedPathEffects,
		input.Owner,
		input.Ownership,
		operationID,
	)
	if err != nil {
		return ApplyEffectPlan{}, fmt.Errorf(
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
		return ApplyEffectPlan{}, fmt.Errorf(
			"derive managed aggregate ownership effect schedule: %w",
			err,
		)
	}
	return compileApplyEffectPlan(
		transition,
		managedSchedule,
		newOwnershipMutationState(managedOwnership, aggregateOwnership),
		input.AggregateEffects,
	)
}

// Segment returns the validated segment for composition into the complete
// operation-owned Apply schedule.
func (plan ApplyEffectPlan) Segment() operationplan.EffectNode {
	return plan.segment
}

func compileApplyEffectPlan(
	transition applyStateTransition,
	managedSchedule managedPathExecutionSchedule,
	ownershipState ownershipMutationState,
	aggregateEffects []AggregateEffect,
) (ApplyEffectPlan, error) {
	var builder operationplan.EffectStructureBuilder
	if !transition.changed {
		segment := operationplan.EffectSequence()
		structure, err := builder.Compile(segment)
		if err != nil {
			return ApplyEffectPlan{}, err
		}
		return ApplyEffectPlan{
			segment:          segment,
			structure:        structure,
			failureStructure: operationplan.EffectStructure{},
			valid:            true,
		}, nil
	}
	failureStructure, err := compileApplyFailureSettlementStructure(
		len(ownershipState.transitions) != 0,
	)
	if err != nil {
		return ApplyEffectPlan{}, err
	}
	promotions, err := newProvisionalPromotionSchedule(ownershipState)
	if err != nil {
		return ApplyEffectPlan{}, err
	}

	nodes := []operationplan.EffectNode{
		applyForwardObligation(
			&builder,
			"apply/journal-publication",
			operationplan.EffectStepPersistence,
		),
		applyCheckedStep(
			&builder,
			"apply/journal-publication/post-validation",
			operationplan.EffectStepObservation,
		),
	}
	if len(ownershipState.transitions) != 0 {
		nodes = append(nodes, applyClaimTransitionObligation(
			&builder,
			"apply/ownership-preparation",
		))
	}
	nodes = append(nodes, applyCheckedStep(
		&builder,
		"apply/prepared-effects-validation",
		operationplan.EffectStepObservation,
	))
	for _, operation := range managedSchedule.publish {
		effect := managedSchedule.effects[operation.effectIndex]
		prefix := applyManagedPathScheduleReference(managedPathPublishPhase, operation.effectIndex)
		if effect.Kind() == ManagedPathEffectRecord {
			nodes = append(nodes, applyCheckedStep(
				&builder,
				prefix+"/record",
				operationplan.EffectStepObservation,
			))
		} else {
			nodes = append(nodes, applyForwardObligation(
				&builder,
				prefix,
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
	for index, effect := range aggregateEffects {
		prefix := applyAggregateScheduleReference(index)
		if effect.Kind() == AggregateEffectRecord {
			nodes = append(nodes, applyCheckedStep(
				&builder,
				prefix+"/record",
				operationplan.EffectStepObservation,
			))
		} else {
			nodes = append(
				nodes,
				applyCheckedStep(
					&builder,
					prefix+"/preconditions",
					operationplan.EffectStepObservation,
				),
				applyForwardObligation(
					&builder,
					prefix,
					operationplan.EffectStepPersistence,
				),
			)
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
			applyManagedPathScheduleReference(managedPathRetirePhase, operation.effectIndex),
			operationplan.EffectStepPersistence,
		))
	}
	if err := promotions.requireComplete(); err != nil {
		return ApplyEffectPlan{}, err
	}
	nodes = append(
		nodes,
		applyCheckedStep(
			&builder,
			"apply/statefile-publication/context-validation",
			operationplan.EffectStepObservation,
		),
		applyCheckedStep(
			&builder,
			"apply/statefile-publication/project-validation",
			operationplan.EffectStepObservation,
		),
		applyForwardObligation(
			&builder,
			"apply/statefile-publication",
			operationplan.EffectStepPersistence,
		),
		applyCheckedStep(
			&builder,
			"apply/post-statefile/project-validation",
			operationplan.EffectStepObservation,
		),
		applyCheckedStep(
			&builder,
			"apply/post-statefile/context-validation",
			operationplan.EffectStepObservation,
		),
	)
	switch {
	case len(ownershipState.transitions) != 0:
		nodes = append(nodes, applyClaimTransitionObligation(
			&builder,
			"apply/ownership-finalization",
		))
	case len(ownershipState.provisional) != 0:
		nodes = append(nodes, builder.Conditional(
			applyOwnershipPromotionTrigger,
			applyClaimTransitionObligation(
				&builder,
				"apply/ownership-finalization",
			),
		))
	}
	nodes = append(nodes, applyJournalRetirementSchedule(&builder)...)
	segment := operationplan.EffectSequence(nodes...)
	structure, err := builder.Compile(builder.ForwardPhase("apply-effect/core", segment))
	if err != nil {
		return ApplyEffectPlan{}, err
	}
	return ApplyEffectPlan{
		segment:          segment,
		structure:        structure,
		failureStructure: failureStructure,
		promotionIndexes: clonePromotionIndexes(promotions.intentByKey),
		changed:          true,
		valid:            true,
	}, nil
}

func applyClaimTransitionObligation(
	builder *operationplan.EffectStructureBuilder,
	prefix string,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		applyCheckedStep(
			builder,
			prefix+"/transition-plan",
			operationplan.EffectStepObservation,
		),
		applyForwardObligation(builder, prefix, operationplan.EffectStepPersistence),
	)
}

func applyForwardObligation(
	builder *operationplan.EffectStructureBuilder,
	id string,
	settlement operationplan.EffectStepKind,
) operationplan.EffectNode {
	return applyVisibilityObligation(
		builder,
		id,
		operationplan.EffectStepForwardEffect,
		settlement,
	)
}

func applyCheckedStep(
	builder *operationplan.EffectStructureBuilder,
	id string,
	kind operationplan.EffectStepKind,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		builder.Step(id, kind),
		builder.Choice(
			id+"/outcome",
			builder.Step(id+"/outcome/success", operationplan.EffectStepNoOp),
			builder.Step(id+"/outcome/failure", operationplan.EffectStepTerminal),
		),
	)
}

func applyJournalRetirementSchedule(
	builder *operationplan.EffectStructureBuilder,
) []operationplan.EffectNode {
	return []operationplan.EffectNode{
		applyCheckedStep(
			builder,
			"apply/journal-retirement/project-validation",
			operationplan.EffectStepObservation,
		),
		applyCheckedStep(
			builder,
			"apply/journal-retirement/forward",
			operationplan.EffectStepForwardEffect,
		),
		applyCheckedStep(
			builder,
			"apply/journal-retirement/reload",
			operationplan.EffectStepObservation,
		),
		applyCheckedStep(
			builder,
			"apply/journal-retirement/settlement",
			operationplan.EffectStepRetirement,
		),
		applyCheckedStep(
			builder,
			"apply/journal-retirement/acceptance",
			operationplan.EffectStepObservation,
		),
		applyCheckedStep(
			builder,
			"apply/journal-retirement/final-project-validation",
			operationplan.EffectStepObservation,
		),
	}
}

func applyManagedPathScheduleReference(phase managedPathPhase, effectIndex int) string {
	prefix := "apply/managed-publish"
	if phase == managedPathRetirePhase {
		prefix = "apply/managed-retire"
	}
	return fmt.Sprintf("%s/%d", prefix, effectIndex)
}

func applyAggregateScheduleReference(index int) string {
	return fmt.Sprintf("apply/aggregate/%d", index)
}

func applyOwnershipPromotionScheduleReference(index int) string {
	return fmt.Sprintf("apply/ownership-promotion/%d", index)
}

func clonePromotionIndexes(source map[ownershipOutputKey]int) map[ownershipOutputKey]int {
	if len(source) == 0 {
		return nil
	}
	result := make(map[ownershipOutputKey]int, len(source))
	for key, index := range source {
		result[key] = index
	}
	return result
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
	prefix := applyOwnershipPromotionScheduleReference(index)
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
		applyCheckedStep(builder, prefix+"/observation", operationplan.EffectStepObservation),
		builder.Choice(
			prefix+"/choice",
			builder.Step(prefix+"/noop", operationplan.EffectStepNoOp),
			active,
		),
	)
}
