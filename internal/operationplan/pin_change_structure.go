package operationplan

// PinChangeEffectNode describes pin reservation, invocation, completion or
// retention, and attempt persistence. Binding availability is a compilation
// fact, not physical authority.
func PinChangeEffectNode(
	builder *EffectStructureBuilder,
	ref string,
	global bool,
	bindingDescribed bool,
) EffectNode {
	var reservation, completion EffectNode
	if global {
		reservation = pinRegistryStructure(builder, ref+"/pin/reserve")
		completion = pinRegistryStructure(builder, ref+"/pin/complete")
	} else {
		reservation = pinPublicationStructure(builder, ref+"/pin/reserve")
		completion = EffectSequence(
			pinValidationStructure(builder, ref+"/pin/pre-completion"),
			pinPublicationStructure(builder, ref+"/pin/complete"),
		)
	}

	return EffectSequence(
		pinCheckedStep(builder, ref+"/forward", EffectStepForwardEffect),
		EffectSequence(
			builder.DescendantEnsure(ref+"/statefile", bindingDescribed),
			pinOutcomeChoice(builder, ref+"/statefile/ensure-outcome"),
		),
		pinCheckedStep(builder, ref+"/pin/binding", EffectStepObservation),
		reservation,
		pinValidationStructure(builder, ref+"/pin/pre-host"),
		builder.Step(ref+"/pin/host", EffectStepExternal),
		pinValidationStructure(builder, ref+"/pin/post-host"),
		pinCheckedStep(builder, ref+"/pin/observe", EffectStepObservation),
		pinCheckedStep(builder, ref+"/pin/classify", EffectStepObservation),
		pinCheckedStep(builder, ref+"/pin/record", EffectStepObservation),
		pinCheckedStep(builder, ref+"/pin/project-root", EffectStepObservation),
		builder.Choice(ref+"/pin/settlement", completion, builder.Step(ref+"/pin/retain", EffectStepNoOp)),
		pinValidationStructure(builder, ref+"/pin/pre-attempt"),
		pinPublicationStructure(builder, ref+"/pin/attempt"),
		pinValidationStructure(builder, ref+"/pin/post-attempt"),
		pinCheckedStep(builder, ref+"/pin/final-project-root", EffectStepObservation),
		pinCheckedStep(builder, ref+"/pin/final-declarations", EffectStepObservation),
		pinOutcomeChoice(builder, ref+"/pin/outcome"),
	)
}

func pinRegistryStructure(builder *EffectStructureBuilder, ref string) EffectNode {
	return EffectSequence(
		pinCheckedStep(builder, ref+"/declarations-before", EffectStepObservation),
		pinCheckedStep(builder, ref+"/root-before", EffectStepObservation),
		pinValidationStructure(builder, ref+"/statefile-before"),
		pinCheckedStep(builder, ref+"/registry", EffectStepPersistence),
		pinValidationStructure(builder, ref+"/statefile-after"),
		pinCheckedStep(builder, ref+"/visibility", EffectStepObservation),
		pinCheckedStep(builder, ref+"/root-after", EffectStepObservation),
		pinCheckedStep(builder, ref+"/declarations-after", EffectStepObservation),
	)
}

func pinValidationStructure(builder *EffectStructureBuilder, ref string) EffectNode {
	return builder.Repeat(1, pinCheckedStep(builder, ref+"/validate", EffectStepValidateDescendant))
}

func pinPublicationStructure(builder *EffectStructureBuilder, ref string) EffectNode {
	return builder.Repeat(1, pinCheckedStep(builder, ref+"/publish", EffectStepPublishDescendant))
}

func pinCheckedStep(builder *EffectStructureBuilder, ref string, kind EffectStepKind) EffectNode {
	return EffectSequence(builder.Step(ref, kind), pinOutcomeChoice(builder, ref+"/outcome"))
}

func pinOutcomeChoice(builder *EffectStructureBuilder, ref string) EffectNode {
	return builder.Choice(
		ref,
		builder.Step(ref+"/success", EffectStepNoOp),
		builder.Step(ref+"/failure", EffectStepTerminal),
	)
}

func pinChangeStatefileDemand(global bool) (statefileDemand, error) {
	// Binding demand belongs to the enclosing plan. A retained-binding validation
	// also covers the initial bind-or-validate choice's validation maximum.
	var builder EffectStructureBuilder
	structure, err := builder.Compile(builder.ForwardPhase(
		"pin-phase",
		PinChangeEffectNode(&builder, "pin", global, true),
	))
	if err != nil {
		return statefileDemand{}, err
	}

	bound, err := structure.legacyUpperBound()
	if err != nil {
		return statefileDemand{}, err
	}

	return statefileDemand{
		validations: bound.descendantValidations,
		commits:     bound.descendantFileCommits,
	}, nil
}
