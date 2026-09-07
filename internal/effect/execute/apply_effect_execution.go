package execute

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/operationplan"
)

type applyEffectExecution struct {
	cursor                *operationplan.EffectCursor
	failureStructure      operationplan.EffectStructure
	promotionIndexes      map[ownershipOutputKey]int
	phase                 applyEffectExecutionPhase
	pendingForwardFailure bool
	pendingFailure        applyForwardFailureDisposition
	finished              bool
}

type applyEffectExecutionPhase uint8

const (
	applyEffectExecutionForward applyEffectExecutionPhase = iota
	applyEffectExecutionSettlement
)

type applyForwardFailureDisposition uint8

const (
	applyForwardFailureUnavailable applyForwardFailureDisposition = iota
	applyForwardFailureNoCompensation
	applyForwardFailureClaimPreparation
	applyForwardFailurePreparedEffects
	applyForwardFailureGuardedRecovery
	applyForwardFailureStatefilePublication
)

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
	if prepared.changed != expected.changed ||
		!prepared.structure.Equal(expected.structure) ||
		!prepared.failureStructure.Equal(expected.failureStructure) {
		return nil, fmt.Errorf("prepared and current apply effect plans differ")
	}
	return &applyEffectExecution{
		cursor:           prepared.structure.Begin(),
		failureStructure: expected.failureStructure,
		promotionIndexes: clonePromotionIndexes(expected.promotionIndexes),
		phase:            applyEffectExecutionForward,
	}, nil
}

func (execution *applyEffectExecution) runCheckedStep(
	id string,
	kind operationplan.EffectStepKind,
	failure applyForwardFailureDisposition,
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
		if execution.phase == applyEffectExecutionForward {
			execution.pendingForwardFailure = true
			execution.pendingFailure = failure
		}
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

func (execution *applyEffectExecution) runObservation(
	id string,
	failure applyForwardFailureDisposition,
	action func() error,
) error {
	return execution.runCheckedStep(
		id,
		operationplan.EffectStepObservation,
		failure,
		action,
	)
}

func (execution *applyEffectExecution) visibilityGate(
	base visibilityEffectGate,
	id string,
	settlement operationplan.EffectStepKind,
	failure applyForwardFailureDisposition,
) visibilityEffectGate {
	base.schedule = &applyVisibilityExecution{
		execution:         execution,
		id:                id,
		validation:        operationplan.EffectStepForwardEffect,
		settlement:        settlement,
		validationFailure: failure,
		settlementFailure: failure,
		acceptanceFailure: failure,
	}
	return base
}

func (execution *applyEffectExecution) statefileVisibilityGate(
	base visibilityEffectGate,
) visibilityEffectGate {
	base.schedule = &applyVisibilityExecution{
		execution:         execution,
		id:                "apply/statefile-publication",
		validation:        operationplan.EffectStepForwardEffect,
		settlement:        operationplan.EffectStepPersistence,
		validationFailure: applyForwardFailureGuardedRecovery,
		settlementFailure: applyForwardFailureStatefilePublication,
		acceptanceFailure: applyForwardFailureNoCompensation,
	}
	return base
}

func (execution *applyEffectExecution) compensationVisibilityGate(
	base visibilityEffectGate,
	id string,
	settlement operationplan.EffectStepKind,
) visibilityEffectGate {
	base.schedule = &applyVisibilityExecution{
		execution:         execution,
		id:                id,
		validation:        operationplan.EffectStepObservation,
		settlement:        settlement,
		validationFailure: applyForwardFailureUnavailable,
		settlementFailure: applyForwardFailureUnavailable,
		acceptanceFailure: applyForwardFailureUnavailable,
	}
	return base
}

func (execution *applyEffectExecution) beginFailureSettlement(
	kind applyFailureSettlementKind,
) (string, error) {
	if execution == nil || execution.cursor == nil || execution.finished {
		return "", fmt.Errorf("apply effect execution is unavailable")
	}
	if execution.phase != applyEffectExecutionForward || !execution.pendingForwardFailure {
		return "", fmt.Errorf("apply forward failure has no terminal settlement handoff")
	}
	if !execution.pendingFailure.allows(kind) {
		return "", fmt.Errorf(
			"apply forward failure disposition %d does not permit settlement %q",
			execution.pendingFailure,
			kind.reference(),
		)
	}
	alternative, err := kind.alternative()
	if err != nil {
		return "", err
	}
	settlementCursor := execution.failureStructure.Begin()
	if err := settlementCursor.SelectAlternative(
		applyFailureSettlementChoice,
		alternative,
	); err != nil {
		return "", fmt.Errorf("select apply failure settlement: %w", err)
	}
	if err := execution.cursor.FinishHandoff(); err != nil {
		return "", fmt.Errorf("finish apply forward failure handoff: %w", err)
	}
	execution.cursor = settlementCursor
	execution.phase = applyEffectExecutionSettlement
	execution.pendingForwardFailure = false
	execution.pendingFailure = applyForwardFailureUnavailable
	return kind.reference(), nil
}

func (execution *applyEffectExecution) finishForwardFailure() error {
	if execution == nil || execution.cursor == nil || execution.finished {
		return fmt.Errorf("apply effect execution is unavailable")
	}
	if execution.phase != applyEffectExecutionForward || !execution.pendingForwardFailure {
		return fmt.Errorf("apply forward failure has no terminal settlement handoff")
	}
	if !execution.pendingFailure.allowsNoCompensation() {
		return fmt.Errorf(
			"apply forward failure disposition %d requires an explicit settlement branch",
			execution.pendingFailure,
		)
	}
	if err := execution.cursor.FinishHandoff(); err != nil {
		return fmt.Errorf("finish apply forward failure handoff: %w", err)
	}
	execution.pendingForwardFailure = false
	execution.pendingFailure = applyForwardFailureUnavailable
	execution.finished = true
	return nil
}

func (execution *applyEffectExecution) settleFailureWithoutCompensation(
	primary error,
) error {
	if settlementErr := execution.finishForwardFailure(); settlementErr != nil {
		return errors.Join(primary, settlementErr)
	}
	return primary
}

func (execution *applyEffectExecution) completeFailureSettlement(
	kind applyFailureSettlementKind,
) error {
	if execution == nil || execution.cursor == nil || execution.finished {
		return fmt.Errorf("apply effect execution is unavailable")
	}
	if execution.phase != applyEffectExecutionSettlement {
		return fmt.Errorf("apply failure settlement is not active")
	}
	if err := execution.cursor.Consume(
		kind.reference()+"/complete",
		operationplan.EffectStepTerminal,
	); err != nil {
		return fmt.Errorf("complete apply failure settlement: %w", err)
	}
	if err := execution.cursor.FinishSuccess(); err != nil {
		return fmt.Errorf("finish apply failure settlement: %w", err)
	}
	execution.finished = true
	return nil
}

func (execution *applyEffectExecution) handoffGuardedRecovery() error {
	if execution == nil || execution.cursor == nil || execution.finished {
		return fmt.Errorf("apply effect execution is unavailable")
	}
	if execution.phase != applyEffectExecutionSettlement {
		return fmt.Errorf("apply failure settlement is not active")
	}
	if err := execution.cursor.Consume(
		applyFailureSettlementGuardedRecovery.reference()+"/active-recovery-handoff",
		operationplan.EffectStepTerminal,
	); err != nil {
		return fmt.Errorf("handoff guarded apply recovery: %w", err)
	}
	if err := execution.cursor.FinishSuccess(); err != nil {
		return fmt.Errorf("finish guarded apply recovery handoff: %w", err)
	}
	execution.finished = true
	return nil
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
	if execution.phase == applyEffectExecutionForward && execution.pendingForwardFailure {
		return fmt.Errorf("apply forward failure settlement was not selected")
	}
	if primary != nil {
		if execution.phase == applyEffectExecutionForward {
			if abortErr := execution.cursor.AbortBeforeEffect(); abortErr == nil {
				execution.finished = true
				return nil
			}
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
	execution         *applyEffectExecution
	id                string
	validation        operationplan.EffectStepKind
	settlement        operationplan.EffectStepKind
	validationFailure applyForwardFailureDisposition
	settlementFailure applyForwardFailureDisposition
	acceptanceFailure applyForwardFailureDisposition
}

func (execution *applyVisibilityExecution) validate(action func() error) error {
	if execution == nil || execution.execution == nil {
		return fmt.Errorf("apply visibility validation schedule is unavailable")
	}
	return execution.execution.runCheckedStep(
		execution.id+"/forward",
		execution.validation,
		execution.validationFailure,
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
		execution.settlementFailure,
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
		execution.acceptanceFailure,
		action,
	)
}
