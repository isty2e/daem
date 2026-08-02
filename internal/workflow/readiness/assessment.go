package readiness

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	mcpeffective "github.com/isty2e/daem/internal/assurance/observe/mcp/effective"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	commandhook "github.com/isty2e/daem/internal/realization/aggregate/hook"
	"github.com/isty2e/daem/internal/reconcile"
	reconcilehostroute "github.com/isty2e/daem/internal/reconcile/build/hostroute"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
)

type Assessment struct {
	StatePath              string
	CurrentState           durable.Snapshot
	GlobalCarrierClaims    durablecarrier.GlobalCarrierClaims
	ManagedPathEvidence    []observe.ManagedPathEvidence
	AggregateEvidence      []observe.AggregateEvidence
	AggregateFailures      []observe.AggregateObservationFailure
	AggregatePreconditions []observe.AggregatePreconditionEvidence
	MCPEffective           []mcpeffective.Observation
	MCPProviders           []MCPProviderPrerequisite
	Reconciliation         reconcile.Result
	RelationObservations   relationobserve.Batch
	Owner                  stateauthority.Authority
	Ownership              []observe.OwnershipObservation
	SelectedTargets        reconcile.SelectedTargets
}

// Input contains canonical inputs for shared non-mutating apply/status
// assessment.
type Input struct {
	Context                 reconcile.OperationContext
	Paths                   daempaths.Paths
	Resolver                liveobserve.DestinationResolver
	Environment             desired.Environment
	Lockfile                lock.File
	Selection               targetselection.Selection
	SourceEpoch             *lockobserve.SourceEpoch
	PersistenceEpoch        *PersistenceEpoch
	RelationObservations    *relationobserve.Batch
	ManageUnmanagedMatches  bool
	Codecs                  aggregate.CodecCatalog
	HookContributionEncoder commandhook.ContributionEncoder
	MCPContributionEncoder  lockrefine.MCPContributionEncoder
}

// Assess sequences fresh observations and persistence reads, then delegates
// pure decisions to plan without rendering payload bytes or mutating the host.
func Assess(ctx context.Context, input Input) (Assessment, error) {
	selectedTargets, err := reconcile.NewSelectedTargets(input.Selection.Targets())
	if err != nil {
		return Assessment{}, fmt.Errorf("normalize selected targets: %w", err)
	}
	sourceEpoch := input.SourceEpoch
	if sourceEpoch == nil {
		resolved, err := lockobserve.ResolveSourceEpoch(
			ctx,
			input.Paths,
			input.Environment,
			input.Lockfile,
			input.Selection,
		)
		if err != nil {
			return Assessment{}, fmt.Errorf("inspect lockfile freshness: %w", err)
		}
		sourceEpoch = &resolved
	}
	lockObservations, err := sourceEpoch.Observations(ctx)
	if err != nil {
		return Assessment{}, fmt.Errorf("inspect lockfile freshness: %w", err)
	}
	persistenceEpoch := input.PersistenceEpoch
	if persistenceEpoch == nil {
		loaded, err := loadPersistenceEpoch(ctx, input.Paths)
		if err != nil {
			return Assessment{}, err
		}
		persistenceEpoch = &loaded
	}
	currentState, globalCarrierClaims, err := persistenceEpoch.facts()
	if err != nil {
		return Assessment{}, err
	}

	return buildAssessment(
		ctx,
		input.Paths,
		input.Resolver,
		input.Environment,
		input.Lockfile,
		input.Selection,
		selectedTargets,
		lockObservations.ExactSupplies(),
		currentState,
		globalCarrierClaims,
		input.ManageUnmanagedMatches,
		input.Codecs,
		input.HookContributionEncoder,
		input.MCPContributionEncoder,
		input.Context,
		input.RelationObservations,
	)
}

func buildAssessment(
	ctx context.Context,
	paths daempaths.Paths,
	resolver liveobserve.DestinationResolver,
	environment desired.Environment,
	locked lock.File,
	selection targetselection.Selection,
	selectedTargets reconcile.SelectedTargets,
	supplyObservations []observe.ExactSupplyObservation,
	currentState durable.Snapshot,
	globalCarrierClaims durablecarrier.GlobalCarrierClaims,
	manageUnmanagedMatches bool,
	codecs aggregate.CodecCatalog,
	hookEncoder commandhook.ContributionEncoder,
	mcpEncoder lockrefine.MCPContributionEncoder,
	operationContext reconcile.OperationContext,
	providedRelationObservations *relationobserve.Batch,
) (Assessment, error) {
	allCarrierClaims := append(
		currentState.ManagedCarrierClaims(),
		globalCarrierClaims.Claims()...,
	)

	managedInputs, err := buildManagedPathPlanningInputs(locked, currentState, selection)
	if err != nil {
		return Assessment{}, err
	}
	aggregateInputs, err := buildManagedAggregatePlanningInputs(
		ctx,
		resolver,
		environment,
		locked.Locked,
		currentState,
		selection,
		hookEncoder,
		mcpEncoder,
		codecs,
	)
	if err != nil {
		return Assessment{}, err
	}
	managedEvidence, err := liveobserve.ManagedPathEvidence(ctx, resolver, managedInputs.requests)
	if err != nil {
		return Assessment{}, fmt.Errorf("observe managed paths: %w", err)
	}
	owner, ownershipObservations, err := buildOwnershipObservations(
		ctx,
		paths,
		liveDestinationResolver(resolver),
		managedInputs.ownership,
		aggregateInputs.ownershipProjections(),
		currentState,
		selection,
	)
	if err != nil {
		return Assessment{}, err
	}
	mcpEffective, err := observeProviderEffectiveMCP(
		paths,
		resolver,
		locked,
		currentState,
		selection,
		codecs,
	)
	if err != nil {
		return Assessment{}, err
	}
	effectiveConstraints, err := providerEffectiveConstraints(mcpEffective.Current)
	if err != nil {
		return Assessment{}, err
	}
	effectiveRemovalNotices, err := providerEffectiveRemovalNotices(
		mcpEffective.Retiring,
	)
	if err != nil {
		return Assessment{}, err
	}
	relationObservations, err := resolveCarrierObservations(ctx, relationhost.Input{
		Paths:                paths,
		Lockfile:             locked,
		ManagedCarrierClaims: allCarrierClaims,
		Selection:            selection,
	}, providedRelationObservations)
	if err != nil {
		return Assessment{}, fmt.Errorf("inspect carrier relation inventory: %w", err)
	}
	relationActions, err := reconcilehostroute.BuildRelationActions(reconcilehostroute.RelationInput{
		Locked:          locked,
		SelectedTargets: selectedTargets,
		Observations:    relationObservations,
		CurrentOwner:    owner,
		PendingInstalls: currentState.PendingCarrierInstalls(),
		ManagedClaims:   allCarrierClaims,
	})
	if err != nil {
		return Assessment{}, newRelationReconciliationError(err)
	}
	mcpContracts, err := selectedMCPProjectionContracts(locked, selection)
	if err != nil {
		return Assessment{}, err
	}
	providerObservations, err := observeMCPProviders(ctx, paths, locked, mcpContracts)
	if err != nil {
		return Assessment{}, fmt.Errorf("observe MCP provider versions: %w", err)
	}
	providerPrerequisites, err := planMCPProviderPrerequisites(
		locked,
		providerObservations,
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
		environment:             environment,
		locked:                  locked.Locked,
		selectedTargets:         selectedTargets,
		supplyObservations:      supplyObservations,
		managedPathStates:       managedInputs.states,
		managedPathEvidence:     managedEvidence,
		aggregateExpected:       aggregateInputs.expected,
		aggregateDesired:        aggregateInputs.desired,
		aggregateConstraints:    aggregateConstraints,
		aggregateRemovalNotices: effectiveRemovalNotices,
		aggregateStates:         aggregateInputs.states,
		aggregateEvidence:       aggregateInputs.evidence,
		aggregateFailures:       aggregateInputs.failures,
		aggregatePreconditions:  aggregateInputs.preconditions,
		manageUnmanagedMatches:  manageUnmanagedMatches,
		owner:                   owner,
		ownership:               ownershipObservations,
		codecs:                  codecs,
	})
	if err != nil {
		return Assessment{}, err
	}
	carrierAdoptionActions, err := reconcilehostroute.BuildCarrierAdoptionActions(
		reconcilehostroute.CarrierAdoptionInput{
			Locked:          locked,
			SelectedTargets: selectedTargets,
			Observations:    relationObservations,
			CurrentOwner:    owner,
			AllClaims:       allCarrierClaims,
			ManageExisting:  manageUnmanagedMatches,
			StoreAvailable:  true,
		},
	)
	if err != nil {
		return Assessment{}, newRelationReconciliationError(err)
	}
	carrierAbsenceActions, err := reconcilehostroute.BuildCarrierAbsenceActions(
		reconcilehostroute.CarrierAbsenceInput{
			Locked:          locked,
			SelectedTargets: selectedTargets,
			Observations:    relationObservations,
			CurrentOwner:    owner,
			AllClaims:       allCarrierClaims,
			PendingRemovals: currentState.PendingCarrierRemovals(),
			ResolveRoute:    reconcilehostroute.ResolveCurrentCarrierRemovalRoute,
		},
	)
	if err != nil {
		return Assessment{}, fmt.Errorf("plan carrier absences: %w", err)
	}
	relationOrderDecisions, err := observeExtensionOrders(
		paths,
		locked,
		selectedTargets,
		relationActions,
		carrierAbsenceActions,
	)
	if err != nil {
		return Assessment{}, fmt.Errorf("plan extension order: %w", err)
	}
	delegateActions, err := reconcilehostroute.BuildDelegateActions(reconcilehostroute.DelegateInput{
		Locked:              locked,
		SelectedTargets:     selectedTargets,
		Context:             operationContext,
		BlockedDependencies: blockedProjectionDependencies(managedPaths, aggregates),
	})
	if err != nil {
		return Assessment{}, fmt.Errorf("plan delegate actions: %w", err)
	}
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:          operationContext,
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

	return Assessment{
		StatePath:              paths.StatefilePath,
		CurrentState:           currentState,
		GlobalCarrierClaims:    globalCarrierClaims,
		ManagedPathEvidence:    managedEvidence,
		AggregateEvidence:      aggregateInputs.evidence,
		AggregateFailures:      aggregateInputs.failures,
		AggregatePreconditions: aggregateInputs.preconditions,
		MCPEffective:           mcpEffective.Current,
		MCPProviders:           providerPrerequisites,
		Reconciliation:         result,
		RelationObservations:   relationObservations,
		Owner:                  owner,
		Ownership:              ownershipObservations,
		SelectedTargets:        selectedTargets,
	}, nil
}

func blockedProjectionDependencies(
	managedPaths []reconcile.ManagedPathDecision,
	aggregates []reconcile.AggregateDecision,
) []reconcilehostroute.DelegateBlockedDependency {
	blocks := make([]reconcilehostroute.DelegateBlockedDependency, 0)
	seen := make(map[reconcilehostroute.DelegateBlockedDependency]struct{})
	appendBlock := func(subject topology.SubjectID) {
		block := reconcilehostroute.DelegateBlockedDependency{
			Kind:    reconcile.DelegateDependencyProjection,
			Subject: subject,
		}
		if _, exists := seen[block]; exists {
			return
		}
		seen[block] = struct{}{}
		blocks = append(blocks, block)
	}
	for _, decision := range managedPaths {
		if decision.IsBlocked() {
			appendBlock(decision.Subject())
		}
	}
	for _, decision := range aggregates {
		for _, projection := range decision.Projections() {
			if projection.Kind() != reconcile.AggregateBlocked {
				continue
			}
			for _, subject := range projection.Subjects() {
				appendBlock(subject)
			}
		}
	}
	return blocks
}
