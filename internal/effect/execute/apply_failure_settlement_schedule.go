package execute

import (
	"fmt"

	"github.com/isty2e/daem/internal/operationplan"
)

type applyFailureSettlementKind uint8

const (
	applyFailureSettlementClaimPreparation applyFailureSettlementKind = iota
	applyFailureSettlementPreparedEffects
	applyFailureSettlementGuardedRecovery
)

const applyFailureSettlementChoice = "apply/failure-settlement"

func compileApplyFailureSettlementStructure(
	hasOwnershipTransitions bool,
) (operationplan.EffectStructure, error) {
	var builder operationplan.EffectStructureBuilder
	structure, err := builder.Compile(builder.Choice(
		applyFailureSettlementChoice,
		applyPreHostFailureSettlementBranch(
			&builder,
			applyFailureSettlementClaimPreparation,
			hasOwnershipTransitions,
		),
		applyPreHostFailureSettlementBranch(
			&builder,
			applyFailureSettlementPreparedEffects,
			hasOwnershipTransitions,
		),
		applyGuardedRecoverySettlementBranch(&builder),
	))
	if err != nil {
		return operationplan.EffectStructure{}, err
	}
	demand, err := structure.LegacyDemand()
	if err != nil {
		return operationplan.EffectStructure{}, err
	}
	if !demand.Empty() {
		return operationplan.EffectStructure{}, fmt.Errorf(
			"compile apply failure settlement: expected zero State Barrier demand",
		)
	}
	return structure, nil
}

func applyPreHostFailureSettlementBranch(
	builder *operationplan.EffectStructureBuilder,
	kind applyFailureSettlementKind,
	hasOwnershipTransitions bool,
) operationplan.EffectNode {
	prefix := kind.reference()
	nodes := make([]operationplan.EffectNode, 0, 9)
	if hasOwnershipTransitions {
		nodes = append(
			nodes,
			applyCheckedStep(
				builder,
				prefix+"/ownership-rollback/transition-plan",
				operationplan.EffectStepObservation,
			),
			applyVisibilityObligation(
				builder,
				prefix+"/ownership-rollback",
				operationplan.EffectStepObservation,
				operationplan.EffectStepCompensation,
			),
		)
	}
	nodes = append(
		nodes,
		applyCheckedStep(
			builder,
			prefix+"/journal-retirement/project-validation",
			operationplan.EffectStepObservation,
		),
		applyCheckedStep(
			builder,
			prefix+"/journal-retirement/forward",
			operationplan.EffectStepObservation,
		),
		applyCheckedStep(
			builder,
			prefix+"/journal-retirement/reload",
			operationplan.EffectStepObservation,
		),
		applyCheckedStep(
			builder,
			prefix+"/journal-retirement/settlement",
			operationplan.EffectStepRetirement,
		),
		applyCheckedStep(
			builder,
			prefix+"/journal-retirement/acceptance",
			operationplan.EffectStepObservation,
		),
		builder.Step(prefix+"/complete", operationplan.EffectStepTerminal),
	)
	return operationplan.EffectSequence(nodes...)
}

func applyGuardedRecoverySettlementBranch(
	builder *operationplan.EffectStructureBuilder,
) operationplan.EffectNode {
	prefix := applyFailureSettlementGuardedRecovery.reference()
	return operationplan.EffectSequence(
		applyCheckedStep(
			builder,
			prefix+"/progress-classification",
			operationplan.EffectStepObservation,
		),
		applyCheckedStep(
			builder,
			prefix+"/rollback-selection",
			operationplan.EffectStepObservation,
		),
		applyCheckedStep(
			builder,
			prefix+"/journal-load",
			operationplan.EffectStepObservation,
		),
		applyCheckedStep(
			builder,
			prefix+"/journal-validation",
			operationplan.EffectStepObservation,
		),
		applyCheckedStep(
			builder,
			prefix+"/execution-compile",
			operationplan.EffectStepObservation,
		),
		builder.Step(prefix+"/active-recovery-handoff", operationplan.EffectStepTerminal),
	)
}

func applyVisibilityObligation(
	builder *operationplan.EffectStructureBuilder,
	id string,
	validation operationplan.EffectStepKind,
	settlement operationplan.EffectStepKind,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		applyCheckedStep(builder, id+"/forward", validation),
		applyCheckedStep(builder, id+"/settlement", settlement),
		applyCheckedStep(builder, id+"/acceptance", operationplan.EffectStepObservation),
	)
}

func (kind applyFailureSettlementKind) reference() string {
	switch kind {
	case applyFailureSettlementClaimPreparation:
		return "apply/failure-settlement/claim-preparation"
	case applyFailureSettlementPreparedEffects:
		return "apply/failure-settlement/prepared-effects"
	case applyFailureSettlementGuardedRecovery:
		return "apply/failure-settlement/guarded-recovery"
	default:
		return "apply/failure-settlement/invalid"
	}
}

func (kind applyFailureSettlementKind) alternative() (int, error) {
	switch kind {
	case applyFailureSettlementClaimPreparation:
		return 0, nil
	case applyFailureSettlementPreparedEffects:
		return 1, nil
	case applyFailureSettlementGuardedRecovery:
		return 2, nil
	default:
		return 0, fmt.Errorf("invalid apply failure settlement kind %d", kind)
	}
}

func (disposition applyForwardFailureDisposition) allows(
	kind applyFailureSettlementKind,
) bool {
	switch disposition {
	case applyForwardFailureClaimPreparation:
		return kind == applyFailureSettlementClaimPreparation
	case applyForwardFailurePreparedEffects:
		return kind == applyFailureSettlementPreparedEffects
	case applyForwardFailureGuardedRecovery,
		applyForwardFailureStatefilePublication:
		return kind == applyFailureSettlementGuardedRecovery
	default:
		return false
	}
}

func (disposition applyForwardFailureDisposition) allowsNoCompensation() bool {
	return disposition == applyForwardFailureNoCompensation ||
		disposition == applyForwardFailureStatefilePublication
}
