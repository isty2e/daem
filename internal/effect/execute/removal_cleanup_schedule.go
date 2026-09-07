package execute

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/operationplan"
)

const (
	removalCleanupObservationStep          = "active-recovery/removal-cleanup/observe"
	removalCleanupObservationOutcomeChoice = "active-recovery/removal-cleanup/observe-outcome"
	removalCleanupObservationFailureStep   = "active-recovery/removal-cleanup/observe-failed"
	removalCleanupActionChoice             = "active-recovery/removal-cleanup/action"
	removalCleanupCompletionStep           = "active-recovery/removal-cleanup/complete"
	removalCleanupCompletionOutcomeChoice  = "active-recovery/removal-cleanup/complete-outcome"
	removalCleanupCompletionFailureStep    = "active-recovery/removal-cleanup/complete-failed"
	removalCleanupSuccessStep              = "active-recovery/removal-cleanup/succeeded"
)

type removalCleanupActionStep struct {
	action recovery.RemovalCleanupActionKind
	id     string
	kind   operationplan.EffectStepKind
}

func removalCleanupSteps() [3]removalCleanupActionStep {
	return [...]removalCleanupActionStep{
		{
			action: recovery.RemovalCleanupActionConfirmAbsence,
			id:     "active-recovery/removal-cleanup/confirm-absence",
			kind:   operationplan.EffectStepObservation,
		},
		{
			action: recovery.RemovalCleanupActionPromoteResidue,
			id:     "active-recovery/removal-cleanup/promote-residue",
			kind:   operationplan.EffectStepRetirement,
		},
		{
			action: recovery.RemovalCleanupActionCleanupProgress,
			id:     "active-recovery/removal-cleanup/cleanup-progress",
			kind:   operationplan.EffectStepCleanup,
		},
	}
}

type removalCleanupExecutionStage uint8

const (
	removalCleanupReadyForObservation removalCleanupExecutionStage = iota + 1
	removalCleanupObservationPending
	removalCleanupReadyForAction
	removalCleanupActionPending
	removalCleanupReadyForCompletion
	removalCleanupCompletionPending
	removalCleanupTerminal
	removalCleanupClosed
)

type removalCleanupExecution struct {
	cursor        *operationplan.EffectCursor
	intentCount   int
	completed     int
	stage         removalCleanupExecutionStage
	pendingAction recovery.RemovalCleanupActionKind
	failed        bool
}

func compileRemovalCleanupStructure(intentCount int) (operationplan.EffectStructure, error) {
	if intentCount < 0 || intentCount > recovery.MaximumRemovalIntents {
		return operationplan.EffectStructure{}, fmt.Errorf(
			"active recovery removal cleanup count %d exceeds operation maximum %d",
			intentCount,
			recovery.MaximumRemovalIntents,
		)
	}

	var builder operationplan.EffectStructureBuilder
	steps := removalCleanupSteps()
	actionAlternatives := make([]operationplan.EffectNode, 0, len(steps))
	for _, step := range steps {
		actionAlternatives = append(
			actionAlternatives,
			operationplan.EffectSequence(
				builder.Step(step.id, step.kind),
				builder.Choice(
					step.id+"/outcome",
					builder.Step(step.id+"/failed", operationplan.EffectStepTerminal),
					operationplan.EffectSequence(),
				),
			),
		)
	}
	intent := operationplan.EffectSequence(
		builder.Step(removalCleanupObservationStep, operationplan.EffectStepObservation),
		builder.Choice(
			removalCleanupObservationOutcomeChoice,
			builder.Step(removalCleanupObservationFailureStep, operationplan.EffectStepTerminal),
			builder.Choice(removalCleanupActionChoice, actionAlternatives...),
		),
	)

	sequence := make([]operationplan.EffectNode, 0, 2)
	if intentCount != 0 {
		sequence = append(sequence, builder.Repeat(intentCount, intent))
	}
	sequence = append(
		sequence,
		operationplan.EffectSequence(
			builder.Step(removalCleanupCompletionStep, operationplan.EffectStepObservation),
			builder.Choice(
				removalCleanupCompletionOutcomeChoice,
				builder.Step(removalCleanupCompletionFailureStep, operationplan.EffectStepTerminal),
				builder.Step(removalCleanupSuccessStep, operationplan.EffectStepTerminal),
			),
		),
	)
	structure, err := builder.Compile(operationplan.EffectSequence(sequence...))
	if err != nil {
		return operationplan.EffectStructure{}, err
	}
	demand, err := structure.LegacyDemand()
	if err != nil {
		return operationplan.EffectStructure{}, err
	}
	if demand != (operationplan.Demand{}) {
		return operationplan.EffectStructure{}, fmt.Errorf(
			"active recovery removal cleanup schedule has State Barrier demand",
		)
	}
	return structure, nil
}

func newRemovalCleanupExecution(
	structure operationplan.EffectStructure,
	intentCount int,
) *removalCleanupExecution {
	stage := removalCleanupReadyForObservation
	if intentCount == 0 {
		stage = removalCleanupReadyForCompletion
	}
	return &removalCleanupExecution{
		cursor:      structure.Begin(),
		intentCount: intentCount,
		stage:       stage,
	}
}

func (execution *removalCleanupExecution) admitObservation() error {
	if err := execution.requireStage(removalCleanupReadyForObservation); err != nil {
		return err
	}
	if execution.completed >= execution.intentCount {
		return fmt.Errorf("active recovery removal cleanup has no remaining intent")
	}
	if err := execution.cursor.Consume(
		removalCleanupObservationStep,
		operationplan.EffectStepObservation,
	); err != nil {
		return err
	}
	execution.stage = removalCleanupObservationPending
	return nil
}

func (execution *removalCleanupExecution) settleObservation(succeeded bool) error {
	if err := execution.requireStage(removalCleanupObservationPending); err != nil {
		return err
	}
	alternative := 0
	if succeeded {
		alternative = 1
	}
	if err := execution.cursor.SelectAlternative(
		removalCleanupObservationOutcomeChoice,
		alternative,
	); err != nil {
		return err
	}
	if succeeded {
		execution.stage = removalCleanupReadyForAction
		return nil
	}
	if err := execution.cursor.Consume(
		removalCleanupObservationFailureStep,
		operationplan.EffectStepTerminal,
	); err != nil {
		return err
	}
	execution.failed = true
	execution.stage = removalCleanupTerminal
	return nil
}

func (execution *removalCleanupExecution) admitAction(
	action recovery.RemovalCleanupActionKind,
) error {
	if err := execution.requireStage(removalCleanupReadyForAction); err != nil {
		return err
	}
	step, alternative, err := removalCleanupStepForAction(action)
	if err != nil {
		return err
	}
	if err := execution.cursor.SelectAlternative(
		removalCleanupActionChoice,
		alternative,
	); err != nil {
		return err
	}
	if err := execution.cursor.Consume(step.id, step.kind); err != nil {
		return err
	}
	execution.pendingAction = action
	execution.stage = removalCleanupActionPending
	return nil
}

func (execution *removalCleanupExecution) settleAction(
	action recovery.RemovalCleanupActionKind,
	succeeded bool,
) error {
	if err := execution.requireStage(removalCleanupActionPending); err != nil {
		return err
	}
	if action != execution.pendingAction {
		return fmt.Errorf(
			"active recovery removal cleanup action %q is pending, not %q",
			execution.pendingAction,
			action,
		)
	}
	step, _, err := removalCleanupStepForAction(action)
	if err != nil {
		return err
	}
	alternative := 0
	if succeeded {
		alternative = 1
	}
	if err := execution.cursor.SelectAlternative(step.id+"/outcome", alternative); err != nil {
		return err
	}
	execution.pendingAction = ""
	if !succeeded {
		if err := execution.cursor.Consume(
			step.id+"/failed",
			operationplan.EffectStepTerminal,
		); err != nil {
			return err
		}
		execution.failed = true
		execution.stage = removalCleanupTerminal
		return nil
	}

	execution.completed++
	if execution.completed == execution.intentCount {
		execution.stage = removalCleanupReadyForCompletion
	} else {
		execution.stage = removalCleanupReadyForObservation
	}
	return nil
}

func (execution *removalCleanupExecution) admitCompletion() error {
	if err := execution.requireStage(removalCleanupReadyForCompletion); err != nil {
		return err
	}
	if execution.completed != execution.intentCount {
		return fmt.Errorf(
			"active recovery removal cleanup completed %d of %d intents",
			execution.completed,
			execution.intentCount,
		)
	}
	if err := execution.cursor.Consume(
		removalCleanupCompletionStep,
		operationplan.EffectStepObservation,
	); err != nil {
		return err
	}
	execution.stage = removalCleanupCompletionPending
	return nil
}

func (execution *removalCleanupExecution) settleCompletion(succeeded bool) error {
	if err := execution.requireStage(removalCleanupCompletionPending); err != nil {
		return err
	}
	alternative := 0
	stepID := removalCleanupCompletionFailureStep
	if succeeded {
		alternative = 1
		stepID = removalCleanupSuccessStep
	}
	if err := execution.cursor.SelectAlternative(
		removalCleanupCompletionOutcomeChoice,
		alternative,
	); err != nil {
		return err
	}
	if err := execution.cursor.Consume(stepID, operationplan.EffectStepTerminal); err != nil {
		return err
	}
	execution.failed = !succeeded
	execution.stage = removalCleanupTerminal
	return nil
}

func (execution *removalCleanupExecution) finish(operationErr error) error {
	if execution == nil || execution.cursor == nil {
		return joinRemovalCleanupExecutionError(
			operationErr,
			fmt.Errorf("active recovery removal cleanup execution is unavailable"),
		)
	}
	if execution.stage == removalCleanupClosed {
		return joinRemovalCleanupExecutionError(
			operationErr,
			fmt.Errorf("active recovery removal cleanup execution is already closed"),
		)
	}

	var structuralErr error
	switch {
	case execution.stage != removalCleanupTerminal:
		structuralErr = execution.cursor.FinishSuccess()
	case execution.failed && operationErr == nil:
		structuralErr = fmt.Errorf(
			"active recovery removal cleanup selected a failure terminal without an execution error",
		)
	case !execution.failed && operationErr != nil:
		structuralErr = fmt.Errorf(
			"active recovery removal cleanup selected success with an execution error",
		)
	default:
		structuralErr = execution.cursor.FinishSuccess()
	}
	execution.stage = removalCleanupClosed
	return joinRemovalCleanupExecutionError(operationErr, structuralErr)
}

func (execution *removalCleanupExecution) requireStage(
	stage removalCleanupExecutionStage,
) error {
	if execution == nil || execution.cursor == nil {
		return fmt.Errorf("active recovery removal cleanup execution is unavailable")
	}
	if execution.stage != stage {
		return fmt.Errorf(
			"active recovery removal cleanup stage is %d, not %d",
			execution.stage,
			stage,
		)
	}
	return nil
}

func removalCleanupStepForAction(
	action recovery.RemovalCleanupActionKind,
) (removalCleanupActionStep, int, error) {
	for index, step := range removalCleanupSteps() {
		if step.action == action {
			return step, index, nil
		}
	}
	return removalCleanupActionStep{}, 0, fmt.Errorf(
		"active recovery removal cleanup action %q is unavailable",
		action,
	)
}

func joinRemovalCleanupExecutionError(primary error, structural error) error {
	switch {
	case primary == nil:
		return structural
	case structural == nil:
		return primary
	default:
		return errors.Join(primary, structural)
	}
}
