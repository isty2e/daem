package applyprogram

import (
	"fmt"

	"github.com/isty2e/daem/internal/operationplan"
)

type FailureSettlement uint8

const (
	SettlementClaimPreparation FailureSettlement = iota
	SettlementPreparedEffects
	SettlementGuardedRecovery
)

const applyFailureSettlementChoice = "apply/failure-settlement"

func CompileFailureSettlement(
	hasOwnershipTransitions bool,
) (operationplan.EffectStructure, error) {
	var builder operationplan.EffectStructureBuilder
	structure, err := builder.Compile(builder.Choice(
		applyFailureSettlementChoice,
		applyPreHostFailureSettlementBranch(
			&builder,
			SettlementClaimPreparation,
			hasOwnershipTransitions,
		),
		applyPreHostFailureSettlementBranch(
			&builder,
			SettlementPreparedEffects,
			hasOwnershipTransitions,
		),
		applyGuardedRecoverySettlementBranch(&builder),
	))
	if err != nil {
		return operationplan.EffectStructure{}, err
	}
	if err := validateFailurePolicy(structure); err != nil {
		return operationplan.EffectStructure{}, err
	}
	return structure, nil
}

func validateFailurePolicy(structure operationplan.EffectStructure) error {
	demand, err := structure.LegacyDemand()
	if err != nil {
		return err
	}
	if !demand.Empty() {
		return fmt.Errorf("compile apply failure settlement: expected zero State Barrier demand")
	}
	return nil
}

func applyPreHostFailureSettlementBranch(
	builder *operationplan.EffectStructureBuilder,
	kind FailureSettlement,
	hasOwnershipTransitions bool,
) operationplan.EffectNode {
	prefix := kind.Reference()
	nodes := make([]operationplan.EffectNode, 0, 9)
	if hasOwnershipTransitions {
		nodes = append(
			nodes,
			CheckedStep(
				builder,
				prefix+"/ownership-rollback/transition-plan",
				operationplan.EffectStepObservation,
			),
			VisibilityObligation(
				builder,
				prefix+"/ownership-rollback",
				operationplan.EffectStepObservation,
				operationplan.EffectStepCompensation,
			),
		)
	}
	nodes = append(
		nodes,
		CheckedStep(
			builder,
			prefix+"/journal-retirement/project-validation",
			operationplan.EffectStepObservation,
		),
		CheckedStep(
			builder,
			prefix+"/journal-retirement/forward",
			operationplan.EffectStepObservation,
		),
		CheckedStep(
			builder,
			prefix+"/journal-retirement/reload",
			operationplan.EffectStepObservation,
		),
		CheckedStep(
			builder,
			prefix+"/journal-retirement/settlement",
			operationplan.EffectStepRetirement,
		),
		CheckedStep(
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
	prefix := SettlementGuardedRecovery.Reference()
	return operationplan.EffectSequence(
		CheckedStep(
			builder,
			prefix+"/progress-classification",
			operationplan.EffectStepObservation,
		),
		CheckedStep(
			builder,
			prefix+"/rollback-selection",
			operationplan.EffectStepObservation,
		),
		CheckedStep(
			builder,
			prefix+"/journal-load",
			operationplan.EffectStepObservation,
		),
		CheckedStep(
			builder,
			prefix+"/journal-validation",
			operationplan.EffectStepObservation,
		),
		CheckedStep(
			builder,
			prefix+"/execution-compile",
			operationplan.EffectStepObservation,
		),
		builder.Step(prefix+"/active-recovery-handoff", operationplan.EffectStepTerminal),
	)
}

func VisibilityObligation(
	builder *operationplan.EffectStructureBuilder,
	id string,
	validation operationplan.EffectStepKind,
	settlement operationplan.EffectStepKind,
) operationplan.EffectNode {
	return operationplan.EffectSequence(
		CheckedStep(builder, id+"/forward", validation),
		CheckedStep(builder, id+"/settlement", settlement),
		CheckedStep(builder, id+"/acceptance", operationplan.EffectStepObservation),
	)
}

func (kind FailureSettlement) Reference() string {
	switch kind {
	case SettlementClaimPreparation:
		return "apply/failure-settlement/claim-preparation"
	case SettlementPreparedEffects:
		return "apply/failure-settlement/prepared-effects"
	case SettlementGuardedRecovery:
		return "apply/failure-settlement/guarded-recovery"
	default:
		return "apply/failure-settlement/invalid"
	}
}

func (kind FailureSettlement) alternative() (int, error) {
	switch kind {
	case SettlementClaimPreparation:
		return 0, nil
	case SettlementPreparedEffects:
		return 1, nil
	case SettlementGuardedRecovery:
		return 2, nil
	default:
		return 0, fmt.Errorf("invalid apply failure settlement kind %d", kind)
	}
}

func (disposition ForwardFailure) allows(
	kind FailureSettlement,
) bool {
	switch disposition {
	case FailureClaimPreparation:
		return kind == SettlementClaimPreparation
	case FailurePreparedEffects:
		return kind == SettlementPreparedEffects
	case FailureGuardedRecovery,
		FailureStatefilePublication:
		return kind == SettlementGuardedRecovery
	default:
		return false
	}
}

func (disposition ForwardFailure) allowsNoCompensation() bool {
	return disposition == FailureNoCompensation ||
		disposition == FailureStatefilePublication
}
