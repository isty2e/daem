package apply

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/execute"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
)

type applyStateDirEffectPlan struct {
	demand    operationplan.Demand
	statefile statefileEffectPlan
	schedule  applyForwardEffectSchedule
}

func stateDirEffectPlanFor(
	current commandPlan,
	providerActions []reconcile.RelationAction,
) (applyStateDirEffectPlan, error) {
	applyInput, err := applyEffectInput(current)
	if err != nil {
		return applyStateDirEffectPlan{}, err
	}
	envelope, executeGates, err := applyEnvelopeFor(current, providerActions, applyInput)
	if err != nil {
		return applyStateDirEffectPlan{}, err
	}
	schedule, err := compileApplyForwardEffectScheduleWithEnvelope(
		current,
		providerActions,
		applyInput,
		envelope,
		executeGates,
	)
	if err != nil {
		return applyStateDirEffectPlan{}, err
	}
	plan := applyPlanFromDemand(envelope.Demand())
	plan.schedule = schedule
	return plan, nil
}

func applyEffectInput(current commandPlan) (execute.ApplyInput, error) {
	managedEffects, err := execute.ManagedPathEffects(
		current.assessment.Reconciliation.ManagedPaths(),
	)
	if err != nil {
		return execute.ApplyInput{}, err
	}
	aggregateEffects, err := execute.AggregateEffects(
		current.assessment.Reconciliation.Aggregates(),
	)
	if err != nil {
		return execute.ApplyInput{}, err
	}
	projectRetirements, _, err := stateOnlyCarrierClaimRetirements(
		current.assessment.Reconciliation.CarrierAbsences(),
	)
	if err != nil {
		return execute.ApplyInput{}, err
	}
	projectAdoptions, _, err := stateOnlyCarrierClaimAdoptions(
		current.assessment.CurrentState,
		current.assessment.Reconciliation.CarrierAdoptions(),
	)
	if err != nil {
		return execute.ApplyInput{}, err
	}
	return execute.ApplyInput{
		ManagedPathEffects:          managedEffects,
		AggregateEffects:            aggregateEffects,
		CurrentState:                current.assessment.CurrentState,
		GlobalCarrierClaims:         current.assessment.GlobalCarrierClaims,
		RetiredProjectCarrierClaims: projectRetirements,
		AdoptedProjectCarrierClaims: projectAdoptions,
		ConfirmedRelationActions:    nonProviderRelationActions(current),
		Owner:                       current.assessment.Owner,
		Ownership:                   current.assessment.Ownership,
	}, nil
}

func applyEnvelopeFor(
	current commandPlan,
	providerActions []reconcile.RelationAction,
	applyInput execute.ApplyInput,
) (operationplan.Envelope, int, error) {
	executeGates, err := execute.MaximumForwardEffectValidationCount(applyInput)
	if err != nil {
		return operationplan.Envelope{}, 0, err
	}
	orderClasses, err := admittedOrderClasses(current)
	if err != nil {
		return operationplan.Envelope{}, 0, err
	}
	envelope, err := operationplan.CompileApply(operationplan.ApplyWork{
		ExecuteGates:    executeGates,
		ProviderActions: routeWorks(current.assessment.CurrentState, providerActions),
		FinalRoutes: routeWorks(
			current.assessment.CurrentState,
			nonProviderRelationActions(current),
		),
		CarrierRemovals: carrierWorks(current.assessment.Reconciliation.CarrierAbsences()),
		OrderClasses:    orderClasses,
		Delegates:       delegateWorks(current.assessment.Reconciliation.Delegates()),
		StatefilePath:   current.assessment.StatePath,
	})
	if err != nil {
		return operationplan.Envelope{}, 0, err
	}
	return envelope, executeGates, nil
}

func applyPlanFromDemand(demand operationplan.Demand) applyStateDirEffectPlan {
	return applyStateDirEffectPlan{
		demand: demand,
		statefile: statefileEffectPlan{
			validations: demand.DescendantValidations(),
			fileCommits: demand.DescendantFileCommits(),
		},
	}
}

func statefileEffectPlanFor(
	current durable.Snapshot,
	reconciliation reconcile.Result,
) (statefileEffectPlan, error) {
	envelope, err := operationplan.CompileApply(operationplan.ApplyWork{
		FinalRoutes:     routeWorks(current, reconciliation.Relations()),
		CarrierRemovals: carrierWorks(reconciliation.CarrierAbsences()),
		Delegates:       delegateWorks(reconciliation.Delegates()),
	})
	if err != nil {
		return statefileEffectPlan{}, err
	}
	return statefilePlanFromDemand(envelope.Demand()), nil
}

func hostRouteStatefileEffectPlan(
	current durable.Snapshot,
	actions []reconcile.RelationAction,
) (statefileEffectPlan, error) {
	envelope, err := operationplan.CompileApply(operationplan.ApplyWork{
		FinalRoutes: routeWorks(current, actions),
	})
	if err != nil {
		return statefileEffectPlan{}, err
	}
	return statefilePlanFromDemand(envelope.Demand()), nil
}

func carrierRemovalStatefileEffectPlan(
	actions []carrierabsence.Action,
) (statefileEffectPlan, error) {
	envelope, err := operationplan.CompileApply(operationplan.ApplyWork{
		CarrierRemovals: carrierWorks(actions),
	})
	if err != nil {
		return statefileEffectPlan{}, err
	}
	return statefilePlanFromDemand(envelope.Demand()), nil
}

func delegateStatefileEffectPlan(
	actions []reconcile.DelegateAction,
) (statefileEffectPlan, error) {
	envelope, err := operationplan.CompileApply(operationplan.ApplyWork{
		Delegates: delegateWorks(actions),
	})
	if err != nil {
		return statefileEffectPlan{}, err
	}
	return statefilePlanFromDemand(envelope.Demand()), nil
}

func statefilePlanFromDemand(demand operationplan.Demand) statefileEffectPlan {
	return statefileEffectPlan{
		validations: demand.DescendantValidations(),
		fileCommits: demand.DescendantFileCommits(),
	}
}

func routeWorks(
	current durable.Snapshot,
	actions []reconcile.RelationAction,
) []operationplan.RouteWork {
	works := make([]operationplan.RouteWork, 0, len(actions))
	for _, action := range actions {
		works = append(works, operationplan.RouteWork{
			InvokesHost: action.InvokesHostRoute(),
			Global:      action.Scope() == target.ScopeGlobal,
			Promotion:   isGlobalCarrierPromotionCandidate(current, action),
		})
	}
	return works
}

func carrierWorks(actions []carrierabsence.Action) []operationplan.CarrierWork {
	works := make([]operationplan.CarrierWork, 0, len(actions))
	for _, action := range actions {
		works = append(works, operationplan.CarrierWork{
			InvokesHost:     action.InvokesHostRoute(),
			MutatesDirect:   action.MutatesDirectProjection(),
			VerifiesPending: action.VerifiesPendingRemoval(),
		})
	}
	return works
}

func delegateWorks(actions []reconcile.DelegateAction) []operationplan.DelegateWork {
	works := make([]operationplan.DelegateWork, 0, len(actions))
	for _, action := range actions {
		works = append(works, operationplan.DelegateWork{
			SchedulesAttempt: action.SchedulesAttempt(),
			Blocked:          action.Disposition() == reconcile.DelegateBlocked,
		})
	}
	return works
}

type admittedOrderClass struct {
	target      target.Target
	scope       target.Scope
	classID     string
	fingerprint string
	decisions   []reconcile.RelationOrderDecision
}

func admittedOrderClasses(current commandPlan) ([]operationplan.OrderClassWork, error) {
	classes, err := admittedOrderClassFacts(current)
	if err != nil {
		return nil, err
	}
	works := make([]operationplan.OrderClassWork, 0, len(classes))
	for _, class := range classes {
		works = append(works, operationplan.OrderClassWork{
			RequiresMutation: relationOrderMutationRequired(class.decisions),
		})
	}
	return works, nil
}

func admittedOrderClassFacts(current commandPlan) ([]admittedOrderClass, error) {
	decisions := current.assessment.Reconciliation.RelationOrders()
	classes := make(map[string][]reconcile.RelationOrderDecision)
	for _, decision := range decisions {
		key := string(decision.ClassID())
		classes[key] = append(classes[key], decision)
	}
	selected := make(map[string]struct{})
	for _, selectedTarget := range current.context.Selection.Targets() {
		selected[string(selectedTarget)] = struct{}{}
	}
	result := make([]admittedOrderClass, 0, len(classes))
	for _, constraint := range current.context.Lockfile.Locked.OrderConstraints() {
		classID := string(constraint.ClassID())
		classDecisions, planned := classes[classID]
		if !planned {
			continue
		}
		selectedTarget, capability, admitted := hostsurfacecatalog.Product().ExtensionOrderCapabilityForClass(
			constraint.ClassID(),
		)
		if !admitted {
			return nil, fmt.Errorf(
				"locked extension order class %q has no unique profile owner",
				constraint.ClassID(),
			)
		}
		if _, ok := selected[string(selectedTarget)]; !ok {
			continue
		}
		result = append(result, admittedOrderClass{
			target:      selectedTarget,
			scope:       capability.Scope(),
			classID:     classID,
			fingerprint: constraint.Fingerprint(),
			decisions:   append([]reconcile.RelationOrderDecision(nil), classDecisions...),
		})
	}
	if len(result) != len(classes) {
		return nil, fmt.Errorf(
			"planned extension order matched %d locked classes, want %d",
			len(result),
			len(classes),
		)
	}
	return result, nil
}

func orderClassWorks(decisions []reconcile.RelationOrderDecision) []operationplan.OrderClassWork {
	classes := make(map[string][]reconcile.RelationOrderDecision)
	for _, decision := range decisions {
		key := string(decision.ClassID())
		classes[key] = append(classes[key], decision)
	}
	works := make([]operationplan.OrderClassWork, 0, len(classes))
	for _, classDecisions := range classes {
		works = append(works, operationplan.OrderClassWork{
			RequiresMutation: relationOrderMutationRequired(classDecisions),
		})
	}
	return works
}

func relationOrderValidationCount(
	decisions []reconcile.RelationOrderDecision,
	mayChangeBeforeExecution bool,
) (int, error) {
	work := operationplan.ApplyWork{OrderClasses: orderClassWorks(decisions)}
	if mayChangeBeforeExecution {
		work.CarrierRemovals = []operationplan.CarrierWork{{VerifiesPending: true}}
	}
	envelope, err := operationplan.CompileApply(work)
	if err != nil {
		return 0, err
	}
	for _, obligation := range envelope.Obligations() {
		if obligation.Kind() == operationplan.ObligationRelationOrderClass {
			return obligation.Count(), nil
		}
	}
	return 0, nil
}

func relationOrderMayReclassifyBeforeExecution(
	providerActions []reconcile.RelationAction,
	finalRelations []reconcile.RelationAction,
	carrierAbsences []carrierabsence.Action,
) bool {
	return operationplan.ApplyWork{
		ProviderActions: routeWorks(durable.Snapshot{}, providerActions),
		FinalRoutes:     routeWorks(durable.Snapshot{}, finalRelations),
		CarrierRemovals: carrierWorks(carrierAbsences),
	}.MayReclassifyRelationOrder()
}
