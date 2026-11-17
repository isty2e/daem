package readiness

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe"
	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	mcpeffectivehost "github.com/isty2e/daem/internal/assurance/observe/mcp/effective/host"
	mcpprovider "github.com/isty2e/daem/internal/assurance/observe/mcp/provider"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
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
	ownerReference         pathauthority.Reference
	ownershipObservations  []observe.OwnershipObservation
	mcpEffective           mcpeffectivehost.ObservationSet
	relationObservations   relationobserve.Batch
	providerObservations   []mcpprovider.Observation
	extensionOrderFacts    []extensionOrderObservation
	mcpContracts           []lock.LockedSubjectContract
	nativeSources          []lockobserve.NativeSourceObservation
	nativeNarrowings       []reconcile.NativeNarrowingAction
	nativeWholeAdoptions   []reconcileprojection.NativeWholeAdoptionPlan
}

func planAssessmentDecisions(input assessmentPlanInput, carrier carrierPlanningFacts, relationOrderDecisions []reconcile.RelationOrderDecision) (assessmentDecisionPlan, error) {
	effectiveConstraints, err := mcpPublicationConstraints(input.mcpEffective)
	if err != nil {
		return assessmentDecisionPlan{}, err
	}
	effectiveRemovalNotices, err := providerEffectiveRemovalNotices(
		input.mcpEffective.Retiring,
	)
	if err != nil {
		return assessmentDecisionPlan{}, err
	}
	relationActions := carrier.relations
	providerPrerequisites, err := planMCPProviderPrerequisites(
		input.locked,
		input.providerObservations,
		relationActions,
	)
	if err != nil {
		return assessmentDecisionPlan{}, fmt.Errorf("plan MCP provider prerequisites: %w", err)
	}
	providerConstraints, err := providerPrerequisiteConstraints(providerPrerequisites)
	if err != nil {
		return assessmentDecisionPlan{}, err
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
		nativeNarrowings:        input.nativeNarrowings,
		nativeWholeAdoptions:    input.nativeWholeAdoptions,
	})
	if err != nil {
		return assessmentDecisionPlan{}, err
	}
	delegateActions, err := reconcilehostroute.BuildDelegateActions(reconcilehostroute.DelegateInput{
		Locked:              input.locked,
		SelectedTargets:     input.selectedTargets,
		Context:             input.operationContext,
		BlockedDependencies: blockedProjectionDependencies(managedPaths, aggregates),
	})
	if err != nil {
		return assessmentDecisionPlan{}, fmt.Errorf("plan delegate actions: %w", err)
	}
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:          input.operationContext,
		ManagedPaths:     managedPaths,
		NativeNarrowings: input.nativeNarrowings,
		Aggregates:       aggregates,
		Relations:        relationActions,
		RelationOrders:   relationOrderDecisions,
		CarrierAdoptions: carrier.adoptions,
		CarrierAbsences:  carrier.absences,
		Delegates:        delegateActions,
	})
	if err != nil {
		return assessmentDecisionPlan{}, fmt.Errorf("assemble complete reconciliation result: %w", err)
	}
	mcpProjections, err := classifyMCPProjections(
		input.mcpContracts,
		input.currentState,
		input.aggregateInputs.evidence,
		input.aggregateInputs.failures,
		input.aggregateInputs.preconditions,
		input.mcpEffective.Current,
		providerPrerequisites,
		input.mcpEffective.HostPrerequisites,
	)
	if err != nil {
		return assessmentDecisionPlan{}, fmt.Errorf("inspect MCP projection status: %w", err)
	}
	return assessmentDecisionPlan{result: result, providers: providerPrerequisites, projections: mcpProjections}, nil
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
