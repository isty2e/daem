package apply

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/recoverygate"
)

type applyStateDirEffectPlan struct {
	forward   recoverygate.ForwardEffectPlan
	statefile statefileEffectPlan
}

func stateDirEffectPlanFor(
	current commandPlan,
	providerActions []reconcile.RelationAction,
) (applyStateDirEffectPlan, error) {
	nonProviderRelations := nonProviderRelationActions(current)
	statefilePlan, err := crossPhaseStatefileEffectPlan(
		current.assessment.CurrentState,
		current.assessment.Reconciliation,
		providerActions,
		nonProviderRelations,
	)
	if err != nil {
		return applyStateDirEffectPlan{}, err
	}
	managedEffects, err := execute.ManagedPathEffects(
		current.assessment.Reconciliation.ManagedPaths(),
	)
	if err != nil {
		return applyStateDirEffectPlan{}, err
	}
	aggregateEffects, err := execute.AggregateEffects(
		current.assessment.Reconciliation.Aggregates(),
	)
	if err != nil {
		return applyStateDirEffectPlan{}, err
	}
	projectRetirements, _, err := stateOnlyCarrierClaimRetirements(
		current.assessment.Reconciliation.CarrierAbsences(),
	)
	if err != nil {
		return applyStateDirEffectPlan{}, err
	}
	projectAdoptions, _, err := stateOnlyCarrierClaimAdoptions(
		current.assessment.CurrentState,
		current.assessment.Reconciliation.CarrierAdoptions(),
	)
	if err != nil {
		return applyStateDirEffectPlan{}, err
	}
	coreCalls, err := execute.MaximumForwardEffectValidationCount(execute.ApplyInput{
		ManagedPathEffects:          managedEffects,
		AggregateEffects:            aggregateEffects,
		CurrentState:                current.assessment.CurrentState,
		GlobalCarrierClaims:         current.assessment.GlobalCarrierClaims,
		RetiredProjectCarrierClaims: projectRetirements,
		AdoptedProjectCarrierClaims: projectAdoptions,
		ConfirmedRelationActions:    nonProviderRelations,
		Owner:                       current.assessment.Owner,
		Ownership:                   current.assessment.Ownership,
	})
	if err != nil {
		return applyStateDirEffectPlan{}, err
	}

	finalEffectCalls := coreCalls
	hostCalls, err := hostRouteEffectValidationCount(
		current.assessment.CurrentState,
		nonProviderRelations,
	)
	if err != nil {
		return applyStateDirEffectPlan{}, err
	}
	finalEffectCalls, err = checkedStatefileEffectCount(finalEffectCalls, hostCalls)
	if err != nil {
		return applyStateDirEffectPlan{}, err
	}
	carrierCalls := carrierRemovalEffectValidationCount(
		current.assessment.Reconciliation.CarrierAbsences(),
	)
	finalEffectCalls, err = checkedStatefileEffectCount(finalEffectCalls, carrierCalls)
	if err != nil {
		return applyStateDirEffectPlan{}, err
	}
	orderCalls, err := maximumRelationOrderEffectCount(
		current,
		providerActions,
		nonProviderRelations,
	)
	if err != nil {
		return applyStateDirEffectPlan{}, err
	}
	finalEffectCalls, err = checkedStatefileEffectCount(finalEffectCalls, orderCalls)
	if err != nil {
		return applyStateDirEffectPlan{}, err
	}

	delegateActions := current.assessment.Reconciliation.Delegates()
	stateDirOnlyCalls := 0
	if delegateActionsRequireAttemptPersistence(delegateActions) {
		finalEffectCalls, err = checkedStatefileEffectCount(finalEffectCalls, 1)
		if err != nil {
			return applyStateDirEffectPlan{}, err
		}
	} else {
		stateDirOnlyCalls, err = checkedStatefileEffectCount(len(delegateActions), len(delegateActions))
		if err != nil {
			return applyStateDirEffectPlan{}, err
		}
	}

	providerCalls := len(providerActions)
	ensureCalls := 0
	if providerCalls != 0 {
		ensureCalls++
	}
	if finalEffectCalls != 0 {
		ensureCalls++
	}
	stateDirEffectCalls, err := checkedStatefileEffectCount(providerCalls, finalEffectCalls)
	if err != nil {
		return applyStateDirEffectPlan{}, err
	}
	stateDirEffectCalls -= ensureCalls
	stateDirOnlyCalls, err = checkedStatefileEffectCount(stateDirOnlyCalls, stateDirEffectCalls)
	if err != nil {
		return applyStateDirEffectPlan{}, err
	}
	barrierCalls := 0
	if providerCalls != 0 {
		// Post-provider execution performs two barrier-preserving replans plus
		// one explicit validation after acquiring rebound leases.
		barrierCalls = 3
	}
	descendantPath := ""
	if !statefilePlan.empty() {
		descendantPath = current.assessment.StatePath
	}
	return applyStateDirEffectPlan{
		statefile: statefilePlan,
		forward: recoverygate.ForwardEffectPlan{
			EnsureCalls:             ensureCalls,
			BarrierValidationCalls:  barrierCalls,
			StateDirValidationCalls: stateDirOnlyCalls,
			DescendantPath:          descendantPath,
			DescendantValidations:   statefilePlan.validations,
			DescendantFileCommits:   statefilePlan.fileCommits,
		},
	}, nil
}

func crossPhaseStatefileEffectPlan(
	current durable.Snapshot,
	reconciliation reconcile.Result,
	providerActions []reconcile.RelationAction,
	finalRelations []reconcile.RelationAction,
) (statefileEffectPlan, error) {
	provider, err := hostRouteStatefileEffectPlan(current, providerActions)
	if err != nil {
		return statefileEffectPlan{}, err
	}
	finalHost, err := hostRouteStatefileEffectPlan(current, finalRelations)
	if err != nil {
		return statefileEffectPlan{}, err
	}
	carrier, err := carrierRemovalStatefileEffectPlan(reconciliation.CarrierAbsences())
	if err != nil {
		return statefileEffectPlan{}, err
	}
	delegate, err := delegateStatefileEffectPlan(reconciliation.Delegates())
	if err != nil {
		return statefileEffectPlan{}, err
	}
	for _, addition := range []statefileEffectPlan{finalHost, carrier, delegate} {
		if err := provider.add(addition.validations, addition.fileCommits); err != nil {
			return statefileEffectPlan{}, err
		}
	}
	return provider, nil
}

func hostRouteEffectValidationCount(
	current durable.Snapshot,
	actions []reconcile.RelationAction,
) (int, error) {
	invocations := 0
	promotion := false
	for _, action := range actions {
		switch {
		case action.InvokesHostRoute():
			var err error
			invocations, err = checkedStatefileEffectCount(invocations, 1)
			if err != nil {
				return 0, err
			}
		case isGlobalCarrierPromotionCandidate(current, action):
			promotion = true
		}
	}
	if promotion {
		return checkedStatefileEffectCount(invocations, 1)
	}
	return invocations, nil
}

func carrierRemovalEffectValidationCount(actions []carrierabsence.Action) int {
	count := 0
	for _, action := range actions {
		if action.InvokesHostRoute() || action.MutatesDirectProjection() ||
			action.VerifiesPendingRemoval() {
			count++
		}
	}
	return count
}

func maximumRelationOrderEffectCount(
	current commandPlan,
	providerActions []reconcile.RelationAction,
	finalRelations []reconcile.RelationAction,
) (int, error) {
	decisions := current.assessment.Reconciliation.RelationOrders()
	classes := make(map[string]struct{})
	for _, decision := range decisions {
		classes[string(decision.ClassID())] = struct{}{}
	}
	selected := make(map[string]struct{})
	for _, selectedTarget := range current.context.Selection.Targets() {
		selected[string(selectedTarget)] = struct{}{}
	}
	matchedClasses := 0
	for _, constraint := range current.context.Lockfile.Locked.OrderConstraints() {
		if _, planned := classes[string(constraint.ClassID())]; !planned {
			continue
		}
		selectedTarget, _, admitted := profile.ExtensionOrderCapabilityForClass(
			constraint.ClassID(),
		)
		if !admitted {
			return 0, fmt.Errorf(
				"locked extension order class %q has no unique profile owner",
				constraint.ClassID(),
			)
		}
		if _, ok := selected[string(selectedTarget)]; !ok {
			continue
		}
		matchedClasses++
	}
	if matchedClasses != len(classes) {
		return 0, fmt.Errorf(
			"planned extension order matched %d locked classes, want %d",
			matchedClasses,
			len(classes),
		)
	}
	mayChange := relationOrderMayReclassifyBeforeExecution(
		providerActions,
		finalRelations,
		current.assessment.Reconciliation.CarrierAbsences(),
	)
	return relationOrderValidationCount(decisions, mayChange)
}

func relationOrderMayReclassifyBeforeExecution(
	providerActions []reconcile.RelationAction,
	finalRelations []reconcile.RelationAction,
	carrierAbsences []carrierabsence.Action,
) bool {
	for _, actions := range [][]reconcile.RelationAction{providerActions, finalRelations} {
		for _, action := range actions {
			if action.InvokesHostRoute() {
				return true
			}
		}
	}
	for _, action := range carrierAbsences {
		if action.InvokesHostRoute() || action.MutatesDirectProjection() ||
			action.VerifiesPendingRemoval() {
			return true
		}
	}
	return false
}

func relationOrderValidationCount(
	decisions []reconcile.RelationOrderDecision,
	mayChangeBeforeExecution bool,
) (int, error) {
	byClass := make(map[string][]reconcile.RelationOrderDecision)
	for _, decision := range decisions {
		key := string(decision.ClassID())
		byClass[key] = append(byClass[key], decision)
	}
	count := 0
	for _, classDecisions := range byClass {
		if !mayChangeBeforeExecution && !relationOrderMutationRequired(classDecisions) {
			continue
		}
		var err error
		count, err = checkedStatefileEffectCount(count, 1)
		if err != nil {
			return 0, err
		}
	}
	return count, nil
}
