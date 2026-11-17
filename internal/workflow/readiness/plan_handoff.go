package readiness

import (
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/reconcile"
	reconcilehostroute "github.com/isty2e/daem/internal/reconcile/build/hostroute"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
)

type carrierPlanningFacts struct {
	relations []reconcile.RelationAction
	adoptions []carrieradoption.Action
	absences  []carrierabsence.Action
}

type assessmentDecisionPlan struct {
	result      reconcile.Result
	providers   []MCPProviderPrerequisite
	projections []mcpobserve.LockedProjectionObservation
}

func buildCarrierPlanningFacts(input assessmentPlanInput, claims []durablecarrier.ManagedCarrierClaim, pending []durablecarrier.PendingCarrierRemoval, observations relationobserve.Batch) (carrierPlanningFacts, error) {
	relations, err := reconcilehostroute.BuildRelationActions(reconcilehostroute.RelationInput{
		Locked: input.locked, SelectedTargets: input.selectedTargets, Observations: observations,
		CurrentOwner: input.owner, PendingInstalls: input.currentState.PendingCarrierInstalls(),
		PendingRemovals: pending, ManagedClaims: claims,
	})
	if err != nil {
		return carrierPlanningFacts{}, newRelationReconciliationError(err)
	}
	adoptions, err := reconcilehostroute.BuildCarrierAdoptionActions(reconcilehostroute.CarrierAdoptionInput{
		Locked: input.locked, SelectedTargets: input.selectedTargets, Observations: observations,
		CurrentOwner: input.owner, AllClaims: claims, ManageExisting: input.manageUnmanagedMatches, StoreAvailable: true,
	})
	if err != nil {
		return carrierPlanningFacts{}, newRelationReconciliationError(err)
	}
	absences, err := reconcilehostroute.BuildCarrierAbsenceActions(reconcilehostroute.CarrierAbsenceInput{
		Locked: input.locked, SelectedTargets: input.selectedTargets, Observations: observations,
		CurrentOwner: input.owner, AllClaims: claims, PendingRemovals: pending,
		ResolveRoute: reconcilehostroute.ResolveCurrentCarrierRemovalRoute,
	})
	if err != nil {
		return carrierPlanningFacts{}, fmt.Errorf("plan carrier retirements: %w", err)
	}
	return carrierPlanningFacts{relations: relations, adoptions: adoptions, absences: absences}, nil
}

// assembleAssessment publishes only current facts. The after-plan has no
// Snapshot/Registry authority and is sealed as prospective before publication.
func assembleAssessment(input assessmentPlanInput) (Assessment, error) {
	actual, err := buildCarrierPlanningFacts(input, input.allCarrierClaims, input.currentState.PendingCarrierRemovals(), input.relationObservations)
	if err != nil {
		return Assessment{}, err
	}
	var handoffs reconcile.CarrierHandoffs
	if len(actual.relations) != 0 && len(actual.absences) != 0 {
		handoffs, err = reconcile.NewCarrierHandoffs(reconcile.CarrierHandoffsInput{
			CurrentOwner: input.owner, Claims: input.allCarrierClaims, PendingRemovals: input.currentState.PendingCarrierRemovals(),
			Acquisitions: actual.relations, Absences: actual.absences, Observations: input.relationObservations,
		})
		if err != nil {
			return Assessment{}, fmt.Errorf("plan carrier handoffs: %w", err)
		}
	}
	orders, err := planCarrierOrderDecisions(input, actual, actual, handoffs, false)
	if err != nil {
		return Assessment{}, err
	}
	current, err := planAssessmentDecisions(input, actual, orders)
	if err != nil {
		return Assessment{}, err
	}
	var forecastProviders []MCPProviderPrerequisite
	if !handoffs.Empty() {
		projected, err := buildCarrierPlanningFacts(input, handoffs.ProspectiveClaims(), handoffs.ProspectivePendingRemovals(), handoffs.ProspectiveObservations())
		if err != nil {
			return Assessment{}, err
		}
		afterOrders, err := planCarrierOrderDecisions(input, projected, actual, handoffs, true)
		if err != nil {
			return Assessment{}, err
		}
		after, err := planAssessmentDecisions(input, projected, afterOrders)
		if err != nil {
			return Assessment{}, err
		}
		expectation, err := reconcile.NewCarrierHandoffExpectation(handoffs, after.result)
		if err != nil {
			return Assessment{}, err
		}
		current.result, err = current.result.WithCarrierHandoff(expectation)
		if err != nil {
			return Assessment{}, err
		}
		forecastProviders = after.providers
	}
	if current.result.IsProspective() {
		return Assessment{}, fmt.Errorf("current assessment cannot contain prospective reconciliation")
	}
	return Assessment{
		StatePath: input.paths.StatefilePath, CurrentState: input.currentState, GlobalCarrierClaims: input.globalCarrierClaims,
		ManagedPathEvidence: input.managedEvidence, AggregateEvidence: input.aggregateInputs.evidence,
		AggregateFailures: input.aggregateInputs.failures, AggregatePreconditions: input.aggregateInputs.preconditions,
		MCPProjections: current.projections, MCPEffective: input.mcpEffective.Current, MCPProviders: current.providers,
		Reconciliation: current.result, RelationObservations: input.relationObservations,
		Owner: input.owner, OwnerReference: input.ownerReference, Ownership: input.ownershipObservations,
		SelectedTargets: input.selectedTargets, NativeSources: input.nativeSources, forecastProviders: forecastProviders,
	}, nil
}

func planCarrierOrderDecisions(input assessmentPlanInput, planned, actual carrierPlanningFacts, handoffs reconcile.CarrierHandoffs, prospective bool) ([]reconcile.RelationOrderDecision, error) {
	var decisions []reconcile.RelationOrderDecision
	var retiring []durablecarrier.ManagedCarrierClaim
	for _, absence := range actual.absences {
		if absence.Decision() == carrierabsence.DecisionRemove || absence.Decision() == carrierabsence.DecisionVerifyPendingRemoval {
			retiring = append(retiring, absence.Claim())
		}
	}
	for _, fact := range input.extensionOrderFacts {
		events, err := handoffs.OrderHandoffs(fact.selectedTarget, fact.capability.Scope())
		if err != nil {
			return nil, err
		}
		if events.Empty() {
			ordinary, err := planExtensionOrderDecisions([]extensionOrderObservation{fact}, planned.relations, planned.absences)
			if err != nil {
				return nil, err
			}
			decisions = append(decisions, ordinary...)
			continue
		}
		if !prospective {
			continue
		}
		if fact.observeErr != nil {
			blocked, err := blockedOrderDecisions(fact.selectedTarget, fact.capability, fact.constraint, fact.observeErr)
			if err != nil {
				return nil, err
			}
			decisions = append(decisions, blocked...)
			continue
		}
		edits, err := fact.observation.SelectedHandoffEdits(retiring, events)
		if err != nil {
			return nil, fmt.Errorf("plan conditional carrier order: %w", err)
		}
		for _, edit := range edits {
			decision, present, err := DecideSelectedExtensionOrder(edit, planned.relations)
			if err != nil {
				return nil, err
			}
			if present {
				decisions = append(decisions, decision)
			}
		}
	}
	return decisions, nil
}

func (assessment Assessment) ReservationReconciliation() reconcile.Result {
	if expectation, present := assessment.Reconciliation.CarrierHandoff(); present {
		return expectation.After()
	}
	return assessment.Reconciliation.Clone()
}

func (assessment Assessment) ReservationMCPProviders() []MCPProviderPrerequisite {
	if _, present := assessment.Reconciliation.CarrierHandoff(); present {
		return append([]MCPProviderPrerequisite(nil), assessment.forecastProviders...)
	}
	return append([]MCPProviderPrerequisite(nil), assessment.MCPProviders...)
}
