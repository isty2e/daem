package applyprogram

import (
	"fmt"
	"maps"

	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output/ownership"
)

const OwnershipPromotionTrigger = "apply/ownership-promotion-finalization"

type PromotionSchedule struct {
	intentByKey map[ownership.LogicalKey]int
	keys        []ownership.LogicalKey
	scheduled   []bool
	triggered   bool
}

func ClaimTransitionObligation(
	builder *operationplan.EffectStructureBuilder,
	prefix string,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		CheckedStep(
			builder,
			prefix+"/transition-plan",
			operationplan.EffectStepObservation,
		),
		ForwardObligation(builder, prefix, operationplan.EffectStepPersistence),
	)
}

func ForwardObligation(
	builder *operationplan.EffectStructureBuilder,
	id string,
	settlement operationplan.EffectStepKind,
) operationplan.EffectNode {
	return VisibilityObligation(
		builder,
		id,
		operationplan.EffectStepForwardEffect,
		settlement,
	)
}

func CheckedStep(
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

func JournalRetirementSchedule(
	builder *operationplan.EffectStructureBuilder,
) []operationplan.EffectNode {
	return []operationplan.EffectNode{
		CheckedStep(
			builder,
			"apply/journal-retirement/project-validation",
			operationplan.EffectStepObservation,
		),
		CheckedStep(
			builder,
			"apply/journal-retirement/forward",
			operationplan.EffectStepForwardEffect,
		),
		CheckedStep(
			builder,
			"apply/journal-retirement/reload",
			operationplan.EffectStepObservation,
		),
		CheckedStep(
			builder,
			"apply/journal-retirement/settlement",
			operationplan.EffectStepRetirement,
		),
		CheckedStep(
			builder,
			"apply/journal-retirement/acceptance",
			operationplan.EffectStepObservation,
		),
		CheckedStep(
			builder,
			"apply/journal-retirement/final-project-validation",
			operationplan.EffectStepObservation,
		),
	}
}

func OwnershipPromotionReference(index int) string {
	return fmt.Sprintf("apply/ownership-promotion/%d", index)
}

func clonePromotionIndexes(source map[ownership.LogicalKey]int) map[ownership.LogicalKey]int {
	if len(source) == 0 {
		return nil
	}
	result := make(map[ownership.LogicalKey]int, len(source))
	maps.Copy(result, source)
	return result
}

func NewPromotionSchedule(
	provisional []ownership.ProvisionalAcquireIntent,
	triggered bool,
) (*PromotionSchedule, error) {
	schedule := &PromotionSchedule{
		intentByKey: make(map[ownership.LogicalKey]int, len(provisional)),
		keys:        make([]ownership.LogicalKey, len(provisional)),
		scheduled:   make([]bool, len(provisional)),
		triggered:   triggered,
	}
	for index, intent := range provisional {
		key := ownership.LogicalKey{
			Destination: intent.Destination(),
			Footprint:   intent.Footprint(),
		}
		if _, duplicate := schedule.intentByKey[key]; duplicate {
			return nil, fmt.Errorf(
				"duplicate provisional ownership effect key %q/%s",
				key.Destination,
				key.ScopeLabel(),
			)
		}
		schedule.intentByKey[key] = index
		schedule.keys[index] = key
	}
	return schedule, nil
}

func (schedule *PromotionSchedule) BindPlan(segment operationplan.EffectNode, structure, failure operationplan.EffectStructure) (Plan, error) {
	if schedule == nil {
		return Plan{}, fmt.Errorf("apply promotion schedule is unavailable")
	}
	if err := schedule.RequireComplete(); err != nil {
		return Plan{}, err
	}
	return newPlan(segment, structure, failure, schedule.intentByKey, true)
}

func (schedule *PromotionSchedule) ForKeys(
	builder *operationplan.EffectStructureBuilder,
	keys []ownership.LogicalKey,
) []operationplan.EffectNode {
	if schedule == nil || len(keys) == 0 {
		return nil
	}
	result := make([]operationplan.EffectNode, 0)
	seen := make(map[ownership.LogicalKey]struct{}, len(keys))
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

func (schedule *PromotionSchedule) RequireComplete() error {
	if schedule == nil {
		return nil
	}
	for index, scheduled := range schedule.scheduled {
		if scheduled {
			continue
		}
		key := schedule.keys[index]
		return fmt.Errorf(
			"provisional ownership effect %q/%s has no producing apply effect",
			key.Destination,
			key.ScopeLabel(),
		)
	}
	return nil
}

func (schedule *PromotionSchedule) choice(
	builder *operationplan.EffectStructureBuilder,
	index int,
) operationplan.EffectNode {
	prefix := OwnershipPromotionReference(index)
	active := operationplan.EffectSequence(
		ForwardObligation(
			builder,
			prefix+"/journal",
			operationplan.EffectStepPersistence,
		),
		ForwardObligation(
			builder,
			prefix+"/claim",
			operationplan.EffectStepPersistence,
		),
	)
	if schedule.triggered {
		active = builder.Trigger(OwnershipPromotionTrigger, active)
	}
	return operationplan.EffectSequence(
		CheckedStep(builder, prefix+"/observation", operationplan.EffectStepObservation),
		builder.Choice(
			prefix+"/choice",
			builder.Step(prefix+"/noop", operationplan.EffectStepNoOp),
			active,
		),
	)
}
