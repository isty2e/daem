package apply

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/execute"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/operationplan"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
)

// applyForwardEffectSchedule is the exact Apply-owned structural schedule used
// for provider-suffix comparison and structural State Barrier lowering. The
// Effect-owned core is consumed here; rollback and outer continuations remain
// later migration units.
type applyForwardEffectSchedule struct {
	full         operationplan.EffectStructure
	final        operationplan.EffectStructure
	continuation applyContinuationPlan
	effectPlan   execute.ApplyEffectPlan
}

func equivalentProviderFinalSchedule(
	reserved operationplan.EffectStructure,
	current commandPlan,
	providerActions []reconcile.RelationAction,
) (applyForwardEffectSchedule, error) {
	applyInput, err := applyEffectInput(current)
	if err != nil {
		return applyForwardEffectSchedule{}, err
	}
	currentSchedule, err := compileApplyForwardEffectSchedule(
		current,
		providerActions,
		applyInput,
	)
	if err != nil {
		return applyForwardEffectSchedule{}, err
	}
	if !reserved.Equal(currentSchedule.final) {
		return applyForwardEffectSchedule{}, fmt.Errorf(
			"reserved and current final apply effect schedules differ",
		)
	}
	return currentSchedule, nil
}

func compileApplyForwardEffectSchedule(
	current commandPlan,
	providerActions []reconcile.RelationAction,
	applyInput execute.ApplyInput,
) (applyForwardEffectSchedule, error) {
	envelope, executeGates, err := applyEnvelopeFor(current, providerActions, applyInput)
	if err != nil {
		return applyForwardEffectSchedule{}, err
	}
	return compileApplyForwardEffectScheduleWithEnvelope(
		current,
		providerActions,
		applyInput,
		envelope,
		executeGates,
	)
}

func compileApplyForwardEffectScheduleWithEnvelope(
	current commandPlan,
	providerActions []reconcile.RelationAction,
	applyInput execute.ApplyInput,
	envelope operationplan.Envelope,
	executeGates int,
) (applyForwardEffectSchedule, error) {
	effectPlan, err := execute.PrepareApplyEffectPlan(applyInput)
	if err != nil {
		return applyForwardEffectSchedule{}, err
	}
	input, err := applyScheduleInputFor(
		current,
		providerActions,
		effectPlan.Segment(),
		executeGates,
	)
	if err != nil {
		return applyForwardEffectSchedule{}, err
	}
	schedule, err := compileApplySchedule(input, envelope.Demand())
	if err != nil {
		return applyForwardEffectSchedule{}, err
	}
	schedule.effectPlan = effectPlan
	return schedule, nil
}

func applyScheduleInputFor(
	current commandPlan,
	providerActions []reconcile.RelationAction,
	effectSegment operationplan.EffectNode,
	executeGates int,
) (applyScheduleInput, error) {
	_, globalRetirements, err := stateOnlyCarrierClaimRetirements(
		current.assessment.Reconciliation.CarrierAbsences(),
	)
	if err != nil {
		return applyScheduleInput{}, err
	}
	_, globalAdoptions, err := stateOnlyCarrierClaimAdoptions(
		current.assessment.CurrentState,
		current.assessment.Reconciliation.CarrierAdoptions(),
	)
	if err != nil {
		return applyScheduleInput{}, err
	}
	orderClasses, err := admittedOrderClassFacts(current)
	if err != nil {
		return applyScheduleInput{}, err
	}
	providerRoutes, err := applyRouteScheduleFacts(
		"apply/provider/route",
		current.assessment.CurrentState,
		providerActions,
		current.context.Lockfile,
		current.context.Paths.ManifestRoot,
	)
	if err != nil {
		return applyScheduleInput{}, err
	}
	finalRoutes, err := applyRouteScheduleFacts(
		"apply/final/route",
		current.assessment.CurrentState,
		nonProviderRelationActions(current),
		current.context.Lockfile,
		current.context.Paths.ManifestRoot,
	)
	if err != nil {
		return applyScheduleInput{}, err
	}
	carrierRemovals, err := applyCarrierScheduleFacts(
		current.assessment.Reconciliation.CarrierAbsences(),
	)
	if err != nil {
		return applyScheduleInput{}, err
	}
	orders := applyOrderScheduleFacts(orderClasses)
	delegates := applyDelegateScheduleFacts(
		current.assessment.Reconciliation.Delegates(),
	)
	return applyScheduleInput{
		providerRoutes:      providerRoutes,
		coreChanged:         executeGates != 0,
		core:                effectSegment,
		hasGlobalRetirement: len(globalRetirements) != 0,
		carrierRemovals:     carrierRemovals,
		finalRoutes:         finalRoutes,
		orderClasses:        orders,
		mayReclassifyOrder: relationOrderMayReclassifyBeforeExecution(
			providerActions,
			nonProviderRelationActions(current),
			current.assessment.Reconciliation.CarrierAbsences(),
		),
		delegates:         delegates,
		hasGlobalAdoption: len(globalAdoptions) != 0,
	}, nil
}

func applyRouteScheduleFacts(
	prefix string,
	currentState durable.Snapshot,
	actions []reconcile.RelationAction,
	locked lock.File,
	workDir string,
) ([]applyRouteScheduleFact, error) {
	works := routeWorks(currentState, actions)
	result := make([]applyRouteScheduleFact, 0, len(actions))
	for index := range actions {
		preflightRejected := false
		if works[index].InvokesHost {
			_, err := executehostroute.BuildCommand(executehostroute.BuildInput{
				Action:   actions[index],
				Lockfile: locked,
				WorkDir:  workDir,
			})
			preflightRejected = err != nil
		}
		result = append(result, applyRouteScheduleFact{
			ref:               applyOrdinalScheduleReference(prefix, index),
			action:            actions[index],
			work:              works[index],
			preflightRejected: preflightRejected,
		})
	}
	return result, nil
}

func applyOrderScheduleFacts(classes []admittedOrderClass) []applyOrderScheduleFact {
	result := make([]applyOrderScheduleFact, 0, len(classes))
	for index, class := range classes {
		result = append(result, applyOrderScheduleFact{
			ref:              applyOrdinalScheduleReference("apply/relation-order", index),
			classID:          class.classID,
			requiresMutation: relationOrderMutationRequired(class.decisions),
		})
	}
	return result
}

func applyDelegateScheduleFacts(
	actions []reconcile.DelegateAction,
) []applyDelegateScheduleFact {
	works := delegateWorks(actions)
	result := make([]applyDelegateScheduleFact, 0, len(actions))
	for index := range actions {
		result = append(result, applyDelegateScheduleFact{
			ref:    applyOrdinalScheduleReference("apply/delegate", index),
			action: actions[index],
			work:   works[index],
		})
	}
	return result
}

// Schedule references identify structural positions only; operation fingerprints
// and authority checks retain semantic plan identity.
func applyOrdinalScheduleReference(prefix string, index int) string {
	return fmt.Sprintf("%s/%06d", prefix, index)
}
