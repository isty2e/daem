package execute

import (
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
	id   string
	kind operationplan.EffectStepKind
}

var activeRetirementPreControlSteps = [...]activeRetirementStep{
	{id: "active-retirement/validate-plan", kind: operationplan.EffectStepObservation},
	{id: "active-retirement/validate-prepared-layout", kind: operationplan.EffectStepObservation},
	{id: "active-retirement/validate-active-identity", kind: operationplan.EffectStepObservation},
}

var activeRetirementPostControlSteps = [...]activeRetirementStep{
	{id: "active-retirement/validate-active-record", kind: operationplan.EffectStepObservation},
	{id: "active-retirement/move-active-to-residue", kind: operationplan.EffectStepRetirement},
	{id: "active-retirement/validate-phase-advance-layout", kind: operationplan.EffectStepObservation},
	{id: "active-retirement/advance-record", kind: operationplan.EffectStepPersistence},
	{id: "active-retirement/validate-finalizing-layout", kind: operationplan.EffectStepObservation},
	{id: "active-retirement/cleanup-residue", kind: operationplan.EffectStepCleanup},
	{id: "active-retirement/retire-control", kind: operationplan.EffectStepRetirement},
	{id: "active-retirement/cleanup-garbage", kind: operationplan.EffectStepCleanup},
}

// compileActiveJournalRetirementStructure describes the fixed post-cleanup
// ExecuteActive tail. It grants no authority and is not runtime admission.
func compileActiveJournalRetirementStructure() (operationplan.EffectStructure, error) {
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
