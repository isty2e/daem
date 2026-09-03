package execute

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/operationplan"
)

type journalCleanupStep struct {
	id             string
	kind           operationplan.EffectStepKind
	retirementStep journal.RetirementExecutionStep
}

var _ journal.RetirementStepGate = (*journalCleanupExecution)(nil)

type journalCleanupExecution struct {
	cursor      *operationplan.EffectCursor
	steps       []journalCleanupStep
	next        int
	pending     bool
	closePrefix string
	closed      bool
}

func newJournalCleanupExecution(
	plan retirement.CleanupPlan,
) (*journalCleanupExecution, error) {
	steps, err := journalCleanupSteps(plan)
	if err != nil {
		return nil, err
	}
	return newJournalCleanupExecutionForSteps(steps)
}

func newJournalCleanupExecutionForSteps(
	steps []journalCleanupStep,
) (*journalCleanupExecution, error) {
	structure, err := compileJournalCleanupStructure(steps)
	if err != nil {
		return nil, err
	}
	return &journalCleanupExecution{
		cursor: structure.Begin(),
		steps:  append([]journalCleanupStep(nil), steps...),
	}, nil
}

func journalCleanupSteps(plan retirement.CleanupPlan) ([]journalCleanupStep, error) {
	authority := plan.Authority()
	if _, err := authority.CurrentRecord(); err != nil {
		return nil, err
	}
	return journalCleanupStepsFor(
		authority.RequiresPhaseAdvance(),
		authority.ResiduePresent(),
	), nil
}

func journalCleanupStepsFor(
	requiresPhaseAdvance bool,
	residuePresent bool,
) []journalCleanupStep {
	steps := []journalCleanupStep{
		{
			id:   "journal-cleanup/validate-authority",
			kind: operationplan.EffectStepObservation,
		},
		{
			id:             "journal-cleanup/validate-cleanup-authority",
			kind:           operationplan.EffectStepObservation,
			retirementStep: journal.RetirementStepValidateCleanupAuthority,
		},
		{
			id:             "journal-cleanup/validate-prepared-layout",
			kind:           operationplan.EffectStepObservation,
			retirementStep: journal.RetirementStepValidatePreparedLayout,
		},
	}
	if requiresPhaseAdvance {
		steps = append(
			steps,
			journalCleanupStep{
				id:             "journal-cleanup/validate-phase-advance-layout",
				kind:           operationplan.EffectStepObservation,
				retirementStep: journal.RetirementStepValidatePhaseAdvanceLayout,
			},
			journalCleanupStep{
				id:             "journal-cleanup/advance-record",
				kind:           operationplan.EffectStepPersistence,
				retirementStep: journal.RetirementStepAdvanceRecord,
			},
		)
	}
	steps = append(steps, journalCleanupStep{
		id:             "journal-cleanup/validate-finalizing-layout",
		kind:           operationplan.EffectStepObservation,
		retirementStep: journal.RetirementStepValidateFinalizingLayout,
	})
	if residuePresent {
		steps = append(steps, journalCleanupStep{
			id:             "journal-cleanup/cleanup-residue",
			kind:           operationplan.EffectStepCleanup,
			retirementStep: journal.RetirementStepCleanupResidue,
		})
	}
	return append(
		steps,
		journalCleanupStep{
			id:             "journal-cleanup/retire-control",
			kind:           operationplan.EffectStepRetirement,
			retirementStep: journal.RetirementStepRetireControl,
		},
		journalCleanupStep{
			id:             "journal-cleanup/cleanup-garbage",
			kind:           operationplan.EffectStepCleanup,
			retirementStep: journal.RetirementStepCleanupGarbage,
		},
	)
}

func compileJournalCleanupStructure(
	steps []journalCleanupStep,
) (operationplan.EffectStructure, error) {
	var builder operationplan.EffectStructureBuilder
	var compileFrom func(int) operationplan.EffectNode
	compileFrom = func(index int) operationplan.EffectNode {
		if index == len(steps) {
			return journalCleanupCloseNode(&builder, "journal-cleanup/success")
		}
		step := steps[index]
		return operationplan.EffectSequence(
			builder.Step(step.id, step.kind),
			builder.Choice(
				step.id+"/outcome",
				journalCleanupCloseNode(&builder, step.id+"/failure"),
				compileFrom(index+1),
			),
		)
	}
	return builder.Compile(compileFrom(0))
}

func journalCleanupCloseNode(
	builder *operationplan.EffectStructureBuilder,
	prefix string,
) operationplan.EffectNode {
	closeID := prefix + "/close"
	return operationplan.EffectSequence(
		builder.Step(closeID, operationplan.EffectStepCleanup),
		builder.Choice(
			closeID+"/outcome",
			builder.Step(closeID+"/failed", operationplan.EffectStepTerminal),
			builder.Step(closeID+"/succeeded", operationplan.EffectStepTerminal),
		),
	)
}

func (execution *journalCleanupExecution) validateBeforeEffects(
	validate func() error,
) error {
	if execution == nil || len(execution.steps) == 0 {
		return fmt.Errorf("journal cleanup execution is unavailable")
	}
	step := execution.steps[0]
	if err := execution.admit(step); err != nil {
		return err
	}
	var actionErr error
	if validate != nil {
		actionErr = validate()
	}
	return errors.Join(actionErr, execution.settle(step, actionErr == nil))
}

func (execution *journalCleanupExecution) AdmitRetirementStep(
	step journal.RetirementExecutionStep,
) error {
	if execution == nil || execution.next >= len(execution.steps) {
		return fmt.Errorf("journal cleanup has no remaining retirement step")
	}
	expected := execution.steps[execution.next]
	if expected.retirementStep == 0 || expected.retirementStep != step {
		return fmt.Errorf(
			"journal cleanup retirement step is %d, not %d",
			expected.retirementStep,
			step,
		)
	}
	return execution.admit(expected)
}

func (execution *journalCleanupExecution) SettleRetirementStep(
	step journal.RetirementExecutionStep,
	succeeded bool,
) error {
	if execution == nil || execution.next >= len(execution.steps) {
		return fmt.Errorf("journal cleanup has no retirement step to settle")
	}
	expected := execution.steps[execution.next]
	if expected.retirementStep == 0 || expected.retirementStep != step {
		return fmt.Errorf(
			"journal cleanup retirement settlement is %d, not %d",
			expected.retirementStep,
			step,
		)
	}
	return execution.settle(expected, succeeded)
}

func (execution *journalCleanupExecution) admit(step journalCleanupStep) error {
	if execution == nil || execution.cursor == nil {
		return fmt.Errorf("journal cleanup execution is unavailable")
	}
	if execution.closed {
		return fmt.Errorf("journal cleanup execution is closed")
	}
	if execution.pending {
		return fmt.Errorf("journal cleanup step %q is awaiting settlement", execution.steps[execution.next].id)
	}
	if execution.closePrefix != "" || execution.next >= len(execution.steps) || execution.steps[execution.next] != step {
		return fmt.Errorf("journal cleanup step %q is not the next selected obligation", step.id)
	}
	if err := execution.cursor.Consume(step.id, step.kind); err != nil {
		return err
	}
	execution.pending = true
	return nil
}

func (execution *journalCleanupExecution) settle(
	step journalCleanupStep,
	succeeded bool,
) error {
	if execution == nil || execution.cursor == nil {
		return fmt.Errorf("journal cleanup execution is unavailable")
	}
	if !execution.pending || execution.next >= len(execution.steps) || execution.steps[execution.next] != step {
		return fmt.Errorf("journal cleanup step %q was not admitted", step.id)
	}
	alternative := 0
	if succeeded {
		alternative = 1
	}
	if err := execution.cursor.SelectAlternative(step.id+"/outcome", alternative); err != nil {
		return err
	}
	execution.pending = false
	if succeeded {
		execution.next++
		return nil
	}
	execution.closePrefix = step.id + "/failure"
	return nil
}

func (execution *journalCleanupExecution) abortBeforePreparation() error {
	if execution == nil || execution.cursor == nil {
		return nil
	}
	return execution.cursor.AbortBeforeEffect()
}

func (execution *journalCleanupExecution) close(
	closePrepared func() error,
) error {
	if execution == nil || execution.cursor == nil {
		if closePrepared == nil {
			return nil
		}
		return closePrepared()
	}
	if execution.closed {
		return fmt.Errorf("journal cleanup execution is already closed")
	}
	execution.closed = true
	if closePrepared == nil {
		closePrepared = func() error { return nil }
	}
	if execution.pending || (execution.closePrefix == "" && execution.next != len(execution.steps)) {
		return errors.Join(closePrepared(), execution.cursor.FinishSuccess())
	}
	prefix := execution.closePrefix
	if prefix == "" {
		prefix = "journal-cleanup/success"
	}
	closeID := prefix + "/close"
	consumeErr := execution.cursor.Consume(closeID, operationplan.EffectStepCleanup)
	closeErr := closePrepared()
	if consumeErr != nil {
		return errors.Join(closeErr, consumeErr, execution.cursor.FinishSuccess())
	}
	alternative := 0
	terminalID := closeID + "/failed"
	if closeErr == nil {
		alternative = 1
		terminalID = closeID + "/succeeded"
	}
	settleErr := execution.cursor.SelectAlternative(closeID+"/outcome", alternative)
	if settleErr == nil {
		settleErr = execution.cursor.Consume(terminalID, operationplan.EffectStepTerminal)
	}
	return errors.Join(closeErr, settleErr, execution.cursor.FinishSuccess())
}
