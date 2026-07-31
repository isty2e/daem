package apply

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	payloadbuild "github.com/isty2e/daem/internal/effect/payload/build"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/workflow/readiness"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
)

type runResult struct {
	ActionCount           int
	StatePath             string
	State                 durable.Snapshot
	GlobalCarrierClaims   durablecarrier.GlobalCarrierClaims
	HostRouteAttempts     []durableattempt.HostRouteAttempt
	DelegateAttempts      []DelegateAttemptResult
	RelationOrderResults  []RelationOrderExecutionResult
	Reconciliation        reconcile.Result
	ReconciliationUpdated bool
}

type runOptions struct {
	ExecuteEvents                  execute.EventSink
	HostRouteExecutor              subprocess.CommandExecutor
	HostRouteObserver              HostRouteObserver
	CarrierRemovalAdapter          executehostroute.RemovalAdapter
	CarrierRemovalObserver         CarrierRemovalObserver
	CarrierRemovalBaselineObserver CarrierRemovalBaselineObserver
	DelegateExecutor               delegate.Executor
	RelationOrderRiskAuthorizer    RelationOrderRiskAuthorizer
	orderRiskBaseline              relationOrderRiskBaseline
	executionGuard                 applyExecutionGuard
	validateBeforeEffects          func(context.Context, mutation.PhysicalAuthoritySet) error
	projectRoot                    *rootedpath.CapturedRoot
}

// HostRouteObserver returns a post-attempt observation fact for one host route
// command using exact carrier correlation facts persisted before invocation.
// It must not execute the route itself or treat durable state as current host
// evidence.
type HostRouteObserver func(
	context.Context,
	executehostroute.Command,
	[]durablecarrier.PendingCarrierInstall,
	[]durablecarrier.ManagedCarrierClaim,
) assurancehostroute.ObservationFact

func runWithOptions(
	ctx context.Context,
	paths daempaths.Paths,
	environment desired.Environment,
	locked lock.File,
	selection targetselection.Selection,
	assessment readiness.Assessment,
	options runOptions,
) (result runResult, resultErr error) {
	filesystem := storagecommit.Adapter{}
	if err := execute.RejectUnsupportedActions(assessment.Reconciliation); err != nil {
		return runResult{}, err
	}

	managedPathEffects, err := execute.ManagedPathEffects(assessment.Reconciliation.ManagedPaths())
	if err != nil {
		return runResult{}, err
	}
	aggregateEffects, err := execute.AggregateEffects(assessment.Reconciliation.Aggregates())
	if err != nil {
		return runResult{}, err
	}
	carrierOwner := assessment.Owner
	projectCarrierRetirements, globalCarrierRetirements, err := stateOnlyCarrierClaimRetirements(
		assessment.Reconciliation.CarrierAbsences(),
	)
	if err != nil {
		return runResult{}, err
	}
	projectCarrierAdoptions, globalCarrierAdoptions, err := stateOnlyCarrierClaimAdoptions(
		assessment.CurrentState,
		assessment.Reconciliation.CarrierAdoptions(),
	)
	if err != nil {
		return runResult{}, err
	}
	if len(managedPathEffects) == 0 && len(aggregateEffects) == 0 {
		stateResult, err := execute.ApplyWithOptions(ctx, execute.ApplyInput{
			Paths:                       executePaths(paths, assessment.StatePath),
			Resolver:                    destinationResolver(paths).Resolve,
			CurrentState:                assessment.CurrentState,
			GlobalCarrierClaims:         assessment.GlobalCarrierClaims,
			RetiredProjectCarrierClaims: projectCarrierRetirements,
			AdoptedProjectCarrierClaims: projectCarrierAdoptions,
			ManagedPathEffects:          managedPathEffects,
			AggregateEffects:            aggregateEffects,
			ManagedPathEvidence:         assessment.ManagedPathEvidence,
			ConfirmedRelationActions:    assessment.Reconciliation.Relations(),
			Owner:                       assessment.Owner,
			Ownership:                   assessment.Ownership,
			ProjectRoot:                 options.projectRoot,
			Codecs:                      aggregatecodec.Catalog(),
			OwnershipRegistryBinder:     ownershipstore.BindRooted,
			StateCodec:                  statefile.Codec{},
			Filesystem:                  filesystem,
		}, execute.ApplyOptions{
			Events:                options.ExecuteEvents,
			ValidateBeforeEffects: options.validateBeforeEffects,
		})
		if err != nil {
			return runResult{
				ActionCount:         stateResult.ActionCount,
				StatePath:           stateResult.StatePath,
				State:               stateResult.State,
				GlobalCarrierClaims: assessment.GlobalCarrierClaims,
			}, err
		}
		return runAfterCarrierClaimRetirements(
			ctx,
			paths,
			locked,
			selection,
			stateResult,
			carrierOwner,
			assessment.GlobalCarrierClaims,
			globalCarrierRetirements,
			globalCarrierAdoptions,
			assessment.Reconciliation,
			assessment.RelationObservations,
			options,
		)
	}

	payloads, err := payloadbuild.PayloadSet(ctx, payloadbuild.Input{
		Paths:                      paths,
		Environment:                environment,
		Lockfile:                   locked,
		Selection:                  selection,
		ManagedPathPayloadSubjects: managedPathPayloadSubjects(managedPathEffects),
	})
	if err != nil {
		return runResult{}, err
	}
	defer func() {
		if cleanupErr := payloads.Cleanup(); cleanupErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("release host payloads: %w", cleanupErr))
		}
	}()
	applyResult, err := execute.ApplyWithOptions(ctx, execute.ApplyInput{
		Paths:                       executePaths(paths, assessment.StatePath),
		Resolver:                    destinationResolver(paths).Resolve,
		ManagedPathEffects:          managedPathEffects,
		AggregateEffects:            aggregateEffects,
		ManagedPathEvidence:         assessment.ManagedPathEvidence,
		CurrentState:                assessment.CurrentState,
		GlobalCarrierClaims:         assessment.GlobalCarrierClaims,
		RetiredProjectCarrierClaims: projectCarrierRetirements,
		AdoptedProjectCarrierClaims: projectCarrierAdoptions,
		Payloads:                    payloads,
		ConfirmedRelationActions:    assessment.Reconciliation.Relations(),
		Owner:                       assessment.Owner,
		Ownership:                   assessment.Ownership,
		ProjectRoot:                 options.projectRoot,
		Codecs:                      aggregatecodec.Catalog(),
		OwnershipRegistryBinder:     ownershipstore.BindRooted,
		StateCodec:                  statefile.Codec{},
		Filesystem:                  filesystem,
	}, execute.ApplyOptions{
		Events:                options.ExecuteEvents,
		ValidateBeforeEffects: options.validateBeforeEffects,
	})
	if err != nil {
		return runResult{
			ActionCount:         applyResult.ActionCount,
			StatePath:           applyResult.StatePath,
			State:               applyResult.State,
			GlobalCarrierClaims: assessment.GlobalCarrierClaims,
		}, err
	}
	return runAfterCarrierClaimRetirements(
		ctx,
		paths,
		locked,
		selection,
		applyResult,
		carrierOwner,
		assessment.GlobalCarrierClaims,
		globalCarrierRetirements,
		globalCarrierAdoptions,
		assessment.Reconciliation,
		assessment.RelationObservations,
		options,
	)
}

func managedPathPayloadSubjects(effects []execute.ManagedPathEffect) []topology.SubjectID {
	subjects := make([]topology.SubjectID, 0, len(effects))
	for _, effect := range effects {
		if effect.RequiresPayload() {
			subjects = append(subjects, effect.Subject())
		}
	}
	return subjects
}

func runHostRoutesOrderDelegatesAndPersistAttemptRecords(
	ctx context.Context,
	paths daempaths.Paths,
	locked lock.File,
	selection targetselection.Selection,
	statePath string,
	current durable.Snapshot,
	carrierOwner stateauthority.Authority,
	globalCarrierClaims durablecarrier.GlobalCarrierClaims,
	actionCount int,
	reconciliation reconcile.Result,
	options runOptions,
) (runResult, error) {
	nextState, nextGlobalClaims, hostRouteAttempts, hostRouteErr := runHostRoutesAndPersistAttemptRecords(
		ctx,
		paths,
		locked,
		statePath,
		current,
		carrierOwner,
		globalCarrierClaims,
		reconciliation.Relations(),
		options,
	)
	result := runResult{
		ActionCount:         actionCount,
		StatePath:           statePath,
		State:               nextState,
		GlobalCarrierClaims: nextGlobalClaims,
		HostRouteAttempts:   hostRouteAttempts,
	}
	if hostRouteErr != nil {
		return result, hostRouteErr
	}

	orderResult, orderErr := runRelationOrderConvergence(
		ctx,
		paths,
		locked,
		reconciliation,
		options,
	)
	result.RelationOrderResults = orderResult.results
	result.Reconciliation = orderResult.reconciliation
	result.ReconciliationUpdated = orderResult.updated
	result.ActionCount += orderResult.actionCount
	if orderErr != nil {
		return result, orderErr
	}

	delegateResult, delegateErr := runDelegatesAndPersistAttemptRecords(
		ctx,
		paths,
		locked,
		selection,
		statePath,
		nextState,
		result.ActionCount,
		orderResult.reconciliation,
		orderResult.planFingerprint,
		options,
	)
	delegateResult.HostRouteAttempts = hostRouteAttempts
	delegateResult.GlobalCarrierClaims = nextGlobalClaims
	delegateResult.RelationOrderResults = orderResult.results
	delegateResult.Reconciliation = orderResult.reconciliation
	delegateResult.ReconciliationUpdated = orderResult.updated
	return delegateResult, delegateErr
}
