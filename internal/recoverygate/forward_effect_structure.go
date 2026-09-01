package recoverygate

import (
	"fmt"

	"github.com/isty2e/daem/internal/operationplan"
)

type structuralForwardWork struct {
	state      stateDirPhysicalWork
	total      stateDirPhysicalWork
	descendant int
}

type loweredForwardPlan struct {
	planned plannedStateDirOperation
	work    structuralForwardWork
}

// ReserveForwardEffectStructure validates branch-aware physical lowering of
// one owner schedule before retaining the legacy scalar runtime authority.
func (authority EffectAuthority) ReserveForwardEffectStructure(
	structure operationplan.EffectStructure,
	legacyDemand operationplan.Demand,
) (*ForwardEffectAuthority, error) {
	return authority.reserveForwardEffectStructure(structure, legacyDemand)
}

func (authority EffectAuthority) reserveForwardEffectStructure(
	structure operationplan.EffectStructure,
	legacyDemand operationplan.Demand,
) (*ForwardEffectAuthority, error) {
	legacyPlan, planned, err := authority.prepareForwardEffectStructure(
		structure,
		legacyDemand,
	)
	if err != nil {
		return nil, err
	}
	return authority.reserveLoweredPlan(legacyPlan, planned)
}

func (authority EffectAuthority) prepareForwardEffectStructure(
	structure operationplan.EffectStructure,
	legacyDemand operationplan.Demand,
) (forwardEffectPlan, plannedStateDirOperation, error) {
	legacyPlan := planFromDemand(legacyDemand)
	structuralPlan, structuralWork, err := authority.inspectForwardEffectStructure(
		structure,
		legacyPlan.DescendantPath,
	)
	if err != nil {
		return forwardEffectPlan{}, plannedStateDirOperation{}, err
	}
	if !sameForwardPlanCounts(structuralPlan, legacyPlan) {
		return forwardEffectPlan{}, plannedStateDirOperation{}, fmt.Errorf(
			"forward effect structure demand differs from legacy demand: "+
				"structural ensure=%d barrier=%d StateDir=%d descendant-validations=%d descendant-commits=%d "+
				"legacy ensure=%d barrier=%d StateDir=%d descendant-validations=%d descendant-commits=%d",
			structuralPlan.EnsureCalls,
			structuralPlan.BarrierValidationCalls,
			structuralPlan.StateDirValidationCalls,
			structuralPlan.DescendantValidations,
			structuralPlan.DescendantFileCommits,
			legacyPlan.EnsureCalls,
			legacyPlan.BarrierValidationCalls,
			legacyPlan.StateDirValidationCalls,
			legacyPlan.DescendantValidations,
			legacyPlan.DescendantFileCommits,
		)
	}
	legacyLowered, err := authority.lowerForwardPlan(legacyPlan)
	if err != nil {
		return forwardEffectPlan{}, plannedStateDirOperation{}, err
	}
	if err := requireStructuralWorkDominance(
		"legacy forward reservation",
		"legacy",
		legacyLowered.work,
		structuralWork,
	); err != nil {
		return forwardEffectPlan{}, plannedStateDirOperation{}, err
	}
	return legacyPlan, legacyLowered.planned, nil
}

func (authority EffectAuthority) prepareForwardEffectExecution(
	structure operationplan.EffectStructure,
	descendantPath string,
) (plannedStateDirOperation, error) {
	structuralPlan, structuralWork, err := authority.inspectForwardEffectStructure(
		structure,
		descendantPath,
	)
	if err != nil {
		return plannedStateDirOperation{}, err
	}
	lowered, err := authority.lowerForwardPlan(structuralPlan)
	if err != nil {
		return plannedStateDirOperation{}, err
	}
	if err := requireStructuralWorkDominance(
		"structural forward reservation",
		"structural upper bound",
		lowered.work,
		structuralWork,
	); err != nil {
		return plannedStateDirOperation{}, err
	}
	return lowered.planned, nil
}

func (authority EffectAuthority) inspectForwardEffectStructure(
	structure operationplan.EffectStructure,
	descendantPath string,
) (forwardEffectPlan, structuralForwardWork, error) {
	if err := authority.requireInitialized(); err != nil {
		return forwardEffectPlan{}, structuralForwardWork{}, err
	}
	alternatives, err := structure.DemandAlternatives()
	if err != nil {
		return forwardEffectPlan{}, structuralForwardWork{}, fmt.Errorf(
			"lower forward effect structure: %w",
			err,
		)
	}
	structuralPlan, err := maximumStructuralForwardPlan(alternatives, descendantPath)
	if err != nil {
		return forwardEffectPlan{}, structuralForwardWork{}, err
	}
	structuralWork, err := authority.lowerStructuralForwardWork(
		alternatives,
		descendantPath,
	)
	if err != nil {
		return forwardEffectPlan{}, structuralForwardWork{}, err
	}
	return structuralPlan, structuralWork, nil
}

func requireStructuralWorkDominance(
	label string,
	reservedLabel string,
	reserved structuralForwardWork,
	structural structuralForwardWork,
) error {
	if reserved.state.dominates(structural.state) &&
		reserved.descendant >= structural.descendant &&
		reserved.total.dominates(structural.total) {
		return nil
	}
	return fmt.Errorf(
		"%s does not dominate structural physical work: "+
			"structural state=%+v descendant=%d total=%+v "+
			"%s state=%+v descendant=%d total=%+v",
		label,
		structural.state,
		structural.descendant,
		structural.total,
		reservedLabel,
		reserved.state,
		reserved.descendant,
		reserved.total,
	)
}

func maximumStructuralForwardPlan(
	alternatives []operationplan.Demand,
	descendantPath string,
) (forwardEffectPlan, error) {
	plan := forwardEffectPlan{DescendantPath: descendantPath}
	for _, alternative := range alternatives {
		plan.EnsureCalls = max(plan.EnsureCalls, alternative.EnsureCalls())
		plan.BarrierValidationCalls = max(
			plan.BarrierValidationCalls,
			alternative.BarrierValidationCalls(),
		)
		plan.StateDirValidationCalls = max(
			plan.StateDirValidationCalls,
			alternative.StateDirValidationCalls(),
		)
		plan.DescendantValidations = max(
			plan.DescendantValidations,
			alternative.DescendantValidations(),
		)
		plan.DescendantFileCommits = max(
			plan.DescendantFileCommits,
			alternative.DescendantFileCommits(),
		)
	}
	if plan.DescendantValidations != 0 || plan.DescendantFileCommits != 0 {
		if descendantPath == "" {
			return forwardEffectPlan{}, fmt.Errorf(
				"forward effect structure requires a descendant path binding",
			)
		}
	} else if descendantPath != "" {
		return forwardEffectPlan{}, fmt.Errorf(
			"forward effect descendant path is set without descendant demand",
		)
	}
	return plan, nil
}

func sameForwardPlanCounts(left forwardEffectPlan, right forwardEffectPlan) bool {
	return left.EnsureCalls == right.EnsureCalls &&
		left.BarrierValidationCalls == right.BarrierValidationCalls &&
		left.StateDirValidationCalls == right.StateDirValidationCalls &&
		left.DescendantValidations == right.DescendantValidations &&
		left.DescendantFileCommits == right.DescendantFileCommits
}

func (authority EffectAuthority) lowerStructuralForwardWork(
	alternatives []operationplan.Demand,
	descendantPath string,
) (structuralForwardWork, error) {
	var maximum structuralForwardWork
	for _, alternative := range alternatives {
		path := descendantPath
		if alternative.DescendantValidations() == 0 &&
			alternative.DescendantFileCommits() == 0 {
			path = ""
		}
		lowered, err := authority.lowerForwardPlan(forwardEffectPlan{
			EnsureCalls:             alternative.EnsureCalls(),
			BarrierValidationCalls:  alternative.BarrierValidationCalls(),
			StateDirValidationCalls: alternative.StateDirValidationCalls(),
			DescendantPath:          path,
			DescendantValidations:   alternative.DescendantValidations(),
			DescendantFileCommits:   alternative.DescendantFileCommits(),
		})
		if err != nil {
			return structuralForwardWork{}, err
		}
		maximum.state = maximum.state.maximum(lowered.work.state)
		maximum.total = maximum.total.maximum(lowered.work.total)
		maximum.descendant = max(maximum.descendant, lowered.work.descendant)
	}
	return maximum, nil
}

func (authority EffectAuthority) lowerForwardPlan(
	plan forwardEffectPlan,
) (loweredForwardPlan, error) {
	stateValidations, createIfAbsent, err := forwardStateDirValidationPlan(
		authority.stateDir.PresentAtCapture(),
		plan,
	)
	if err != nil {
		return loweredForwardPlan{}, err
	}
	fileSetCensuses, err := checkedForwardCount(plan.EnsureCalls, 2)
	if err != nil {
		return loweredForwardPlan{}, err
	}
	fileSetCensuses, err = checkedForwardAdd(fileSetCensuses, plan.BarrierValidationCalls)
	if err != nil {
		return loweredForwardPlan{}, err
	}
	planned, err := authority.stateDir.planOperation(
		stateValidations,
		fileSetCensuses,
		createIfAbsent,
		plan.DescendantPath,
		plan.DescendantValidations,
		plan.DescendantFileCommits,
	)
	if err != nil {
		return loweredForwardPlan{}, err
	}
	return loweredForwardPlan{
		planned: planned,
		work: structuralForwardWork{
			state:      planned.stateWork,
			total:      planned.totalWork,
			descendant: planned.descendantWork,
		},
	}, nil
}
