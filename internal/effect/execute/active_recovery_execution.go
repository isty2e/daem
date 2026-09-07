package execute

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/operationplan"
)

type activeRecoveryHostVisit struct {
	index int
}

type activeRecoveryHostExecution struct {
	expected []activeRecoveryHostVisit
	visited  int
}

type activeRecoveryExecution struct {
	cursor         *operationplan.EffectCursor
	input          activeRecoveryScheduleInput
	expectedPlan   recovery.Plan
	planBound      bool
	terminal       bool
	awaitingClose  string
	host           *activeRecoveryHostExecution
	operationEnded bool
}

func newActiveRecoveryExecutionForPlan(
	plan recovery.Plan,
	caller activeRecoveryCaller,
	hasBeforeRetirement bool,
) (*activeRecoveryExecution, error) {
	input := activeRecoveryScheduleInputForPlan(
		plan,
		caller,
		hasBeforeRetirement,
	)
	structure, err := compileActiveRecoveryStructure(input)
	if err != nil {
		return nil, err
	}
	return &activeRecoveryExecution{
		cursor:       structure.Begin(),
		input:        input,
		expectedPlan: plan,
		planBound:    true,
	}, nil
}

func (execution *activeRecoveryExecution) requirePlan(plan recovery.Plan) error {
	if execution == nil || !execution.planBound {
		return fmt.Errorf("active recovery execution plan authority is unavailable")
	}
	if !execution.expectedPlan.SameExecutionAuthority(plan) {
		return fmt.Errorf("active recovery execution plan authority changed")
	}
	return nil
}

func (execution *activeRecoveryExecution) runTerminalStep(
	step activeRecoveryStep,
	action func() error,
) error {
	if err := execution.consumeStep(step); err != nil {
		return err
	}
	operationErr := action()
	if err := execution.selectOutcome(step.id+"/outcome", operationErr == nil); err != nil {
		return errors.Join(operationErr, err)
	}
	if operationErr != nil {
		return errors.Join(
			operationErr,
			execution.enterTerminal(step.id+"/failure"),
		)
	}
	return nil
}

func (execution *activeRecoveryExecution) runBranchingStep(
	step activeRecoveryStep,
	action func() error,
) error {
	if err := execution.consumeStep(step); err != nil {
		return err
	}
	operationErr := action()
	return errors.Join(
		operationErr,
		execution.selectOutcome(step.id+"/outcome", operationErr == nil),
	)
}

func (execution *activeRecoveryExecution) beginHostBatch(
	expected []activeRecoveryHostVisit,
) error {
	if execution == nil || execution.cursor == nil {
		return fmt.Errorf("active recovery execution is unavailable")
	}
	if execution.host != nil {
		return fmt.Errorf("active recovery host batch is already active")
	}
	if err := validateActiveRecoveryHostVisits(expected, execution.input.hostActionCount); err != nil {
		return err
	}
	hostBatchKind := operationplan.EffectStepNoOp
	if execution.input.hostActionCount != 0 {
		hostBatchKind = operationplan.EffectStepExternal
	}
	if err := execution.cursor.Consume(
		"active-recovery/rollback/host-batch",
		hostBatchKind,
	); err != nil {
		return err
	}
	execution.host = &activeRecoveryHostExecution{
		expected: append([]activeRecoveryHostVisit(nil), expected...),
	}
	return nil
}

func validateActiveRecoveryHostVisits(
	expected []activeRecoveryHostVisit,
	scheduled int,
) error {
	if len(expected) != scheduled {
		return fmt.Errorf(
			"active recovery host visit count %d does not match scheduled count %d",
			len(expected),
			scheduled,
		)
	}
	seen := make([]bool, scheduled)
	for position, visit := range expected {
		if visit.index < 0 || visit.index >= scheduled {
			return fmt.Errorf(
				"active recovery host visit %d has out-of-range action index %d",
				position,
				visit.index,
			)
		}
		if seen[visit.index] {
			return fmt.Errorf(
				"active recovery host visit %d repeats action index %d",
				position,
				visit.index,
			)
		}
		seen[visit.index] = true
	}
	return nil
}

func (execution *activeRecoveryExecution) visitHostAction(index int) error {
	if execution == nil || execution.host == nil {
		return fmt.Errorf("active recovery host batch is unavailable")
	}
	if execution.host.visited >= len(execution.host.expected) {
		return fmt.Errorf("active recovery host batch has no remaining action visit")
	}
	expected := execution.host.expected[execution.host.visited]
	if index != expected.index {
		return fmt.Errorf(
			"active recovery host action visit is %d, not %d",
			index,
			expected.index,
		)
	}
	if err := execution.cursor.SelectAlternative(activeRecoveryHostVisitChoice, 1); err != nil {
		return err
	}
	if err := execution.cursor.Consume(
		activeRecoveryHostVisitStep,
		operationplan.EffectStepObservation,
	); err != nil {
		return err
	}
	execution.host.visited++
	return nil
}

func (execution *activeRecoveryExecution) settleHostBatch(operationErr error) error {
	if execution == nil || execution.host == nil {
		return errors.Join(
			operationErr,
			fmt.Errorf("active recovery host batch is unavailable"),
		)
	}
	if operationErr == nil && execution.host.visited != len(execution.host.expected) {
		operationErr = fmt.Errorf(
			"active recovery host batch visited %d of %d actions",
			execution.host.visited,
			len(execution.host.expected),
		)
	}
	for execution.host.visited < len(execution.host.expected) {
		if err := execution.cursor.SelectAlternative(activeRecoveryHostVisitChoice, 0); err != nil {
			return errors.Join(operationErr, err)
		}
		if err := execution.cursor.Consume(
			activeRecoveryHostVisitSkipped,
			operationplan.EffectStepNoOp,
		); err != nil {
			return errors.Join(operationErr, err)
		}
		execution.host.visited++
	}
	execution.host = nil
	return errors.Join(
		operationErr,
		execution.selectOutcome(activeRecoveryHostOutcome, operationErr == nil),
	)
}

func (execution *activeRecoveryExecution) runContinuingOutcomeStep(
	step activeRecoveryStep,
	prefix string,
	action func() error,
) error {
	if err := execution.consumeStep(step); err != nil {
		return err
	}
	operationErr := action()
	if err := execution.selectOutcome(prefix+"/outcome", operationErr == nil); err != nil {
		return errors.Join(operationErr, err)
	}
	outcome := "failed"
	if operationErr == nil {
		outcome = "succeeded"
	}
	return errors.Join(
		operationErr,
		execution.cursor.Consume(prefix+"/"+outcome, operationplan.EffectStepNoOp),
	)
}

func (execution *activeRecoveryExecution) runFailureCleanup(
	prefix string,
	action func() error,
) error {
	step := activeRecoveryStep{id: prefix + "/cleanup", kind: operationplan.EffectStepCleanup}
	if err := execution.consumeStep(step); err != nil {
		return err
	}
	operationErr := action()
	if err := execution.selectOutcome(prefix+"/cleanup-outcome", operationErr == nil); err != nil {
		return errors.Join(operationErr, err)
	}
	terminalPrefix := prefix + "/cleanup-failure"
	if operationErr == nil {
		terminalPrefix = prefix + "/completed"
	}
	return errors.Join(operationErr, execution.enterTerminal(terminalPrefix))
}

func (execution *activeRecoveryExecution) finish(
	operationErr error,
	closeAuthority func() error,
) error {
	if execution == nil || execution.cursor == nil {
		closeErr := error(nil)
		if closeAuthority != nil {
			closeErr = closeAuthority()
		}
		return errors.Join(
			operationErr,
			closeErr,
			fmt.Errorf("active recovery execution is unavailable"),
		)
	}
	if execution.operationEnded {
		return errors.Join(
			operationErr,
			fmt.Errorf("active recovery execution is already closed"),
		)
	}
	execution.operationEnded = true

	var structuralErr error
	var closeErr error
	if execution.input.ownsAuthority() {
		switch {
		case execution.awaitingClose != "":
			structuralErr, closeErr = execution.consumeClose(
				execution.awaitingClose,
				closeAuthority,
			)
		case operationErr == nil:
			structuralErr, closeErr = execution.consumeClose(
				activeRecoverySuccessPrefix,
				closeAuthority,
			)
		default:
			if closeAuthority != nil {
				closeErr = closeAuthority()
			}
		}
	} else if operationErr == nil && !execution.terminal {
		structuralErr = execution.enterTerminal(activeRecoverySuccessPrefix)
	}
	structuralErr = errors.Join(structuralErr, execution.cursor.FinishSuccess())
	return errors.Join(operationErr, closeErr, structuralErr)
}

func (execution *activeRecoveryExecution) consumeStep(step activeRecoveryStep) error {
	if execution == nil || execution.cursor == nil {
		return fmt.Errorf("active recovery execution is unavailable")
	}
	if execution.terminal || execution.awaitingClose != "" {
		return fmt.Errorf("active recovery execution already selected a terminal path")
	}
	return execution.cursor.Consume(step.id, step.kind)
}

func (execution *activeRecoveryExecution) selectOutcome(
	choiceID string,
	succeeded bool,
) error {
	alternative := 0
	if succeeded {
		alternative = 1
	}
	return execution.cursor.SelectAlternative(choiceID, alternative)
}

func (execution *activeRecoveryExecution) enterTerminal(prefix string) error {
	if execution.input.ownsAuthority() {
		if execution.awaitingClose != "" {
			return fmt.Errorf("active recovery execution already awaits authority close")
		}
		execution.awaitingClose = prefix
		return nil
	}
	if err := execution.cursor.Consume(
		prefix+"/terminal",
		operationplan.EffectStepTerminal,
	); err != nil {
		return err
	}
	execution.terminal = true
	return nil
}

func (execution *activeRecoveryExecution) consumeClose(
	prefix string,
	closeAuthority func() error,
) (structuralErr error, closeErr error) {
	if closeAuthority == nil {
		return fmt.Errorf("active recovery authority close is unavailable"), nil
	}
	if err := execution.cursor.Consume(
		prefix+"/close-authority",
		operationplan.EffectStepCleanup,
	); err != nil {
		closeErr = closeAuthority()
		return err, closeErr
	}
	closeErr = closeAuthority()
	if err := execution.selectOutcome(prefix+"/close-outcome", closeErr == nil); err != nil {
		return err, closeErr
	}
	terminalID := prefix + "/close-failed"
	if closeErr == nil {
		terminalID = prefix + "/terminal"
	}
	if err := execution.cursor.Consume(terminalID, operationplan.EffectStepTerminal); err != nil {
		return err, closeErr
	}
	execution.awaitingClose = ""
	execution.terminal = true
	return nil, closeErr
}
