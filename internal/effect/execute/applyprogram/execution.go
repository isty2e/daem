package applyprogram

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output/ownership"
)

type Execution struct {
	cursor                *operationplan.EffectCursor
	failureStructure      operationplan.EffectStructure
	promotionIndexes      map[ownership.LogicalKey]int
	phase                 applyEffectExecutionPhase
	pendingForwardFailure bool
	pendingFailure        ForwardFailure
	finished              bool
}

type applyEffectExecutionPhase uint8

const (
	applyEffectExecutionForward applyEffectExecutionPhase = iota
	applyEffectExecutionSettlement
)

type ForwardFailure uint8

const (
	FailureUnavailable ForwardFailure = iota
	FailureNoCompensation
	FailureClaimPreparation
	FailurePreparedEffects
	FailureGuardedRecovery
	FailureStatefilePublication
)

func Begin(
	prepared *Plan,
	expected Plan,
) (*Execution, error) {
	if prepared == nil {
		prepared = &expected
	}
	return newExecution(*prepared, expected)
}

func newExecution(
	prepared Plan,
	expected Plan,
) (*Execution, error) {
	if !prepared.valid {
		return nil, fmt.Errorf("prepared apply effect plan is unavailable")
	}
	if !expected.valid {
		return nil, fmt.Errorf("current apply effect plan is unavailable")
	}
	if prepared.changed != expected.changed ||
		!prepared.SameStructure(expected) ||
		!prepared.SameFailurePolicy(expected) {
		return nil, fmt.Errorf("prepared and current apply effect plans differ")
	}
	return &Execution{
		cursor:           prepared.structure.Begin(),
		failureStructure: expected.failureStructure,
		promotionIndexes: clonePromotionIndexes(expected.promotionIndexes),
		phase:            applyEffectExecutionForward,
	}, nil
}

func (execution *Execution) RunStep(
	id string,
	kind operationplan.EffectStepKind,
	failure ForwardFailure,
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

func (execution *Execution) Observe(
	id string,
	failure ForwardFailure,
	action func() error,
) error {
	return execution.RunStep(
		id,
		operationplan.EffectStepObservation,
		failure,
		action,
	)
}

func (execution *Execution) VisibilityGate(
	base VisibilityGate,
	id string,
	settlement operationplan.EffectStepKind,
	failure ForwardFailure,
) VisibilityGate {
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

func (execution *Execution) StatefileVisibilityGate(
	base VisibilityGate,
) VisibilityGate {
	base.schedule = &applyVisibilityExecution{
		execution:         execution,
		id:                "apply/statefile-publication",
		validation:        operationplan.EffectStepForwardEffect,
		settlement:        operationplan.EffectStepPersistence,
		validationFailure: FailureGuardedRecovery,
		settlementFailure: FailureStatefilePublication,
		acceptanceFailure: FailureNoCompensation,
	}
	return base
}

func (execution *Execution) CompensationVisibilityGate(
	base VisibilityGate,
	id string,
	settlement operationplan.EffectStepKind,
) VisibilityGate {
	base.schedule = &applyVisibilityExecution{
		execution:         execution,
		id:                id,
		validation:        operationplan.EffectStepObservation,
		settlement:        settlement,
		validationFailure: FailureUnavailable,
		settlementFailure: FailureUnavailable,
		acceptanceFailure: FailureUnavailable,
	}
	return base
}

func (execution *Execution) BeginFailureSettlement(
	kind FailureSettlement,
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
			kind.Reference(),
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
	execution.pendingFailure = FailureUnavailable
	return kind.Reference(), nil
}

func (execution *Execution) finishForwardFailure() error {
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
	execution.pendingFailure = FailureUnavailable
	execution.finished = true
	return nil
}

func (execution *Execution) SettleFailureWithoutCompensation(
	primary error,
) error {
	if settlementErr := execution.finishForwardFailure(); settlementErr != nil {
		return errors.Join(primary, settlementErr)
	}
	return primary
}

func (execution *Execution) CompleteFailureSettlement(
	kind FailureSettlement,
) error {
	if execution == nil || execution.cursor == nil || execution.finished {
		return fmt.Errorf("apply effect execution is unavailable")
	}
	if execution.phase != applyEffectExecutionSettlement {
		return fmt.Errorf("apply failure settlement is not active")
	}
	if err := execution.cursor.Consume(
		kind.Reference()+"/complete",
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

func (execution *Execution) HandoffGuardedRecovery() error {
	if execution == nil || execution.cursor == nil || execution.finished {
		return fmt.Errorf("apply effect execution is unavailable")
	}
	if execution.phase != applyEffectExecutionSettlement {
		return fmt.Errorf("apply failure settlement is not active")
	}
	if err := execution.cursor.Consume(
		SettlementGuardedRecovery.Reference()+"/active-recovery-handoff",
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

func (execution *Execution) SelectPromotion(
	key ownership.LogicalKey,
	promoted bool,
) error {
	if execution == nil || execution.cursor == nil || execution.finished {
		return fmt.Errorf("apply effect execution is unavailable")
	}
	index, present := execution.promotionIndexes[key]
	if !present {
		return fmt.Errorf(
			"apply ownership promotion for %q footprint %s is not scheduled",
			key.Destination,
			key.ScopeLabel(),
		)
	}
	prefix := OwnershipPromotionReference(index)
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

func (execution *Execution) PromotionReference(
	key ownership.LogicalKey,
) (string, error) {
	if execution == nil {
		return "", fmt.Errorf("apply effect execution is unavailable")
	}
	index, present := execution.promotionIndexes[key]
	if !present {
		return "", fmt.Errorf(
			"apply ownership promotion for %q footprint %s is not scheduled",
			key.Destination,
			key.ScopeLabel(),
		)
	}
	return OwnershipPromotionReference(index), nil
}

func (execution *Execution) Finish(primary error) error {
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
	execution         *Execution
	id                string
	validation        operationplan.EffectStepKind
	settlement        operationplan.EffectStepKind
	validationFailure ForwardFailure
	settlementFailure ForwardFailure
	acceptanceFailure ForwardFailure
}

func (execution *applyVisibilityExecution) validate(action func() error) error {
	if execution == nil || execution.execution == nil {
		return fmt.Errorf("apply visibility validation schedule is unavailable")
	}
	return execution.execution.RunStep(
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
	return execution.execution.RunStep(
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
	return execution.execution.RunStep(
		execution.id+"/acceptance",
		operationplan.EffectStepObservation,
		execution.acceptanceFailure,
		action,
	)
}
