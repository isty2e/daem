package execute

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/operationplan"
)

type activeRecoveryCaller uint8

const (
	activeRecoveryCallerStandalone activeRecoveryCaller = iota + 1
	activeRecoveryCallerApplySettlement
)

const (
	activeRecoveryHostVisitChoice  = "active-recovery/rollback/host-action-visit"
	activeRecoveryHostVisitSkipped = "active-recovery/rollback/host-action-skipped"
	activeRecoveryHostVisitStep    = "active-recovery/rollback/host-action-visited"
	activeRecoveryHostOutcome      = "active-recovery/rollback/host-batch/outcome"
	activeRecoverySuccessPrefix    = "active-recovery/success"
)

type activeRecoveryScheduleInput struct {
	classification            recovery.Classification
	hostActionCount           int
	hasClaimTransitions       bool
	requiresOwnershipRegistry bool
	caller                    activeRecoveryCaller
	hasBeforeRetirement       bool
}

func (input activeRecoveryScheduleInput) ownsAuthority() bool {
	return input.caller == activeRecoveryCallerStandalone
}

type activeRecoveryStep struct {
	id   string
	kind operationplan.EffectStepKind
}

func activeRecoveryScheduleInputForPlan(
	plan recovery.Plan,
	caller activeRecoveryCaller,
	hasBeforeRetirement bool,
) activeRecoveryScheduleInput {
	hostActionCount := 0
	if plan.Classification() == recovery.ClassificationNeedsRollback {
		for _, action := range plan.Actions() {
			if action.Kind == recovery.ActionKindRestoreWrite ||
				action.Kind == recovery.ActionKindRestoreDelete {
				hostActionCount++
			}
		}
	}
	claimTransitions := plan.ClaimTransitions()
	return activeRecoveryScheduleInput{
		classification:            plan.Classification(),
		hostActionCount:           hostActionCount,
		hasClaimTransitions:       len(claimTransitions) != 0,
		requiresOwnershipRegistry: len(claimTransitions) != 0 || len(plan.ProvisionalAcquireIntents()) != 0,
		caller:                    caller,
		hasBeforeRetirement:       hasBeforeRetirement,
	}
}

func activeRecoveryStandalonePrefixSteps() []activeRecoveryStep {
	return []activeRecoveryStep{
		{id: "active-recovery/bind-journal-authority", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/bind-statefile-authority", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/establish-journal-basis", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/validate-journal-basis", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/bind-removal-authority", kind: operationplan.EffectStepObservation},
	}
}

func activeRecoveryCommonPreparationSteps() []activeRecoveryStep {
	return []activeRecoveryStep{
		{id: "active-recovery/prepare-mutation-authority", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/reload-before-effects", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/bind-reloaded-removal-authority", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/prepare-journal-retirement", kind: operationplan.EffectStepPersistence},
	}
}

func activeRecoveryCleanPreparationSteps() []activeRecoveryStep {
	return []activeRecoveryStep{
		{id: "active-recovery/clean/reserve-semantic-validations", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/clean/conclude-rollback-stage-absent", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/clean/prepare-removal-cleanup", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/clean/begin-general-execution", kind: operationplan.EffectStepObservation},
	}
}

func activeRecoveryFinalizePreparationSteps() []activeRecoveryStep {
	return []activeRecoveryStep{
		{id: "active-recovery/finalize/reserve-semantic-validations", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/finalize/conclude-rollback-stage-absent", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/finalize/prepare-removal-cleanup", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/finalize/begin-general-execution", kind: operationplan.EffectStepObservation},
	}
}

func activeRecoveryRollbackPreparationSteps() []activeRecoveryStep {
	return []activeRecoveryStep{
		{id: "active-recovery/rollback/prepare-host-actions", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/rollback/reserve-semantic-validations", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/rollback/match-manifest-root", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/rollback/prepare-backups", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/rollback/prepare-forward-removals", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/rollback/stage-rollback", kind: operationplan.EffectStepPersistence},
	}
}

func activeRecoveryOuterSettlementSteps(input activeRecoveryScheduleInput) []activeRecoveryStep {
	steps := []activeRecoveryStep{
		{id: "active-recovery/outer/validate-project-before-retirement", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/outer/validate-visibility-before-retirement", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/outer/reload-after-effects", kind: operationplan.EffectStepObservation},
	}
	if input.hasBeforeRetirement {
		steps = append(steps, activeRecoveryStep{
			id:   "active-recovery/outer/before-retirement",
			kind: operationplan.EffectStepObservation,
		})
	}
	steps = append(
		steps,
		activeRecoveryStep{id: "active-recovery/retirement/validate-before-cleanup", kind: operationplan.EffectStepObservation},
		activeRecoveryStep{id: "active-recovery/retirement/bind-removal-authority", kind: operationplan.EffectStepObservation},
		activeRecoveryStep{id: "active-recovery/retirement/validate-clean-plan", kind: operationplan.EffectStepObservation},
		activeRecoveryStep{id: "active-recovery/retirement/prepare-tail", kind: operationplan.EffectStepObservation},
		activeRecoveryStep{id: "active-recovery/retirement/advance-basis", kind: operationplan.EffectStepObservation},
		activeRecoveryStep{id: "active-recovery/retirement/removal-cleanup", kind: operationplan.EffectStepCleanup},
		activeRecoveryStep{id: "active-recovery/retirement/validate-journal", kind: operationplan.EffectStepObservation},
		activeRecoveryStep{id: "active-recovery/retirement/validate-semantics", kind: operationplan.EffectStepObservation},
		activeRecoveryStep{id: "active-recovery/retirement/execute-tail", kind: operationplan.EffectStepRetirement},
		activeRecoveryStep{id: "active-recovery/outer/accept-visibility", kind: operationplan.EffectStepObservation},
	)
	if input.caller == activeRecoveryCallerStandalone {
		steps = append(steps, activeRecoveryStep{
			id:   "active-recovery/outer/validate-project-after-retirement",
			kind: operationplan.EffectStepObservation,
		})
	}
	return steps
}

func compileActiveRecoveryStructure(
	input activeRecoveryScheduleInput,
) (operationplan.EffectStructure, error) {
	if input.hostActionCount < 0 {
		return operationplan.EffectStructure{}, fmt.Errorf(
			"active recovery host action count must not be negative",
		)
	}
	if input.caller != activeRecoveryCallerStandalone &&
		input.caller != activeRecoveryCallerApplySettlement {
		return operationplan.EffectStructure{}, fmt.Errorf(
			"active recovery caller mode %d is unsupported",
			input.caller,
		)
	}
	if input.caller == activeRecoveryCallerApplySettlement && input.hasBeforeRetirement {
		return operationplan.EffectStructure{}, fmt.Errorf(
			"Apply active recovery settlement cannot schedule a standalone retirement hook",
		)
	}
	if input.hasClaimTransitions && !input.requiresOwnershipRegistry {
		return operationplan.EffectStructure{}, fmt.Errorf(
			"active recovery claim transitions require ownership registry authority",
		)
	}
	if input.classification != recovery.ClassificationNeedsRollback && input.hostActionCount != 0 {
		return operationplan.EffectStructure{}, fmt.Errorf(
			"active recovery classification %q cannot schedule host rollback actions",
			input.classification,
		)
	}

	var builder operationplan.EffectStructureBuilder
	continuation := activeRecoveryTerminalSuffix(
		&builder,
		activeRecoverySuccessPrefix,
		input.ownsAuthority(),
	)
	continuation = activeRecoveryStepsWithFailure(
		&builder,
		activeRecoveryOuterSettlementSteps(input),
		continuation,
		input.ownsAuthority(),
	)

	var branch operationplan.EffectNode
	switch input.classification {
	case recovery.ClassificationCleanBefore, recovery.ClassificationCleanAfter:
		branch = activeRecoveryStepsWithFailure(
			&builder,
			activeRecoveryCleanPreparationSteps(),
			continuation,
			input.ownsAuthority(),
		)
	case recovery.ClassificationNeedsFinalize:
		claimKind := operationplan.EffectStepNoOp
		if input.hasClaimTransitions {
			claimKind = operationplan.EffectStepPersistence
		}
		branch = activeRecoveryStepWithFailure(
			&builder,
			activeRecoveryStep{
				id:   "active-recovery/finalize/claims",
				kind: claimKind,
			},
			continuation,
			input.ownsAuthority(),
		)
		branch = activeRecoveryStepsWithFailure(
			&builder,
			activeRecoveryFinalizePreparationSteps(),
			branch,
			input.ownsAuthority(),
		)
	case recovery.ClassificationNeedsRollback:
		branch = activeRecoveryRollbackBranch(&builder, input, continuation)
	default:
		return operationplan.EffectStructure{}, fmt.Errorf(
			"active recovery classification %q is unsupported",
			input.classification,
		)
	}

	branch = activeRecoveryStepsWithFailure(
		&builder,
		activeRecoveryCommonPreparationSteps(),
		branch,
		input.ownsAuthority(),
	)
	if input.caller == activeRecoveryCallerStandalone {
		branch = activeRecoveryStepsWithFailure(
			&builder,
			activeRecoveryStandalonePrefixSteps(),
			branch,
			input.ownsAuthority(),
		)
	}
	structure, err := builder.Compile(branch)
	if err != nil {
		return operationplan.EffectStructure{}, err
	}
	demand, err := structure.LegacyDemand()
	if err != nil {
		return operationplan.EffectStructure{}, err
	}
	if demand != (operationplan.Demand{}) {
		return operationplan.EffectStructure{}, fmt.Errorf(
			"active recovery outer schedule has State Barrier demand",
		)
	}
	return structure, nil
}

func activeRecoveryRollbackBranch(
	builder *operationplan.EffectStructureBuilder,
	input activeRecoveryScheduleInput,
	continuation operationplan.EffectNode,
) operationplan.EffectNode {
	successCleanup := activeRecoveryStepWithFailure(
		builder,
		activeRecoveryStep{
			id:   "active-recovery/rollback/cleanup-after-success",
			kind: operationplan.EffectStepCleanup,
		},
		continuation,
		input.ownsAuthority(),
	)
	postClaim := activeRecoveryBranchingStep(
		builder,
		activeRecoveryStep{
			id:   "active-recovery/rollback/validate-after-claims",
			kind: operationplan.EffectStepObservation,
		},
		activeRecoveryFailureCleanup(
			builder,
			"active-recovery/rollback/post-claim-cancellation",
			input.ownsAuthority(),
		),
		successCleanup,
	)
	claimKind := operationplan.EffectStepNoOp
	if input.hasClaimTransitions {
		claimKind = operationplan.EffectStepPersistence
	}
	claims := activeRecoveryBranchingStep(
		builder,
		activeRecoveryStep{
			id:   "active-recovery/rollback/claims",
			kind: claimKind,
		},
		activeRecoveryFailureCleanup(
			builder,
			"active-recovery/rollback/claim-failure",
			input.ownsAuthority(),
		),
		postClaim,
	)
	postHost := activeRecoveryBranchingStep(
		builder,
		activeRecoveryStep{
			id:   "active-recovery/rollback/validate-after-host-actions",
			kind: operationplan.EffectStepObservation,
		},
		activeRecoveryCompensation(
			builder,
			"active-recovery/rollback/post-host-cancellation",
			input.ownsAuthority(),
		),
		claims,
	)

	hostBatchKind := operationplan.EffectStepNoOp
	if input.hostActionCount != 0 {
		hostBatchKind = operationplan.EffectStepExternal
	}
	hostSequence := []operationplan.EffectNode{
		builder.Step("active-recovery/rollback/host-batch", hostBatchKind),
	}
	if input.hostActionCount != 0 {
		hostSequence = append(hostSequence, builder.Repeat(
			input.hostActionCount,
			builder.Choice(
				activeRecoveryHostVisitChoice,
				builder.Step(activeRecoveryHostVisitSkipped, operationplan.EffectStepNoOp),
				builder.Step(activeRecoveryHostVisitStep, operationplan.EffectStepObservation),
			),
		))
	}
	hostSequence = append(hostSequence, builder.Choice(
		activeRecoveryHostOutcome,
		activeRecoveryCompensation(
			builder,
			"active-recovery/rollback/host-failure",
			input.ownsAuthority(),
		),
		postHost,
	))
	branch := operationplan.EffectSequence(hostSequence...)
	branch = activeRecoveryBranchingStep(
		builder,
		activeRecoveryStep{
			id:   "active-recovery/rollback/prepare-host-execution",
			kind: operationplan.EffectStepObservation,
		},
		activeRecoveryCompensation(
			builder,
			"active-recovery/rollback/prepare-host-execution-failure",
			input.ownsAuthority(),
		),
		branch,
	)
	registryKind := operationplan.EffectStepNoOp
	if input.requiresOwnershipRegistry {
		registryKind = operationplan.EffectStepObservation
	}
	branch = activeRecoveryBranchingStep(
		builder,
		activeRecoveryStep{
			id:   "active-recovery/rollback/rebind-ownership-registry",
			kind: registryKind,
		},
		activeRecoveryFailureCleanup(
			builder,
			"active-recovery/rollback/rebind-ownership-failure",
			input.ownsAuthority(),
		),
		branch,
	)
	branch = activeRecoveryBranchingStep(
		builder,
		activeRecoveryStep{
			id:   "active-recovery/rollback/begin-general-execution",
			kind: operationplan.EffectStepObservation,
		},
		activeRecoveryFailureCleanup(
			builder,
			"active-recovery/rollback/begin-general-execution-failure",
			input.ownsAuthority(),
		),
		branch,
	)
	branch = activeRecoveryBranchingStep(
		builder,
		activeRecoveryStep{
			id:   "active-recovery/rollback/prepare-removal-cleanup",
			kind: operationplan.EffectStepObservation,
		},
		activeRecoveryFailureCleanup(
			builder,
			"active-recovery/rollback/prepare-removal-cleanup-failure",
			input.ownsAuthority(),
		),
		branch,
	)
	return activeRecoveryStepsWithFailure(
		builder,
		activeRecoveryRollbackPreparationSteps(),
		branch,
		input.ownsAuthority(),
	)
}

func activeRecoveryCompensation(
	builder *operationplan.EffectStructureBuilder,
	prefix string,
	ownsAuthority bool,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		builder.Step(prefix+"/restore", operationplan.EffectStepCompensation),
		builder.Choice(
			prefix+"/restore/outcome",
			builder.Step(prefix+"/restore/failed", operationplan.EffectStepNoOp),
			builder.Step(prefix+"/restore/succeeded", operationplan.EffectStepNoOp),
		),
		builder.Step(prefix+"/cleanup", operationplan.EffectStepCleanup),
		builder.Choice(
			prefix+"/cleanup-outcome",
			activeRecoveryTerminalSuffix(builder, prefix+"/cleanup-failure", ownsAuthority),
			activeRecoveryTerminalSuffix(builder, prefix+"/completed", ownsAuthority),
		),
	)
}

func activeRecoveryFailureCleanup(
	builder *operationplan.EffectStructureBuilder,
	prefix string,
	ownsAuthority bool,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		builder.Step(prefix+"/cleanup", operationplan.EffectStepCleanup),
		builder.Choice(
			prefix+"/cleanup-outcome",
			activeRecoveryTerminalSuffix(builder, prefix+"/cleanup-failure", ownsAuthority),
			activeRecoveryTerminalSuffix(builder, prefix+"/completed", ownsAuthority),
		),
	)
}

func activeRecoveryStepsWithFailure(
	builder *operationplan.EffectStructureBuilder,
	steps []activeRecoveryStep,
	continuation operationplan.EffectNode,
	ownsAuthority bool,
) operationplan.EffectNode {
	for index := len(steps) - 1; index >= 0; index-- {
		continuation = activeRecoveryStepWithFailure(
			builder,
			steps[index],
			continuation,
			ownsAuthority,
		)
	}
	return continuation
}

func activeRecoveryStepWithFailure(
	builder *operationplan.EffectStructureBuilder,
	step activeRecoveryStep,
	continuation operationplan.EffectNode,
	ownsAuthority bool,
) operationplan.EffectNode {
	return activeRecoveryBranchingStep(
		builder,
		step,
		activeRecoveryTerminalSuffix(builder, step.id+"/failure", ownsAuthority),
		continuation,
	)
}

func activeRecoveryBranchingStep(
	builder *operationplan.EffectStructureBuilder,
	step activeRecoveryStep,
	failure operationplan.EffectNode,
	success operationplan.EffectNode,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		builder.Step(step.id, step.kind),
		builder.Choice(step.id+"/outcome", failure, success),
	)
}

func activeRecoveryTerminalSuffix(
	builder *operationplan.EffectStructureBuilder,
	prefix string,
	ownsAuthority bool,
) operationplan.EffectNode {
	if !ownsAuthority {
		return builder.Step(prefix+"/terminal", operationplan.EffectStepTerminal)
	}
	return operationplan.EffectSequence(
		builder.Step(prefix+"/close-authority", operationplan.EffectStepCleanup),
		builder.Choice(
			prefix+"/close-outcome",
			builder.Step(prefix+"/close-failed", operationplan.EffectStepTerminal),
			builder.Step(prefix+"/terminal", operationplan.EffectStepTerminal),
		),
	)
}
