package execute

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/operationplan"
)

type applyEffectExecution struct {
	cursor           *operationplan.EffectCursor
	promotionIndexes map[ownershipOutputKey]int
	finished         bool
}

func beginApplyEffectExecution(
	prepared *ApplyEffectPlan,
	expected ApplyEffectPlan,
) (*applyEffectExecution, error) {
	if prepared == nil {
		prepared = &expected
	}
	return newApplyEffectExecution(*prepared, expected)
}

func newApplyEffectExecution(
	prepared ApplyEffectPlan,
	expected ApplyEffectPlan,
) (*applyEffectExecution, error) {
	if !prepared.valid {
		return nil, fmt.Errorf("prepared apply effect plan is unavailable")
	}
	if !expected.valid {
		return nil, fmt.Errorf("current apply effect plan is unavailable")
	}
	if prepared.changed != expected.changed || !prepared.structure.Equal(expected.structure) {
		return nil, fmt.Errorf("prepared and current apply effect plans differ")
	}
	return &applyEffectExecution{
		cursor:           prepared.structure.Begin(),
		promotionIndexes: clonePromotionIndexes(expected.promotionIndexes),
	}, nil
}

func (execution *applyEffectExecution) runCheckedStep(
	id string,
	kind operationplan.EffectStepKind,
	action func() error,
) error {
	if execution == nil || execution.cursor == nil || execution.finished {
		return fmt.Errorf("apply effect execution is unavailable")
	}
	if action == nil {
		return fmt.Errorf("apply effect step %q action is required", id)
	}
	var consumeErr error
	if kind == operationplan.EffectStepForwardEffect {
		_, consumeErr = execution.cursor.ConsumeForwardEffect(id)
	} else {
		consumeErr = execution.cursor.Consume(id, kind)
	}
	if consumeErr != nil {
		return fmt.Errorf("consume apply effect step %q: %w", id, consumeErr)
	}
	actionErr := action()
	alternative := 0
	outcomeID := id + "/outcome/success"
	outcomeKind := operationplan.EffectStepNoOp
	if actionErr != nil {
		alternative = 1
		outcomeID = id + "/outcome/failure"
		outcomeKind = operationplan.EffectStepTerminal
	}
	settleErr := execution.cursor.SelectAlternative(id+"/outcome", alternative)
	if settleErr == nil {
		settleErr = execution.cursor.Consume(outcomeID, outcomeKind)
	}
	if actionErr != nil {
		return errors.Join(actionErr, settleErr)
	}
	return settleErr
}

func (execution *applyEffectExecution) runObservation(id string, action func() error) error {
	return execution.runCheckedStep(id, operationplan.EffectStepObservation, action)
}

func (execution *applyEffectExecution) visibilityGate(
	base visibilityEffectGate,
	id string,
	settlement operationplan.EffectStepKind,
) visibilityEffectGate {
	base.schedule = &applyVisibilityExecution{
		execution:  execution,
		id:         id,
		settlement: settlement,
	}
	return base
}

func (execution *applyEffectExecution) selectPromotion(
	key ownershipOutputKey,
	promoted bool,
) error {
	if execution == nil || execution.cursor == nil || execution.finished {
		return fmt.Errorf("apply effect execution is unavailable")
	}
	index, present := execution.promotionIndexes[key]
	if !present {
		return fmt.Errorf(
			"apply ownership promotion for %q content path %q is not scheduled",
			key.destination,
			key.contentPath,
		)
	}
	prefix := applyOwnershipPromotionScheduleReference(index)
	alternative := 0
	if promoted {
		alternative = 1
	}
	if err := execution.cursor.SelectAlternative(prefix+"/choice", alternative); err != nil {
		return err
	}
	if promoted {
		return nil
	}
	return execution.cursor.Consume(prefix+"/noop", operationplan.EffectStepNoOp)
}

func (execution *applyEffectExecution) promotionReference(
	key ownershipOutputKey,
) (string, error) {
	if execution == nil {
		return "", fmt.Errorf("apply effect execution is unavailable")
	}
	index, present := execution.promotionIndexes[key]
	if !present {
		return "", fmt.Errorf(
			"apply ownership promotion for %q content path %q is not scheduled",
			key.destination,
			key.contentPath,
		)
	}
	return applyOwnershipPromotionScheduleReference(index), nil
}

func (execution *applyEffectExecution) finish(primary error) error {
	if execution == nil || execution.cursor == nil || execution.finished {
		return nil
	}
	if primary != nil {
		if abortErr := execution.cursor.AbortBeforeEffect(); abortErr == nil {
			execution.finished = true
			return nil
		}
	}
	finishErr := execution.cursor.FinishSuccess()
	if finishErr == nil {
		execution.finished = true
		return nil
	}
	if primary == nil {
		return fmt.Errorf("finish apply effect execution: %w", finishErr)
	}
	return fmt.Errorf("apply effect execution remained incomplete after failure: %w", finishErr)
}

type applyVisibilityExecution struct {
	execution  *applyEffectExecution
	id         string
	settlement operationplan.EffectStepKind
}

func (execution *applyVisibilityExecution) validate(action func() error) error {
	if execution == nil || execution.execution == nil {
		return fmt.Errorf("apply visibility validation schedule is unavailable")
	}
	return execution.execution.runCheckedStep(
		execution.id+"/forward",
		operationplan.EffectStepForwardEffect,
		action,
	)
}

func (execution *applyVisibilityExecution) apply(action func() error) error {
	if execution == nil || execution.execution == nil {
		return fmt.Errorf("apply visibility effect schedule is unavailable")
	}
	return execution.execution.runCheckedStep(
		execution.id+"/settlement",
		execution.settlement,
		action,
	)
}

func (execution *applyVisibilityExecution) accept(action func() error) error {
	if execution == nil || execution.execution == nil {
		return fmt.Errorf("apply visibility acceptance schedule is unavailable")
	}
	return execution.execution.runCheckedStep(
		execution.id+"/acceptance",
		operationplan.EffectStepObservation,
		action,
	)
}
