package readiness

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe"
	mcpeffectivehost "github.com/isty2e/daem/internal/assurance/observe/mcp/effective/host"
	mcpprovider "github.com/isty2e/daem/internal/assurance/observe/mcp/provider"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	reconcilehostroute "github.com/isty2e/daem/internal/reconcile/build/hostroute"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

type assessmentPlanInput struct {
	paths                  daempaths.Paths
	environment            desired.Environment
	locked                 lock.File
	selection              targetselection.Selection
	selectedTargets        reconcile.SelectedTargets
	supplyObservations     []observe.ExactSupplyObservation
	currentState           durable.Snapshot
	globalCarrierClaims    durablecarrier.GlobalCarrierClaims
	allCarrierClaims       []durablecarrier.ManagedCarrierClaim
	manageUnmanagedMatches bool
	codecs                 aggregate.CodecCatalog
	operationContext       reconcile.OperationContext
	managedInputs          managedPathPlanningInputs
	aggregateInputs        managedAggregatePlanningInputs
	managedEvidence        []observe.ManagedPathEvidence
	owner                  stateauthority.Authority
	ownershipObservations  []observe.OwnershipObservation
	mcpEffective           mcpeffectivehost.ObservationSet
	relationObservations   relationobserve.Batch
	providerObservations   []mcpprovider.Observation
	extensionOrderFacts    []extensionOrderObservation
	mcpContracts           []lock.LockedSubjectContract
}

// assembleAssessment classifies observed facts into Assessment. It performs
// no filesystem, subprocess, or persistence I/O.
func assembleAssessment(input assessmentPlanInput) (Assessment, error) {
	effectiveConstraints, err := providerEffectiveConstraints(input.mcpEffective.Current)
	if err != nil {
		return Assessment{}, err
	}
	effectiveRemovalNotices, err := providerEffectiveRemovalNotices(
		input.mcpEffective.Retiring,
	)
	if err != nil {
		return Assessment{}, err
	}
	relationActions, err := reconcilehostroute.BuildRelationActions(reconcilehostroute.RelationInput{
		Locked:          input.locked,
		SelectedTargets: input.selectedTargets,
		Observations:    input.relationObservations,
		CurrentOwner:    input.owner,
		PendingInstalls: input.currentState.PendingCarrierInstalls(),
		ManagedClaims:   input.allCarrierClaims,
	})
	if err != nil {
		return Assessment{}, newRelationReconciliationError(err)
	}
	providerPrerequisites, err := planMCPProviderPrerequisites(
		input.locked,
		input.providerObservations,
		relationActions,
	)
	if err != nil {
		return Assessment{}, fmt.Errorf("plan MCP provider prerequisites: %w", err)
	}
	providerConstraints, err := providerPrerequisiteConstraints(providerPrerequisites)
	if err != nil {
		return Assessment{}, err
	}
	aggregateConstraints := append(effectiveConstraints, providerConstraints...)

	managedPaths, aggregates, err := buildProjectionDecisions(projectionPlanningInput{
		environment:             input.environment,
		locked:                  input.locked.Locked,
		selectedTargets:         input.selectedTargets,
		supplyObservations:      input.supplyObservations,
		managedPathStates:       input.managedInputs.states,
		managedPathEvidence:     input.managedEvidence,
		aggregateExpected:       input.aggregateInputs.expected,
		aggregateDesired:        input.aggregateInputs.desired,
		aggregateConstraints:    aggregateConstraints,
		aggregateRemovalNotices: effectiveRemovalNotices,
		aggregateStates:         input.aggregateInputs.states,
		aggregateEvidence:       input.aggregateInputs.evidence,
		aggregateFailures:       input.aggregateInputs.failures,
		aggregatePreconditions:  input.aggregateInputs.preconditions,
		manageUnmanagedMatches:  input.manageUnmanagedMatches,
		owner:                   input.owner,
		ownership:               input.ownershipObservations,
		codecs:                  input.codecs,
	})
	if err != nil {
		return Assessment{}, err
	}
	carrierAdoptionActions, err := reconcilehostroute.BuildCarrierAdoptionActions(
		reconcilehostroute.CarrierAdoptionInput{
			Locked:          input.locked,
			SelectedTargets: input.selectedTargets,
			Observations:    input.relationObservations,
			CurrentOwner:    input.owner,
			AllClaims:       input.allCarrierClaims,
			ManageExisting:  input.manageUnmanagedMatches,
			StoreAvailable:  true,
		},
	)
	if err != nil {
		return Assessment{}, newRelationReconciliationError(err)
	}
	carrierAbsenceActions, err := reconcilehostroute.BuildCarrierAbsenceActions(
		reconcilehostroute.CarrierAbsenceInput{
			Locked:          input.locked,
			SelectedTargets: input.selectedTargets,
			Observations:    input.relationObservations,
			CurrentOwner:    input.owner,
			AllClaims:       input.allCarrierClaims,
			PendingRemovals: input.currentState.PendingCarrierRemovals(),
			ResolveRoute:    reconcilehostroute.ResolveCurrentCarrierRemovalRoute,
		},
	)
	if err != nil {
		return Assessment{}, fmt.Errorf("plan carrier absences: %w", err)
	}
	relationOrderDecisions, err := planExtensionOrderDecisions(
		input.extensionOrderFacts,
		relationActions,
		carrierAbsenceActions,
	)
	if err != nil {
		return Assessment{}, fmt.Errorf("plan extension order: %w", err)
	}
	delegateActions, err := reconcilehostroute.BuildDelegateActions(reconcilehostroute.DelegateInput{
		Locked:              input.locked,
		SelectedTargets:     input.selectedTargets,
		Context:             input.operationContext,
		BlockedDependencies: blockedProjectionDependencies(managedPaths, aggregates),
	})
	if err != nil {
		return Assessment{}, fmt.Errorf("plan delegate actions: %w", err)
	}
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:          input.operationContext,
		ManagedPaths:     managedPaths,
		Aggregates:       aggregates,
		Relations:        relationActions,
		RelationOrders:   relationOrderDecisions,
		CarrierAdoptions: carrierAdoptionActions,
		CarrierAbsences:  carrierAbsenceActions,
		Delegates:        delegateActions,
	})
	if err != nil {
		return Assessment{}, fmt.Errorf("assemble complete reconciliation result: %w", err)
	}
	mcpProjections, err := classifyMCPProjections(
		input.mcpContracts,
		input.currentState,
		input.aggregateInputs.evidence,
		input.aggregateInputs.failures,
		input.aggregateInputs.preconditions,
		input.mcpEffective.Current,
		providerPrerequisites,
	)
	if err != nil {
		return Assessment{}, fmt.Errorf("inspect MCP projection status: %w", err)
	}

	return Assessment{
		StatePath:              input.paths.StatefilePath,
		CurrentState:           input.currentState,
		GlobalCarrierClaims:    input.globalCarrierClaims,
		ManagedPathEvidence:    input.managedEvidence,
		AggregateEvidence:      input.aggregateInputs.evidence,
		AggregateFailures:      input.aggregateInputs.failures,
		AggregatePreconditions: input.aggregateInputs.preconditions,
		MCPProjections:         mcpProjections,
		MCPEffective:           input.mcpEffective.Current,
		MCPProviders:           providerPrerequisites,
		Reconciliation:         result,
		RelationObservations:   input.relationObservations,
		Owner:                  input.owner,
		Ownership:              input.ownershipObservations,
		SelectedTargets:        input.selectedTargets,
	}, nil
}

type outputInventoryPlanInput struct {
	environment     desired.Environment
	locked          lock.File
	selection       targetselection.Selection
	selectedTargets reconcile.SelectedTargets
	currentState    durable.Snapshot
	managedInputs   managedPathPlanningInputs
	aggregateInputs managedAggregatePlanningInputs
	managedEvidence []observe.ManagedPathEvidence
	owner           stateauthority.Authority
	ownership       []observe.OwnershipObservation
	codecs          aggregate.CodecCatalog
}

func planOutputInventory(input outputInventoryPlanInput) (OutputInventoryAssessment, error) {
	expectations, err := managedPathExpectations(input.environment, input.locked.Locked)
	if err != nil {
		return OutputInventoryAssessment{}, fmt.Errorf("derive output inventory path expectations: %w", err)
	}
	managedPaths, err := reconcileprojection.BuildManagedPathInventoryDecisions(
		reconcileprojection.ManagedPathInventoryInput{
			Locked:          input.locked.Locked,
			Expectations:    expectations,
			SelectedTargets: input.selectedTargets,
			States:          input.managedInputs.states,
			Evidence:        input.managedEvidence,
			Owner:           input.owner,
			Ownership:       input.ownership,
		},
	)
	if err != nil {
		return OutputInventoryAssessment{}, fmt.Errorf("classify output inventory paths: %w", err)
	}
	aggregates, err := reconcileprojection.BuildAggregateDecisions(
		reconcileprojection.AggregateInput{
			Locked:               input.locked.Locked,
			Expected:             input.aggregateInputs.expected,
			Desired:              input.aggregateInputs.desired,
			States:               input.aggregateInputs.states,
			Evidence:             input.aggregateInputs.evidence,
			ObservationFailures:  input.aggregateInputs.failures,
			PreconditionEvidence: input.aggregateInputs.preconditions,
			SelectedTargets:      input.selectedTargets,
			Owner:                input.owner,
			Ownership:            input.ownership,
			Codecs:               input.codecs,
		},
	)
	if err != nil {
		return OutputInventoryAssessment{}, fmt.Errorf("classify output inventory aggregates: %w", err)
	}

	return OutputInventoryAssessment{
		CurrentState: input.currentState,
		Selection:    input.selection,
		ManagedPaths: managedPaths,
		Aggregates:   aggregates,
	}, nil
}
