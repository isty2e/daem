package apply

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
)

type carrierHandoffPhaseResult struct {
	retired   runResult
	qualified requalifiedApplyPlan
	schedule  applyForwardEffectSchedule
	settled   bool
}

func runCarrierHandoffPrerequisite(ctx context.Context, qualification applyRequalificationInput, providers []reconcile.RelationAction, options runOptions) (result carrierHandoffPhaseResult, phaseErr error) {
	current := qualification.current
	expectation, present := current.assessment.Reconciliation.CarrierHandoff()
	if !present {
		return result, fmt.Errorf("carrier handoff prerequisite has no actual planning expectation")
	}
	if err := rejectBlockedCarrierHandoffPlan(current.assessment.Reconciliation); err != nil {
		return result, err
	}
	actions := expectation.Handoffs().Retirements()
	_, globals, err := stateOnlyCarrierClaimRetirements(actions)
	if err != nil {
		return result, err
	}
	options.preparedContinuation = qualification.schedule.handoff
	options.currentContinuation = qualification.schedule.handoff
	result.retired, err = runCarrierClaimRetirements(ctx, current.context.Paths,
		execute.ApplyResult{State: current.assessment.CurrentState, StatePath: current.assessment.StatePath},
		current.assessment.GlobalCarrierClaims, globals, actions, current.assessment.RelationObservations, options)
	if err != nil {
		return result, err
	}
	if err := requireCarrierHandoffFrontier(result.retired.State, result.retired.GlobalCarrierClaims, actions); err != nil {
		return result, err
	}
	result.settled = true
	stable, err := carrierHandoffRemainderFingerprint(expectation.After(), actions)
	if err != nil {
		return result, err
	}
	qualification.request.RelationObservations = nil
	qualified, schedule, err := requalifyApplyPlan(ctx, qualification, requalificationAfterCarrierHandoff,
		func(ctx context.Context, fresh *commandPlan) (applyForwardEffectSchedule, error) {
			if requiresCarrierHandoffPhase(fresh.assessment.Reconciliation) || fresh.assessment.Reconciliation.IsProspective() ||
				!fresh.assessment.CurrentState.Equal(result.retired.State) || !fresh.assessment.GlobalCarrierClaims.Equal(result.retired.GlobalCarrierClaims) {
				return applyForwardEffectSchedule{}, staleApplyError(qualification.guard.planWasDisclosed, nil)
			}
			if err := rejectBlockedCarrierHandoffPlan(fresh.assessment.Reconciliation); err != nil {
				return applyForwardEffectSchedule{}, err
			}
			observed, err := carrierHandoffRemainderFingerprint(fresh.assessment.Reconciliation, actions)
			if err != nil || !observed.Equal(stable) {
				return applyForwardEffectSchedule{}, staleApplyError(qualification.guard.planWasDisclosed, err)
			}
			fresh.handoffReservation = actions
			if current.nativePlan != nil {
				if fresh.nativePlan == nil {
					return applyForwardEffectSchedule{}, fmt.Errorf("carrier handoff lost its native continuation")
				}
				if err := prepareNativeInvocationRemainder(ctx, fresh); err != nil {
					return applyForwardEffectSchedule{}, err
				}
			}
			schedule, err := equivalentProviderFinalSchedule(qualification.schedule.finalBinding(), *fresh, providers)
			if err != nil {
				return applyForwardEffectSchedule{}, staleApplyError(qualification.guard.planWasDisclosed, err)
			}
			return schedule, nil
		})
	result.qualified, result.schedule = qualified, schedule
	return result, err
}

func requireCarrierHandoffFrontier(state durable.Snapshot, registry durablecarrier.GlobalCarrierClaims, actions []carrierabsence.Action) error {
	keys := make(map[durablecarrier.CarrierFactKey]struct{}, len(actions))
	for _, action := range actions {
		keys[action.Claim().FactKey()] = struct{}{}
	}
	claims := append(state.ManagedCarrierClaims(), registry.Claims()...)
	for _, claim := range claims {
		if _, present := keys[claim.FactKey()]; present {
			return fmt.Errorf("carrier handoff still has an active old claim")
		}
	}
	for _, pending := range state.PendingCarrierRemovals() {
		if _, present := keys[pending.Claim().FactKey()]; present {
			return fmt.Errorf("carrier handoff still has pending removal debt")
		}
	}
	for _, pending := range state.PendingCarrierInstalls() {
		if _, present := keys[pending.FactKey()]; present {
			return fmt.Errorf("carrier handoff still has pending install debt")
		}
	}
	return nil
}

func carrierHandoffRemainderFingerprint(result reconcile.Result, retirements []carrierabsence.Action) (mutation.OperationFingerprint, error) {
	// Inventory revisions change when the admitted removals edit their documents.
	// Admission and identity/request equality remain mandatory; fresh order and
	// provider qualification own the corresponding conditional observations.
	relations := relationFingerprintRows(result.Relations())
	for index := range relations {
		relations[index].Watchpoints = nil
	}
	var adoptions []carrierAdoptionFingerprintFacts
	for _, action := range result.CarrierAdoptions() {
		adoptions = append(adoptions, carrierAdoptionFingerprintFacts{
			Subject: action.Subject(), Target: string(action.Target()),
			Scope: string(action.Scope()), Result: action.Result(), Blocker: action.Lifecycle().Blocker(), PlanIdentity: action.PlanIdentity(),
		})
	}
	orders := relationOrderFingerprintRows(result.RelationOrders())
	for index, order := range result.RelationOrders() {
		affected := false
		for _, action := range retirements {
			affected = affected || order.Target() == action.Target() && order.Scope() == action.Scope()
		}
		if affected {
			orders[index].Revision = ""
		}
	}
	payload, err := json.Marshal(struct {
		Managed    []managedPathFingerprintFacts
		Aggregates []aggregateFingerprintFacts
		Relations  []relationFingerprintFacts
		Absences   []carrierAbsenceFingerprintFacts
		Adoptions  []carrierAdoptionFingerprintFacts
		Orders     []relationOrderFingerprintFacts
		Delegates  []delegateFingerprintFacts
	}{
		managedPathFingerprintRows(result.ManagedPaths()), aggregateFingerprintRows(result.Aggregates()), relations,
		carrierAbsenceFingerprintRows(result.CarrierAbsences()), adoptions, orders, delegateFingerprintRows(result.Delegates()),
	})
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	return mutation.NewOperationFingerprint(payload), nil
}

func handoffReservationActions(current commandPlan) []carrierabsence.Action {
	if expectation, present := current.assessment.Reconciliation.CarrierHandoff(); present {
		return expectation.Handoffs().Retirements()
	}
	return append([]carrierabsence.Action(nil), current.handoffReservation...)
}

func rejectBlockedCarrierHandoffPlan(result reconcile.Result) error {
	if result.IsProspective() {
		return errProspectiveCarrierHandoff
	}
	if err := rejectBlockedCarrierAbsences(result.CarrierAbsences()); err != nil {
		return err
	}
	admission := result
	hasHandoff := false
	if expectation, present := result.CarrierHandoff(); present {
		admission = expectation.After()
		hasHandoff = true
	} else if requiresCarrierHandoffPhase(result) {
		return errUnsettledCarrierHandoff
	}
	if err := rejectBlockedRelationActions(admission); err != nil {
		return err
	}
	if err := rejectBlockedCarrierAdoptions(admission); err != nil {
		return err
	}
	if hasHandoff {
		return rejectBlockedRelationOrders(admission)
	}
	return nil
}

// handoffPlanningBaseline is a reservation-only projection. Its snapshots are
// never returned as observed successors or installed in a live Assessment.
func handoffPlanningBaseline(current commandPlan) (durable.Snapshot, durablecarrier.GlobalCarrierClaims, error) {
	state, registry := current.assessment.CurrentState, current.assessment.GlobalCarrierClaims
	expectation, present := current.assessment.Reconciliation.CarrierHandoff()
	if !present {
		return state, registry, nil
	}
	var globals []durablecarrier.ManagedCarrierClaim
	for _, action := range expectation.Handoffs().Retirements() {
		claim := action.Claim()
		var err error
		state, _, err = state.WithoutCarrierManagement(claim.Owner(), claim.Identity())
		if err != nil {
			return durable.Snapshot{}, durablecarrier.GlobalCarrierClaims{}, fmt.Errorf("project handoff reservation facts: %w", err)
		}
		if claim.Identity().ClaimStore() == durablecarrier.ClaimStoreGlobalRegistry {
			globals = append(globals, claim)
		}
	}
	registry, err := registry.RetireClaims(globals)
	return state, registry, err
}

func requiresCarrierHandoffPhase(result reconcile.Result) bool {
	if _, present := result.CarrierHandoff(); present {
		return true
	}
	for _, absence := range result.CarrierAbsences() {
		if absence.Desired() == carrierabsence.DesiredReplaced {
			return true
		}
	}
	return false
}
