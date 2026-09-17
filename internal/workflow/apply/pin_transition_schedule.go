package apply

import "github.com/isty2e/daem/internal/operationplan"

func compileApplyPinRouteSchedule(builder *operationplan.EffectStructureBuilder, statefile *applyStatefileSchedule, route applyRouteScheduleFact) operationplan.EffectNode {
	var reservation, completion operationplan.EffectNode
	if route.work.Global {
		reservation = compileApplyPinRegistrySchedule(builder, statefile, route.ref+"/pin/reserve")
		completion = compileApplyPinRegistrySchedule(builder, statefile, route.ref+"/pin/complete")
	} else {
		reservation = statefile.checkedPublications(route.ref+"/pin/reserve", 1)
		completion = operationplan.EffectSequence(
			statefile.checkedValidations(route.ref+"/pin/pre-completion", 1),
			statefile.checkedPublications(route.ref+"/pin/complete", 1),
		)
	}
	return operationplan.EffectSequence(
		compileApplyCheckedStep(builder, route.ref+"/forward", operationplan.EffectStepForwardEffect),
		statefile.checkedEnsure(route.ref+"/statefile"),
		compileApplyCheckedStep(builder, route.ref+"/pin/binding", operationplan.EffectStepObservation),
		reservation,
		statefile.checkedValidations(route.ref+"/pin/pre-host", 1),
		builder.Step(route.ref+"/pin/host", operationplan.EffectStepExternal),
		statefile.checkedValidations(route.ref+"/pin/post-host", 1),
		compileApplyCheckedStep(builder, route.ref+"/pin/observe", operationplan.EffectStepObservation),
		compileApplyCheckedStep(builder, route.ref+"/pin/classify", operationplan.EffectStepObservation),
		compileApplyCheckedStep(builder, route.ref+"/pin/record", operationplan.EffectStepObservation),
		compileApplyCheckedStep(builder, route.ref+"/pin/project-root", operationplan.EffectStepObservation),
		builder.Choice(route.ref+"/pin/settlement", completion, builder.Step(route.ref+"/pin/retain", operationplan.EffectStepNoOp)),
		statefile.checkedValidations(route.ref+"/pin/pre-attempt", 1),
		statefile.checkedPublications(route.ref+"/pin/attempt", 1),
		statefile.checkedValidations(route.ref+"/pin/post-attempt", 1),
		compileApplyCheckedStep(builder, route.ref+"/pin/final-project-root", operationplan.EffectStepObservation),
		compileApplyCheckedStep(builder, route.ref+"/pin/final-declarations", operationplan.EffectStepObservation),
		compileApplyFailFastChoice(builder, route.ref+"/pin/outcome"),
	)
}

func compileApplyPinRegistrySchedule(builder *operationplan.EffectStructureBuilder, statefile *applyStatefileSchedule, ref string) operationplan.EffectNode {
	return operationplan.EffectSequence(
		compileApplyCheckedStep(builder, ref+"/declarations-before", operationplan.EffectStepObservation),
		compileApplyCheckedStep(builder, ref+"/root-before", operationplan.EffectStepObservation),
		statefile.checkedValidations(ref+"/statefile-before", 1),
		compileApplyCheckedStep(builder, ref+"/registry", operationplan.EffectStepPersistence),
		statefile.checkedValidations(ref+"/statefile-after", 1),
		compileApplyCheckedStep(builder, ref+"/visibility", operationplan.EffectStepObservation),
		compileApplyCheckedStep(builder, ref+"/root-after", operationplan.EffectStepObservation),
		compileApplyCheckedStep(builder, ref+"/declarations-after", operationplan.EffectStepObservation),
	)
}
