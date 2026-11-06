package execute

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/claimoperation"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/operationplan"
)

// PrepareApplyEffectPlan compiles the Effect-owned structural Apply plan.
func PrepareApplyEffectPlan(input ApplyInput) (applyprogram.Plan, error) {
	transition, err := deriveApplyStateTransition(input)
	if err != nil {
		return applyprogram.Plan{}, err
	}
	if !transition.changed {
		return compileApplyEffectPlan(
			transition,
			managedPathExecutionSchedule{},
			claimoperation.State{},
			nil,
		)
	}
	managedSchedule, err := newManagedPathExecutionSchedule(input.ManagedPathEffects)
	if err != nil {
		return applyprogram.Plan{}, err
	}
	operationID := forwardEffectPlanningOperationID()
	managedOwnership, err := claimoperation.PlanManagedPaths(
		input.ManagedPathEffects,
		input.Owner,
		input.Ownership,
		operationID,
	)
	if err != nil {
		return applyprogram.Plan{}, fmt.Errorf(
			"derive managed path ownership effect schedule: %w",
			err,
		)
	}
	aggregateOwnership, err := claimoperation.PlanAggregates(
		input.AggregateEffects,
		input.Owner,
		input.Ownership,
		operationID,
	)
	if err != nil {
		return applyprogram.Plan{}, fmt.Errorf(
			"derive managed aggregate ownership effect schedule: %w",
			err,
		)
	}
	return compileApplyEffectPlan(
		transition,
		managedSchedule,
		claimoperation.NewState(managedOwnership, aggregateOwnership),
		input.AggregateEffects,
	)
}

func compileApplyEffectPlan(
	transition applyStateTransition,
	managedSchedule managedPathExecutionSchedule,
	ownershipState claimoperation.State,
	aggregateEffects []effectpreparation.AggregateEffect,
) (applyprogram.Plan, error) {
	var builder operationplan.EffectStructureBuilder
	if !transition.changed {
		return applyprogram.NoOpPlan()
	}
	failureStructure, err := applyprogram.CompileFailureSettlement(
		ownershipState.HasTransitions(),
	)
	if err != nil {
		return applyprogram.Plan{}, err
	}
	promotions, err := applyprogram.NewPromotionSchedule(ownershipState.Intents(), !ownershipState.HasTransitions() && ownershipState.IntentCount() != 0)
	if err != nil {
		return applyprogram.Plan{}, err
	}

	nodes := []operationplan.EffectNode{
		applyprogram.ForwardObligation(
			&builder,
			"apply/journal-publication",
			operationplan.EffectStepPersistence,
		),
		applyprogram.CheckedStep(
			&builder,
			"apply/journal-publication/post-validation",
			operationplan.EffectStepObservation,
		),
	}
	if ownershipState.HasTransitions() {
		nodes = append(nodes, applyprogram.ClaimTransitionObligation(
			&builder,
			"apply/ownership-preparation",
		))
	}
	nodes = append(nodes, applyprogram.CheckedStep(
		&builder,
		"apply/prepared-effects-validation",
		operationplan.EffectStepObservation,
	))
	nodes = append(nodes, ordinaryApplyEffectNodes(&builder, managedSchedule, aggregateEffects, promotions)...)
	if err := promotions.RequireComplete(); err != nil {
		return applyprogram.Plan{}, err
	}
	nodes = append(
		nodes,
		applyprogram.CheckedStep(
			&builder,
			"apply/statefile-publication/context-validation",
			operationplan.EffectStepObservation,
		),
		applyprogram.CheckedStep(
			&builder,
			"apply/statefile-publication/project-validation",
			operationplan.EffectStepObservation,
		),
		applyprogram.ForwardObligation(
			&builder,
			"apply/statefile-publication",
			operationplan.EffectStepPersistence,
		),
		applyprogram.CheckedStep(
			&builder,
			"apply/post-statefile/project-validation",
			operationplan.EffectStepObservation,
		),
		applyprogram.CheckedStep(
			&builder,
			"apply/post-statefile/context-validation",
			operationplan.EffectStepObservation,
		),
	)
	switch {
	case ownershipState.HasTransitions():
		nodes = append(nodes, applyprogram.ClaimTransitionObligation(
			&builder,
			"apply/ownership-finalization",
		))
	case ownershipState.IntentCount() != 0:
		nodes = append(nodes, builder.Conditional(
			applyprogram.OwnershipPromotionTrigger,
			applyprogram.ClaimTransitionObligation(
				&builder,
				"apply/ownership-finalization",
			),
		))
	}
	nodes = append(nodes, applyprogram.JournalRetirementSchedule(&builder)...)
	segment := operationplan.EffectSequence(nodes...)
	structure, err := builder.Compile(builder.ForwardPhase("apply-effect/core", segment))
	if err != nil {
		return applyprogram.Plan{}, err
	}
	return promotions.BindPlan(segment, structure, failureStructure)
}

func applyManagedPathScheduleReference(phase managedPathPhase, effectIndex int) string {
	prefix := "apply/managed-publish"
	if phase == managedPathRetirePhase {
		prefix = "apply/managed-retire"
	}
	return fmt.Sprintf("%s/%d", prefix, effectIndex)
}

func applyAggregateScheduleReference(index int) string {
	return fmt.Sprintf("apply/aggregate/%d", index)
}
