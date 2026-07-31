package apply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
)

type ExecuteOptions struct {
	ExecuteEvents                  execute.EventSink
	RelationObservations           *relationobserve.Batch
	HostRouteExecutor              subprocess.CommandExecutor
	HostRouteObserver              HostRouteObserver
	CarrierRemovalAdapter          executehostroute.RemovalAdapter
	CarrierRemovalObserver         CarrierRemovalObserver
	CarrierRemovalBaselineObserver CarrierRemovalBaselineObserver
	DelegateExecutor               delegate.Executor
	RelationOrderRiskAuthorizer    RelationOrderRiskAuthorizer
	PlanWasDisclosed               bool
}

func ExecuteWithOptions(ctx context.Context, prepared *PreparedWrite, options ExecuteOptions) (result CommandResult, returnErr error) {
	execution, err := prepared.beginExecution()
	if err != nil {
		return CommandResult{}, err
	}
	defer func() {
		if err := closeCommandPlan(&execution.planned); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()

	planned := execution.planned
	disclose := func(planned commandPlan) CommandResult {
		return cloneCommandResult(planned.result)
	}
	if err := rejectBlockedRelationActions(planned.assessment.Reconciliation); err != nil {
		return disclose(planned), err
	}
	if err := rejectBlockedCarrierAdoptions(planned.assessment.Reconciliation); err != nil {
		return disclose(planned), err
	}
	if err := rejectBlockedCarrierAbsences(planned.assessment.Reconciliation.CarrierAbsences()); err != nil {
		return disclose(planned), err
	}
	if ctx == nil {
		return disclose(planned), fmt.Errorf("apply context is required")
	}
	if err := ctx.Err(); err != nil {
		return disclose(planned), err
	}
	visibleFingerprint, err := applyOperationFingerprint(planned, execution.operationContext)
	if err != nil || !execution.operationEvidence.Equal(visibleFingerprint) {
		return disclose(planned), staleApplyError(options.PlanWasDisclosed, err)
	}
	visibleAuthority, err := buildApplyAuthorityEvidence(ctx, planned)
	if err != nil || !execution.authorityEvidence.authorityFingerprint.Equal(visibleAuthority.authorityFingerprint) {
		return disclose(planned), staleApplyError(options.PlanWasDisclosed, err)
	}
	if err := preflightMCPEnvironmentSources(
		ctx,
		planned.context.RuntimeEnvironment,
		planned.context.Selection,
		execution.request.environmentPresent,
	); err != nil {
		return disclose(planned), err
	}

	store, err := mutation.NewStore(planned.context.Paths.DataDir)
	if err != nil {
		return disclose(planned), err
	}
	effectPaths, err := planned.context.Paths.WithDataDir(store.DataDir())
	if err != nil {
		return disclose(planned), err
	}
	leases, err := store.Acquire(ctx, execution.authorityEvidence.domains...)
	if err != nil {
		return disclose(planned), err
	}
	defer func() {
		if err := leases.Release(); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return disclose(planned), err
	} else if !matches {
		return disclose(planned), staleApplyError(options.PlanWasDisclosed, nil)
	}
	if err := transaction.RequireClearFileSet(ctx, planned.context.Paths.StateDir); err != nil {
		return disclose(planned), err
	}
	if _, err := projectRootFingerprint(planned); err != nil {
		return disclose(planned), staleApplyError(options.PlanWasDisclosed, err)
	}
	revisions, err := mutation.CaptureRevisionSet(ctx, execution.authorityEvidence.revisions...)
	if err != nil {
		return disclose(planned), err
	}
	executionGuard := newApplyExecutionGuard(
		execution.declarationRevisions,
		options.PlanWasDisclosed,
	)
	if err := executionGuard.requireDeclarationsCurrent(
		ctx,
		"prepared apply plan",
	); err != nil {
		return disclose(planned), err
	}

	currentInput := cloneCommandInput(execution.request)
	if options.RelationObservations != nil {
		currentInput.RelationObservations = options.RelationObservations
	}
	current, err := planReadinessAtPaths(ctx, currentInput, execution.operationContext, planned.context.Paths)
	if err != nil {
		return disclose(planned), staleApplyError(options.PlanWasDisclosed, err)
	}
	if err := rejectLocalSourceMutationOverlap(current); err != nil {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, err)
	}
	if err := execute.RejectUnsupportedActions(current.assessment.Reconciliation); err != nil {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, err)
	}
	if err := rejectBlockedRelationActions(current.assessment.Reconciliation); err != nil {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, err)
	}
	if err := rejectBlockedCarrierAdoptions(current.assessment.Reconciliation); err != nil {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, err)
	}
	if err := rejectBlockedCarrierAbsences(current.assessment.Reconciliation.CarrierAbsences()); err != nil {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, err)
	}
	current.projectRoot = planned.projectRoot
	currentOperation, err := applyOperationFingerprint(current, execution.operationContext)
	if err != nil {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, err)
	}
	currentAuthority, err := buildApplyAuthorityEvidence(ctx, current)
	if err != nil {
		return disclose(current), fmt.Errorf("derive current apply mutation authority: %w", err)
	}
	if !execution.operationEvidence.Equal(currentOperation) ||
		!execution.authorityEvidence.authorityFingerprint.Equal(currentAuthority.authorityFingerprint) {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, nil)
	}
	if matches, err := revisions.MatchesCurrent(ctx); err != nil {
		return disclose(current), err
	} else if !matches {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, nil)
	}
	if err := executionGuard.requireDeclarationsCurrent(
		ctx,
		"initial apply revalidation",
	); err != nil {
		return disclose(current), err
	}
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return disclose(current), err
	} else if !matches {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, nil)
	}
	if err := preflightMCPEnvironmentSources(
		ctx,
		current.context.RuntimeEnvironment,
		current.context.Selection,
		execution.request.environmentPresent,
	); err != nil {
		return disclose(current), err
	}

	// The captured revisions describe the pre-execution world. After the first
	// daem effect they are stale by construction; later phases retain lease and
	// project-root checks, while direct file mutations add effect-local CAS.
	revisionBoundaryValidated := false
	validateBeforeEffects := func(ctx context.Context, authority mutation.PhysicalAuthoritySet) error {
		if err := executionGuard.requireDeclarationsCurrent(
			ctx,
			"apply effect validation",
		); err != nil {
			return err
		}
		if !revisionBoundaryValidated {
			matches, err := revisions.MatchesCurrent(ctx)
			if err != nil {
				return err
			}
			if !matches {
				return staleApplyError(options.PlanWasDisclosed, nil)
			}
		}
		matches, err := leases.DomainsMatchCurrent(ctx)
		if err != nil {
			return err
		}
		if !matches {
			return staleApplyError(options.PlanWasDisclosed, nil)
		}
		covered, err := leases.CoversPhysicalAuthority(authority)
		if err != nil {
			return err
		}
		if !covered {
			return staleApplyError(options.PlanWasDisclosed, nil)
		}
		if _, err := projectRootFingerprint(current); err != nil {
			return staleApplyError(options.PlanWasDisclosed, err)
		}
		revisionBoundaryValidated = true
		return nil
	}

	hostRouteObserver := options.HostRouteObserver
	if hostRouteObserver == nil && !current.context.RelationObservationsExplicit {
		hostRouteObserver = passiveHostRouteObserver(
			current.context.Paths,
			current.context.Lockfile,
			current.context.Selection,
		)
	}
	carrierRemovalAdapter := options.CarrierRemovalAdapter
	if carrierRemovalAdapter == nil {
		carrierRemovalAdapter = executehostroute.BuildDelegatedRemovalAttempt
	}
	carrierRemovalObserver := options.CarrierRemovalObserver
	if carrierRemovalObserver == nil {
		carrierRemovalObserver = passiveCarrierRemovalObserver(current.context.Paths)
	}
	carrierRemovalBaselineObserver := options.CarrierRemovalBaselineObserver
	if carrierRemovalBaselineObserver == nil {
		carrierRemovalBaselineObserver = passiveCarrierRemovalBaselineObserver(current.context.Paths)
	}
	executionOptions := runOptions{
		ExecuteEvents:                  options.ExecuteEvents,
		HostRouteExecutor:              options.HostRouteExecutor,
		HostRouteObserver:              hostRouteObserver,
		CarrierRemovalAdapter:          carrierRemovalAdapter,
		CarrierRemovalObserver:         carrierRemovalObserver,
		CarrierRemovalBaselineObserver: carrierRemovalBaselineObserver,
		DelegateExecutor:               options.DelegateExecutor,
		RelationOrderRiskAuthorizer:    options.RelationOrderRiskAuthorizer,
		orderRiskBaseline: newRelationOrderRiskBaseline(
			planned.assessment.Reconciliation.RelationOrders(),
		),
		executionGuard:        executionGuard,
		validateBeforeEffects: validateBeforeEffects,
		projectRoot:           planned.projectRoot,
	}

	providerPhase, err := runMCPProviderPrerequisitePhase(
		ctx,
		&current,
		currentInput,
		execution,
		execution.authorityEvidence,
		effectPaths,
		store,
		leases,
		revisions,
		executionGuard,
		validateBeforeEffects,
		executionOptions,
		options.PlanWasDisclosed,
	)
	if err != nil {
		current.result.HostRouteAttempts = providerPhase.attempts
		return disclose(current), err
	}
	if providerPhase.rebound {
		leases = providerPhase.leases
		revisions = providerPhase.revisions
		revisionBoundaryValidated = false
	}

	runResult, err := runWithOptions(
		ctx,
		effectPaths,
		current.context.RuntimeEnvironment,
		current.context.Lockfile,
		current.context.Selection,
		current.assessment,
		executionOptions,
	)
	if err != nil {
		current.result.DelegateAttempts = runResult.DelegateAttempts
		current.result.RelationOrderResults = runResult.RelationOrderResults
		if runResult.ReconciliationUpdated {
			current.result.Reconciliation = runResult.Reconciliation
		}
		current.result.HostRouteAttempts = append(providerPhase.attempts, runResult.HostRouteAttempts...)
		committedAdoptions, _, adoptionErr := committedCarrierAdoptionClaims(
			current.result.Reconciliation.CarrierAdoptions(),
			runResult.State,
			runResult.GlobalCarrierClaims,
		)
		current.result.CarrierAdoptionResults = committedAdoptions
		if runResult.ActionCount != 0 {
			current.result.ActionCount = runResult.ActionCount
		}
		if runResult.StatePath != "" {
			current.result.StatefilePath = runResult.StatePath
		}
		return disclose(current), errors.Join(err, adoptionErr)
	}

	carrierAdoptionResults, err := finalizedCarrierAdoptionClaims(
		current.result.Reconciliation.CarrierAdoptions(),
		runResult.State,
		runResult.GlobalCarrierClaims,
	)
	if err != nil {
		return disclose(current), fmt.Errorf("resolve finalized carrier adoption claims: %w", err)
	}
	current.result.ActionCount = runResult.ActionCount
	current.result.StatefilePath = runResult.StatePath
	current.result.DelegateAttempts = runResult.DelegateAttempts
	current.result.RelationOrderResults = runResult.RelationOrderResults
	if runResult.ReconciliationUpdated {
		current.result.Reconciliation = runResult.Reconciliation
	}
	current.result.HostRouteAttempts = append(providerPhase.attempts, runResult.HostRouteAttempts...)
	current.result.CarrierAdoptionResults = carrierAdoptionResults
	return disclose(current), nil
}

func staleApplyError(disclosed bool, cause error) error {
	var stale error = mutation.StaleSnapshotError{}
	if disclosed {
		stale = mutation.StalePlanError{}
	}
	return errors.Join(stale, cause)
}

type applyExecutionGuard struct {
	declarationRevisions mutation.RevisionSet
	planWasDisclosed     bool
	valid                bool
}

func newApplyExecutionGuard(
	revisions mutation.RevisionSet,
	planWasDisclosed bool,
) applyExecutionGuard {
	return applyExecutionGuard{
		declarationRevisions: revisions,
		planWasDisclosed:     planWasDisclosed,
		valid:                true,
	}
}

func captureDeclarationRevisions(
	ctx context.Context,
	manifestPath string,
	lockfilePath string,
) (mutation.RevisionSet, error) {
	requests := make([]mutation.RevisionRequest, 0, 4)
	for _, path := range []string{manifestPath, lockfilePath} {
		for _, effect := range []mutation.PathEffect{
			mutation.PathEffectDirectoryEntry,
			mutation.PathEffectReferent,
		} {
			requests = append(requests, mutation.RevisionRequest{
				Path: path, Effect: effect,
			})
		}
	}
	return mutation.CaptureRevisionSet(ctx, requests...)
}

func (guard applyExecutionGuard) requireDeclarationsCurrent(
	ctx context.Context,
	phase string,
) error {
	if !guard.valid {
		return fmt.Errorf("apply execution guard is required")
	}
	matches, err := guard.declarationRevisions.MatchesCurrent(ctx)
	if err != nil {
		return err
	}
	if matches {
		return nil
	}
	return staleApplyError(
		guard.planWasDisclosed,
		errors.New(phase+": manifest or selected lockfile changed"),
	)
}

func (guard applyExecutionGuard) requirePlanCurrent(
	ctx context.Context,
	expected mutation.OperationFingerprint,
	actual mutation.OperationFingerprint,
	phase string,
) error {
	if err := guard.requireDeclarationsCurrent(ctx, phase); err != nil {
		return err
	}
	if expected.Equal(actual) {
		return nil
	}
	return staleApplyError(
		guard.planWasDisclosed,
		errors.New(phase+": remaining execution plan changed"),
	)
}

type remainingExecutionFingerprintFacts struct {
	RelationOrders  []relationOrderFingerprintFacts
	DelegateActions []delegateFingerprintFacts
}

func remainingExecutionFingerprint(
	reconciliation reconcile.Result,
) (mutation.OperationFingerprint, error) {
	canonical, err := json.Marshal(remainingExecutionFingerprintFacts{
		RelationOrders: relationOrderFingerprintRows(
			reconciliation.RelationOrders(),
		),
		DelegateActions: delegateFingerprintRows(
			reconciliation.Delegates(),
		),
	})
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf(
			"fingerprint remaining apply execution: %w",
			err,
		)
	}
	return mutation.NewOperationFingerprint(canonical), nil
}
