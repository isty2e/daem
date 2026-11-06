package recoveryoperation

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/operationplan"
)

const (
	activeRetirementControlModeChoice      = "active-retirement/control-mode"
	activeRetirementControlOutcomeChoice   = "active-retirement/control-outcome"
	activeRetirementControlContinueTrigger = "active-retirement/control-complete"
	activeRetirementControlPresentStep     = "active-retirement/control-present"
	activeRetirementControlPublishStep     = "active-retirement/control-publish"
	activeRetirementControlFailureStep     = "active-retirement/control-failed"
	activeRetirementSuccessStep            = "active-retirement/succeeded"
)

type activeRetirementStep struct {
	id             string
	kind           operationplan.EffectStepKind
	retirementStep RetirementExecutionStep
}

var activeRetirementPreControlSteps = [...]activeRetirementStep{
	{
		id:             "active-retirement/validate-plan",
		kind:           operationplan.EffectStepObservation,
		retirementStep: RetirementStepValidateActivePlan,
	},
	{
		id:             "active-retirement/validate-prepared-layout",
		kind:           operationplan.EffectStepObservation,
		retirementStep: RetirementStepValidatePreparedLayout,
	},
	{
		id:             "active-retirement/validate-active-identity",
		kind:           operationplan.EffectStepObservation,
		retirementStep: RetirementStepValidateActiveIdentity,
	},
}

var activeRetirementPostControlSteps = [...]activeRetirementStep{
	{
		id:             "active-retirement/validate-active-record",
		kind:           operationplan.EffectStepObservation,
		retirementStep: RetirementStepValidateActiveRecord,
	},
	{
		id:             "active-retirement/move-active-to-residue",
		kind:           operationplan.EffectStepRetirement,
		retirementStep: RetirementStepRetireActiveJournal,
	},
	{
		id:             "active-retirement/validate-phase-advance-layout",
		kind:           operationplan.EffectStepObservation,
		retirementStep: RetirementStepValidatePhaseAdvanceLayout,
	},
	{
		id:             "active-retirement/advance-record",
		kind:           operationplan.EffectStepPersistence,
		retirementStep: RetirementStepAdvanceRecord,
	},
	{
		id:             "active-retirement/validate-finalizing-layout",
		kind:           operationplan.EffectStepObservation,
		retirementStep: RetirementStepValidateFinalizingLayout,
	},
	{
		id:             "active-retirement/cleanup-residue",
		kind:           operationplan.EffectStepCleanup,
		retirementStep: RetirementStepCleanupResidue,
	},
	{
		id:             "active-retirement/retire-control",
		kind:           operationplan.EffectStepRetirement,
		retirementStep: RetirementStepRetireControl,
	},
	{
		id:             "active-retirement/cleanup-garbage",
		kind:           operationplan.EffectStepCleanup,
		retirementStep: RetirementStepCleanupGarbage,
	},
}

var _ RetirementStepGate = (*RetirementExecution)(nil)

type activeRetirementPendingStep struct {
	step            activeRetirementStep
	outcomeChoice   string
	failureTerminal string
}

type RetirementExecution struct {
	cursor  *operationplan.EffectCursor
	next    int
	pending *activeRetirementPendingStep
	failed  bool
	closed  bool
}

// CompileRetirementSchedule describes the fixed post-cleanup
// ExecuteActive tail consumed by the runtime gate. It grants no authority.
func CompileRetirementSchedule() (operationplan.EffectStructure, error) {
	var builder operationplan.EffectStructureBuilder
	continuation := builder.Step(activeRetirementSuccessStep, operationplan.EffectStepTerminal)
	for index := len(activeRetirementPostControlSteps) - 1; index >= 0; index-- {
		continuation = activeRetirementStepWithOutcome(
			&builder,
			activeRetirementPostControlSteps[index],
			continuation,
		)
	}
	continuation = operationplan.EffectSequence(
		builder.Choice(
			activeRetirementControlModeChoice,
			builder.Trigger(
				activeRetirementControlContinueTrigger,
				builder.Step(activeRetirementControlPresentStep, operationplan.EffectStepNoOp),
			),
			operationplan.EffectSequence(
				builder.Step(activeRetirementControlPublishStep, operationplan.EffectStepPersistence),
				builder.Choice(
					activeRetirementControlOutcomeChoice,
					builder.Step(activeRetirementControlFailureStep, operationplan.EffectStepTerminal),
					builder.Trigger(
						activeRetirementControlContinueTrigger,
						operationplan.EffectSequence(),
					),
				),
			),
		),
		builder.Conditional(activeRetirementControlContinueTrigger, continuation),
	)
	for index := len(activeRetirementPreControlSteps) - 1; index >= 0; index-- {
		continuation = activeRetirementStepWithOutcome(
			&builder,
			activeRetirementPreControlSteps[index],
			continuation,
		)
	}
	structure, err := builder.Compile(continuation)
	if err != nil {
		return operationplan.EffectStructure{}, err
	}
	demand, err := structure.LegacyDemand()
	if err != nil {
		return operationplan.EffectStructure{}, err
	}
	if demand != (operationplan.Demand{}) {
		return operationplan.EffectStructure{}, fmt.Errorf(
			"active journal retirement schedule has State Barrier demand",
		)
	}
	return structure, nil
}

func activeRetirementStepWithOutcome(
	builder *operationplan.EffectStructureBuilder,
	step activeRetirementStep,
	success operationplan.EffectNode,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		builder.Step(step.id, step.kind),
		builder.Choice(
			step.id+"/outcome",
			builder.Step(step.id+"/failed", operationplan.EffectStepTerminal),
			success,
		),
	)
}

func NewRetirementExecution(
	structure operationplan.EffectStructure,
) *RetirementExecution {
	return &RetirementExecution{cursor: structure.Begin()}
}

func (execution *RetirementExecution) AdmitRetirementStep(
	retirementStep RetirementExecutionStep,
) error {
	if execution == nil || execution.cursor == nil {
		return fmt.Errorf("active journal retirement execution is unavailable")
	}
	if execution.closed {
		return fmt.Errorf("active journal retirement execution is closed")
	}
	if execution.failed {
		return fmt.Errorf("active journal retirement already selected a failure terminal")
	}
	if execution.pending != nil {
		return fmt.Errorf(
			"active journal retirement step %q is awaiting settlement",
			execution.pending.step.id,
		)
	}
	pending, err := execution.pendingStep(retirementStep)
	if err != nil {
		return err
	}
	if err := execution.cursor.Consume(pending.step.id, pending.step.kind); err != nil {
		return err
	}
	execution.pending = &pending
	return nil
}

func (execution *RetirementExecution) SettleRetirementStep(
	retirementStep RetirementExecutionStep,
	succeeded bool,
) error {
	if execution == nil || execution.cursor == nil {
		return fmt.Errorf("active journal retirement execution is unavailable")
	}
	if execution.pending == nil || execution.pending.step.retirementStep != retirementStep {
		return fmt.Errorf(
			"active journal retirement step %d was not admitted",
			retirementStep,
		)
	}
	pending := *execution.pending
	if pending.outcomeChoice == "" {
		if !succeeded {
			return fmt.Errorf(
				"active journal retirement step %q has no failure alternative",
				pending.step.id,
			)
		}
		execution.pending = nil
		execution.next++
		return nil
	}
	alternative := 0
	if succeeded {
		alternative = 1
	}
	if err := execution.cursor.SelectAlternative(pending.outcomeChoice, alternative); err != nil {
		return err
	}
	execution.pending = nil
	if succeeded {
		execution.next++
		return nil
	}
	if err := execution.cursor.Consume(
		pending.failureTerminal,
		operationplan.EffectStepTerminal,
	); err != nil {
		return err
	}
	execution.failed = true
	return nil
}

func (execution *RetirementExecution) pendingStep(
	retirementStep RetirementExecutionStep,
) (activeRetirementPendingStep, error) {
	if execution.next < len(activeRetirementPreControlSteps) {
		step := activeRetirementPreControlSteps[execution.next]
		return activeRetirementPendingStepFor(step, retirementStep)
	}
	if execution.next == len(activeRetirementPreControlSteps) {
		switch retirementStep {
		case RetirementStepControlPresent:
			if err := execution.cursor.SelectAlternative(activeRetirementControlModeChoice, 0); err != nil {
				return activeRetirementPendingStep{}, err
			}
			return activeRetirementPendingStep{
				step: activeRetirementStep{
					id:             activeRetirementControlPresentStep,
					kind:           operationplan.EffectStepNoOp,
					retirementStep: retirementStep,
				},
			}, nil
		case RetirementStepPublishControl:
			if err := execution.cursor.SelectAlternative(activeRetirementControlModeChoice, 1); err != nil {
				return activeRetirementPendingStep{}, err
			}
			return activeRetirementPendingStep{
				step: activeRetirementStep{
					id:             activeRetirementControlPublishStep,
					kind:           operationplan.EffectStepPersistence,
					retirementStep: retirementStep,
				},
				outcomeChoice:   activeRetirementControlOutcomeChoice,
				failureTerminal: activeRetirementControlFailureStep,
			}, nil
		default:
			return activeRetirementPendingStep{}, fmt.Errorf(
				"active journal retirement control step is %d, not %d or %d",
				retirementStep,
				RetirementStepControlPresent,
				RetirementStepPublishControl,
			)
		}
	}
	postIndex := execution.next - len(activeRetirementPreControlSteps) - 1
	if postIndex < 0 || postIndex >= len(activeRetirementPostControlSteps) {
		return activeRetirementPendingStep{}, fmt.Errorf(
			"active journal retirement has no remaining retirement step",
		)
	}
	return activeRetirementPendingStepFor(
		activeRetirementPostControlSteps[postIndex],
		retirementStep,
	)
}

func activeRetirementPendingStepFor(
	step activeRetirementStep,
	retirementStep RetirementExecutionStep,
) (activeRetirementPendingStep, error) {
	if step.retirementStep != retirementStep {
		return activeRetirementPendingStep{}, fmt.Errorf(
			"active journal retirement step is %d, not %d",
			step.retirementStep,
			retirementStep,
		)
	}
	return activeRetirementPendingStep{
		step:            step,
		outcomeChoice:   step.id + "/outcome",
		failureTerminal: step.id + "/failed",
	}, nil
}

func (execution *RetirementExecution) Finish(retirementErr error) error {
	if execution == nil || execution.cursor == nil {
		return errors.Join(
			retirementErr,
			fmt.Errorf("active journal retirement execution is unavailable"),
		)
	}
	if execution.closed {
		return errors.Join(
			retirementErr,
			fmt.Errorf("active journal retirement execution is already closed"),
		)
	}
	execution.closed = true
	var structuralErr error
	switch {
	case execution.pending != nil:
		structuralErr = execution.cursor.FinishSuccess()
	case execution.failed:
		if retirementErr == nil {
			structuralErr = fmt.Errorf(
				"active journal retirement selected a failure branch without an execution error",
			)
		}
		structuralErr = errors.Join(structuralErr, execution.cursor.FinishSuccess())
	case execution.next == activeRetirementStepCount() && retirementErr == nil:
		if err := execution.cursor.Consume(
			activeRetirementSuccessStep,
			operationplan.EffectStepTerminal,
		); err != nil {
			structuralErr = err
		} else {
			structuralErr = execution.cursor.FinishSuccess()
		}
	default:
		structuralErr = execution.cursor.FinishSuccess()
	}
	if retirementErr == nil {
		return structuralErr
	}
	if structuralErr == nil {
		return retirementErr
	}
	return errors.Join(retirementErr, structuralErr)
}

func activeRetirementStepCount() int {
	return len(activeRetirementPreControlSteps) + 1 + len(activeRetirementPostControlSteps)
}
