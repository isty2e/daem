package apply

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
)

type applyContinuationPlan struct {
	segment                 operationplan.EffectNode
	structure               operationplan.EffectStructure
	statefileInitiallyBound bool
	carrierRemovals         []applyCarrierScheduleFact
	finalRoutes             []applyRouteScheduleFact
	orderClasses            []applyOrderScheduleFact
	mayReclassifyOrder      bool
	delegates               []applyDelegateScheduleFact
	available               bool
}

func (plan applyContinuationPlan) valid() bool {
	return plan.available
}

func (plan applyContinuationPlan) equal(other applyContinuationPlan) bool {
	return plan.statefileInitiallyBound == other.statefileInitiallyBound &&
		plan.structure.Equal(other.structure)
}

type applyContinuationExecution struct {
	plan                    applyContinuationPlan
	cursor                  *operationplan.EffectCursor
	statefileInitiallyBound bool
	terminal                bool
	finished                bool
}

func newApplyContinuationExecution(
	prepared applyContinuationPlan,
	current applyContinuationPlan,
) (*applyContinuationExecution, error) {
	if !prepared.valid() || !current.valid() {
		return nil, fmt.Errorf("apply continuation plan is unavailable")
	}
	if !prepared.equal(current) {
		return nil, fmt.Errorf("prepared and current apply continuation plans differ")
	}
	return &applyContinuationExecution{
		plan:                    current,
		cursor:                  current.structure.Begin(),
		statefileInitiallyBound: current.statefileInitiallyBound,
	}, nil
}

func (execution *applyContinuationExecution) carrierRemovalReference(
	action carrierabsence.Action,
) (string, error) {
	if execution == nil {
		return "", nil
	}
	for _, fact := range execution.plan.carrierRemovals {
		if fact.action.Claim().ExactEqual(action.Claim()) &&
			fact.action.Decision() == action.Decision() {
			return fact.ref, nil
		}
	}
	return "", fmt.Errorf("apply continuation carrier removal is not scheduled")
}

func (execution *applyContinuationExecution) finalRouteFact(
	action reconcile.RelationAction,
) (applyRouteScheduleFact, error) {
	if execution == nil {
		return applyRouteScheduleFact{}, nil
	}
	for _, fact := range execution.plan.finalRoutes {
		if fact.action.Compare(action) == 0 {
			return fact, nil
		}
	}
	return applyRouteScheduleFact{}, fmt.Errorf("apply continuation final route is not scheduled")
}

func (execution *applyContinuationExecution) orderClassFact(
	classID string,
) (applyOrderScheduleFact, bool, error) {
	if execution == nil {
		return applyOrderScheduleFact{}, false, nil
	}
	for _, fact := range execution.plan.orderClasses {
		if fact.classID == classID {
			return fact, execution.plan.mayReclassifyOrder, nil
		}
	}
	return applyOrderScheduleFact{}, false, fmt.Errorf(
		"apply continuation relation-order class %q is not scheduled",
		classID,
	)
}

func (execution *applyContinuationExecution) delegateFact(
	action reconcile.DelegateAction,
) (applyDelegateScheduleFact, error) {
	if execution == nil {
		return applyDelegateScheduleFact{}, nil
	}
	for _, fact := range execution.plan.delegates {
		if fact.action.Compare(action) == 0 {
			return fact, nil
		}
	}
	return applyDelegateScheduleFact{}, fmt.Errorf("apply continuation delegate is not scheduled")
}

func (execution *applyContinuationExecution) selectAlternative(
	choiceID string,
	alternative int,
) error {
	if execution == nil || execution.cursor == nil {
		return fmt.Errorf("apply continuation execution is unavailable")
	}
	if err := execution.cursor.SelectAlternative(choiceID, alternative); err != nil {
		return fmt.Errorf("select apply continuation choice %q[%d]: %w", choiceID, alternative, err)
	}
	return nil
}

func (execution *applyContinuationExecution) consume(
	stepID string,
	kind operationplan.EffectStepKind,
) error {
	if execution == nil || execution.cursor == nil {
		return fmt.Errorf("apply continuation execution is unavailable")
	}
	if err := execution.cursor.Consume(stepID, kind); err != nil {
		return fmt.Errorf("consume apply continuation step %q/%d: %w", stepID, kind, err)
	}
	return nil
}

func (execution *applyContinuationExecution) consumeForward(stepID string) error {
	if execution == nil || execution.cursor == nil {
		return fmt.Errorf("apply continuation execution is unavailable")
	}
	_, err := execution.cursor.ConsumeForwardEffect(stepID)
	return err
}

func scheduledContinuationCall(
	execution *applyContinuationExecution,
	ref string,
	kind operationplan.EffectStepKind,
	call func() error,
) error {
	if execution == nil {
		return call()
	}
	if err := execution.consume(ref, kind); err != nil {
		return err
	}
	callErr := call()
	return errors.Join(callErr, execution.settleFailFast(ref+"/outcome", callErr))
}

func scheduledContinuationForwardCall(
	execution *applyContinuationExecution,
	ref string,
	call func() error,
) error {
	if execution == nil {
		return call()
	}
	if err := execution.consumeForward(ref); err != nil {
		return err
	}
	callErr := call()
	return errors.Join(callErr, execution.settleFailFast(ref+"/outcome", callErr))
}

func scheduledContinuationOptionalCall(
	execution *applyContinuationExecution,
	ref string,
	kind operationplan.EffectStepKind,
	enabled bool,
	call func() error,
) (bool, error) {
	if execution == nil {
		if !enabled {
			return false, nil
		}
		return true, call()
	}
	alternative := 1
	stepID := ref + "/skipped"
	stepKind := operationplan.EffectStepNoOp
	if enabled {
		alternative = 0
		stepID = ref
		stepKind = kind
	}
	if err := execution.selectAlternative(ref+"/execution", alternative); err != nil {
		return false, err
	}
	if enabled && kind == operationplan.EffectStepForwardEffect {
		if err := execution.consumeForward(stepID); err != nil {
			return false, err
		}
	} else if err := execution.consume(stepID, stepKind); err != nil {
		return false, err
	}
	if !enabled {
		return false, nil
	}
	return true, call()
}

func scheduledContinuationCleanup(
	execution *applyContinuationExecution,
	ref string,
	cleanup func() error,
) (bool, error) {
	attempted := false
	err := scheduledContinuationCall(
		execution,
		ref,
		operationplan.EffectStepCleanup,
		func() error {
			attempted = true
			return cleanup()
		},
	)
	return attempted, err
}

func (execution *applyContinuationExecution) consumeTerminal(stepID string) error {
	if execution == nil {
		return nil
	}
	if err := execution.consume(stepID, operationplan.EffectStepTerminal); err != nil {
		return err
	}
	execution.terminal = true
	return nil
}

func (execution *applyContinuationExecution) settleFailFast(ref string, cause error) error {
	if execution == nil {
		return fmt.Errorf("apply continuation execution is unavailable")
	}
	alternative := 0
	stepID := ref + "/success"
	kind := operationplan.EffectStepNoOp
	if cause != nil {
		alternative = 1
		stepID = ref + "/failure"
		kind = operationplan.EffectStepTerminal
	}
	if err := execution.selectAlternative(ref, alternative); err != nil {
		return err
	}
	if err := execution.consume(stepID, kind); err != nil {
		return err
	}
	if cause != nil {
		execution.terminal = true
	}
	return nil
}

func (execution *applyContinuationExecution) settleContinuing(
	ref string,
	outcome applyContinuationOutcome,
) error {
	if execution == nil {
		return fmt.Errorf("apply continuation execution is unavailable")
	}
	alternative := int(outcome)
	stepID := ref + "/success"
	kind := operationplan.EffectStepNoOp
	switch outcome {
	case applyContinuationSucceeded:
	case applyContinuationOrdinaryFailure:
		stepID = ref + "/ordinary"
	case applyContinuationTerminalFailure:
		stepID = ref + "/failure"
		kind = operationplan.EffectStepTerminal
	default:
		return fmt.Errorf("apply continuation outcome %d is invalid", outcome)
	}
	if err := execution.selectAlternative(ref, alternative); err != nil {
		return err
	}
	if err := execution.consume(stepID, kind); err != nil {
		return err
	}
	if outcome == applyContinuationTerminalFailure {
		execution.terminal = true
	}
	return nil
}

func (execution *applyContinuationExecution) finish(resultErr error) error {
	if execution == nil || execution.cursor == nil {
		return errors.Join(resultErr, fmt.Errorf("apply continuation execution is unavailable"))
	}
	if execution.finished {
		return errors.Join(resultErr, fmt.Errorf("apply continuation execution is already finished"))
	}
	var finishErr error
	if execution.terminal {
		finishErr = execution.cursor.FinishSuccess()
	} else if resultErr == nil {
		finishErr = execution.cursor.FinishSuccess()
	} else {
		finishErr = execution.cursor.AbortBeforeEffect()
	}
	if finishErr == nil {
		execution.finished = true
	}
	return errors.Join(resultErr, finishErr)
}

type applyContinuationOutcome uint8

const (
	applyContinuationSucceeded applyContinuationOutcome = iota
	applyContinuationOrdinaryFailure
	applyContinuationTerminalFailure
)
