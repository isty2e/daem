package apply

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/statefile"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/diagnose"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/findings"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

var (
	ErrReadLockfile         = errors.New("read lockfile")
	ErrRelationActionBlock  = errors.New("relation action blocked")
	ErrRelationOrderBlock   = errors.New("relation order blocked")
	ErrCarrierAdoptionBlock = errors.New("carrier adoption blocked")
	ErrCarrierAbsenceBlock  = errors.New("carrier absence blocked")
)

type CommandInput struct {
	ManifestPath           string
	LockfilePath           string
	TargetValues           []string
	RelationObservations   *relationobserve.Batch
	ManageUnmanagedMatches bool
	environmentPresent     environmentSourcePresence
}

type CommandResult struct {
	ManifestPath           string
	LockfilePath           string
	LockfileExplicit       bool
	StatefilePath          string
	Reconciliation         reconcile.Result
	ReconciliationReady    bool
	DelegateAttempts       []DelegateAttemptResult
	RelationOrderResults   []RelationOrderExecutionResult
	HostRouteAttempts      []durableattempt.HostRouteAttempt
	CarrierAdoptionResults []durablecarrier.ManagedCarrierClaim
	Diagnostics            []findings.Diagnostic
	LockOnly               []readiness.UnsupportedProjection
	MCPProjections         []mcpobserve.LockedProjectionObservation
	ActionCount            int
}

// HasBlockedRelationActions reports whether relation planning blocks ordinary apply.
func (result CommandResult) HasBlockedRelationActions() bool {
	return result.Reconciliation.HasBlockedRelations()
}

type commandContext struct {
	Paths                        daempaths.Paths
	RuntimeEnvironment           desired.Environment
	Lockfile                     lock.File
	Selection                    targetselection.Selection
	SourceEpoch                  lockobserve.SourceEpoch
	PersistenceEpoch             readiness.PersistenceEpoch
	RelationObservationsExplicit bool
	ManageUnmanagedMatches       bool
}

type commandPlan struct {
	result      CommandResult
	context     commandContext
	assessment  readiness.Assessment
	projectRoot *rootedpath.CapturedRoot
}

// DryRunPlan is a capability-free dry-run result plus the immutable inputs
// needed to render optional diffs without reloading command state.
type DryRunPlan struct {
	CommandResult
	planned commandPlan
}

func PlanDryRun(ctx context.Context, input CommandInput) (DryRunPlan, error) {
	planned, err := planReadiness(ctx, input, reconcile.ContextDryRun)
	if err != nil {
		return newDryRunPlan(planned), err
	}
	if err := rejectLocalSourceMutationOverlap(planned); err != nil {
		return newDryRunPlan(planned), err
	}
	if err := execute.RejectUnsupportedExecutableActions(planned.assessment.Reconciliation); err != nil {
		return newDryRunPlan(planned), err
	}

	return newDryRunPlan(planned), nil
}

// PlanWrite builds an executable apply plan and retains its selected physical
// project-root witness. The caller must call PreparedWrite.Close unless it
// passes the result to Execute or ExecuteWithOptions, which consume and close it.
func PlanWrite(ctx context.Context, input CommandInput) (prepared *PreparedWrite, returnErr error) {
	operationContext := reconcile.ContextApply
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return unavailablePreparedWrite(CommandResult{}), err
	}
	root, rootCaptureErr := captureProjectRootAuthorityBeforeLoad(paths)
	defer func() {
		if root != nil {
			if err := root.Close(); err != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("close pre-load apply project-root witness: %w", err))
			}
		}
	}()
	planned, err := planReadinessAtPaths(ctx, input, operationContext, paths)
	if err != nil {
		return unavailablePreparedWrite(planned.result), err
	}
	if err := execute.RejectUnsupportedActions(planned.assessment.Reconciliation); err != nil {
		return unavailablePreparedWrite(planned.result), err
	}
	if err := rejectBlockedRelationActions(planned.assessment.Reconciliation); err != nil {
		return unavailablePreparedWrite(planned.result), err
	}
	if err := rejectBlockedRelationOrders(planned.assessment.Reconciliation); err != nil {
		return unavailablePreparedWrite(planned.result), err
	}
	if err := rejectBlockedCarrierAdoptions(planned.assessment.Reconciliation); err != nil {
		return unavailablePreparedWrite(planned.result), err
	}
	if err := rejectBlockedCarrierAbsences(planned.assessment.Reconciliation.CarrierAbsences()); err != nil {
		return unavailablePreparedWrite(planned.result), err
	}
	if err := preflightMCPEnvironmentSources(
		ctx,
		planned.context.RuntimeEnvironment,
		planned.context.Selection,
		input.environmentPresent,
	); err != nil {
		return unavailablePreparedWrite(planned.result), err
	}
	if err := retainProjectRootAuthority(&planned, root, rootCaptureErr); err != nil {
		return unavailablePreparedWrite(planned.result), err
	}
	if planned.projectRoot != nil {
		root = nil
	}
	operationEvidence, err := applyOperationFingerprint(planned, operationContext)
	if err != nil {
		closeErr := closeCommandPlan(&planned)
		return unavailablePreparedWrite(planned.result), errors.Join(err, closeErr)
	}
	authorityEvidence, err := buildApplyAuthorityEvidence(ctx, planned)
	if err != nil {
		closeErr := closeCommandPlan(&planned)
		return unavailablePreparedWrite(planned.result), errors.Join(
			fmt.Errorf("derive apply mutation authority: %w", err),
			closeErr,
		)
	}
	if err := rejectLocalSourceMutationOverlap(planned); err != nil {
		closeErr := closeCommandPlan(&planned)
		return unavailablePreparedWrite(planned.result), errors.Join(err, closeErr)
	}

	return newPreparedWrite(
		planned,
		cloneCommandInput(input),
		operationContext,
		operationEvidence,
		authorityEvidence,
	), nil
}

func BuildDiffs(ctx context.Context, result DryRunPlan) ([]DryRunDiff, error) {
	return BuildDryRunDiffs(
		ctx,
		result.planned.context.Paths,
		result.planned.context.RuntimeEnvironment,
		result.planned.context.Lockfile,
		result.planned.context.Selection,
		result.planned.result.Reconciliation,
		nil,
	)
}

func planReadiness(ctx context.Context, input CommandInput, operationContext reconcile.OperationContext) (commandPlan, error) {
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return commandPlan{}, err
	}
	return planReadinessAtPaths(ctx, input, operationContext, paths)
}

func planReadinessAtPaths(
	ctx context.Context,
	input CommandInput,
	operationContext reconcile.OperationContext,
	paths daempaths.Paths,
) (commandPlan, error) {
	loaded, result, err := loadCommandInputsAtPaths(ctx, input, paths)
	planned := commandPlan{result: result, context: loaded}
	if err != nil {
		return planned, err
	}

	planning, err := readiness.Assess(ctx, readiness.Input{
		Context:                 operationContext,
		Paths:                   loaded.Paths,
		Resolver:                liveobserve.DestinationResolver(destinationResolver(loaded.Paths).Resolve),
		Environment:             loaded.RuntimeEnvironment,
		Lockfile:                loaded.Lockfile,
		Selection:               loaded.Selection,
		SourceEpoch:             &loaded.SourceEpoch,
		PersistenceEpoch:        &loaded.PersistenceEpoch,
		RelationObservations:    input.RelationObservations,
		ManageUnmanagedMatches:  input.ManageUnmanagedMatches,
		Codecs:                  aggregatecodec.Catalog(),
		HookContributionEncoder: hookcodec.CanonicalHookContribution,
		MCPContributionEncoder:  mcpcodec.CanonicalMCPBindingContribution,
	})
	if err != nil {
		return planned, err
	}

	result.Reconciliation = planning.Reconciliation
	result.ReconciliationReady = true
	result.Diagnostics = append(
		result.Diagnostics,
		diagnose.RetainedSkillDiscoveryDiagnostics(
			ctx,
			loaded.Paths,
			loaded.RuntimeEnvironment.Skills(),
			loaded.Selection,
			planning.Reconciliation,
		)...,
	)
	if err := ctx.Err(); err != nil {
		planned.result = result
		planned.assessment = planning
		return planned, err
	}
	mcpProjections, err := readiness.ClassifyMCPProjections(
		loaded.Lockfile,
		loaded.Selection,
		planning.CurrentState,
		planning.AggregateEvidence,
		planning.AggregateFailures,
		planning.AggregatePreconditions,
		planning.MCPEffective,
		planning.MCPProviders,
	)
	if err != nil {
		planned.result = result
		planned.assessment = planning
		return planned, fmt.Errorf("inspect MCP projection status: %w", err)
	}
	result.MCPProjections = mcpProjections

	planned.result = result
	planned.assessment = planning
	return planned, nil
}

func rejectBlockedRelationActions(result reconcile.Result) error {
	action, blocked := result.FirstBlockedRelation()
	if !blocked {
		return nil
	}
	return fmt.Errorf(
		"%w: subject=%s/%s/%s kind=%s reason=%s",
		ErrRelationActionBlock,
		action.Subject().Kind(),
		action.Subject().Namespace(),
		action.Subject().Key(),
		action.Kind(),
		action.Reason(),
	)
}

func rejectBlockedRelationOrders(result reconcile.Result) error {
	decision, blocked := result.FirstBlockedRelationOrder()
	if !blocked {
		return nil
	}
	return fmt.Errorf(
		"%w: target=%s scope=%s class=%s sequence=%s reason=%s detail=%s",
		ErrRelationOrderBlock,
		decision.Target(),
		decision.Scope(),
		decision.ClassID(),
		decision.SequenceID(),
		decision.Reason(),
		decision.Detail(),
	)
}

func rejectBlockedCarrierAdoptions(result reconcile.Result) error {
	action, blocked := result.FirstBlockedCarrierAdoption()
	if !blocked {
		return nil
	}
	return fmt.Errorf(
		"%w: subject=%s/%s/%s target=%s scope=%s result=%s",
		ErrCarrierAdoptionBlock,
		action.Subject().Kind(),
		action.Subject().Namespace(),
		action.Subject().Key(),
		action.Target(),
		action.Scope(),
		action.Result(),
	)
}

func rejectBlockedCarrierAbsences(actions []carrierabsence.Action) error {
	for _, action := range actions {
		if !action.BlocksOrdinaryApply() {
			continue
		}
		return fmt.Errorf(
			"%w: subject=%s/%s/%s target=%s scope=%s decision=%s",
			ErrCarrierAbsenceBlock,
			action.Subject().Kind(),
			action.Subject().Namespace(),
			action.Subject().Key(),
			action.Target(),
			action.Scope(),
			action.Decision(),
		)
	}
	return nil
}

func loadCommandInputsAtPaths(
	ctx context.Context,
	input CommandInput,
	paths daempaths.Paths,
) (commandContext, CommandResult, error) {
	lockfilePath, err := selectedLockfilePath(paths, input.LockfilePath)
	if err != nil {
		return commandContext{}, CommandResult{}, err
	}
	result := CommandResult{
		ManifestPath:     paths.ManifestPath,
		LockfilePath:     lockfilePath,
		LockfileExplicit: input.LockfilePath != "",
		StatefilePath:    paths.StatefilePath,
	}

	if err := journal.EnsureNoActive(paths.RecoveryDir); err != nil {
		return commandContext{}, result, err
	}
	if err := transaction.RequireClearFileSet(ctx, paths.StateDir); err != nil {
		return commandContext{}, result, err
	}

	environment, err := declarationmanifest.LoadSelected(paths)
	if err != nil {
		return commandContext{}, result, fmt.Errorf("invalid manifest: %w", err)
	}

	locked, lockfileErr := lockfile.Load(result.LockfilePath)
	lockfileMissing := false
	if lockfileErr != nil {
		if !os.IsNotExist(lockfileErr) {
			return commandContext{}, result, fmt.Errorf("%w: %w", ErrReadLockfile, lockfileErr)
		}
		lockfileMissing = true
	} else if err := lockrefine.ValidateCurrentExtensionOrder(
		environment.Extensions(),
		locked,
		aggregatecodec.ExtensionOrderIdentityResolver(paths),
	); err != nil {
		return commandContext{}, result, fmt.Errorf("%w: %w", ErrReadLockfile, err)
	}

	// Each planning pass owns one persistence epoch. Execute performs a new pass
	// after acquiring mutation leases rather than reusing the disclosed plan.
	currentState, err := statefile.LoadOptional(ctx, paths.StatefilePath)
	if err != nil {
		return commandContext{}, result, err
	}
	carrierStore, err := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
	if err != nil {
		return commandContext{}, result, err
	}
	globalCarrierClaims, err := carrierStore.Load(ctx)
	if err != nil {
		return commandContext{}, result, err
	}
	persistenceEpoch := readiness.NewPersistenceEpoch(currentState, globalCarrierClaims)

	availableTargets, err := readiness.FromManifestLockAndState(
		environment,
		locked,
		currentState,
		globalCarrierClaims,
	)
	if err != nil {
		return commandContext{}, result, fmt.Errorf("%w: %w", targetselection.ErrInvalid, err)
	}
	selection, err := targetselection.ForAvailableTargets(availableTargets, input.TargetValues)
	if err != nil {
		return commandContext{}, result, fmt.Errorf("%w: %w", targetselection.ErrInvalid, err)
	}

	if lockfileMissing {
		return commandContext{}, result, fmt.Errorf("%w: %w", ErrReadLockfile, lockfileErr)
	}

	generatedSkills, err := locked.Locked.SkillSetChildren(environment.Skills(), environment.SkillSets())
	if err != nil {
		return commandContext{}, result, fmt.Errorf("expand skill groups from lockfile: %w", err)
	}
	runtimeEnvironment, err := environment.WithGeneratedSkills(generatedSkills)
	if err != nil {
		return commandContext{}, result, fmt.Errorf("build runtime desired environment: %w", err)
	}

	sourceEpoch, err := lockobserve.ResolveSourceEpoch(
		ctx,
		paths,
		runtimeEnvironment,
		locked,
		selection,
	)
	if err != nil {
		return commandContext{}, result, fmt.Errorf("resolve lock observation sources: %w", err)
	}
	context := commandContext{
		Paths:                        paths,
		RuntimeEnvironment:           runtimeEnvironment,
		Lockfile:                     locked,
		Selection:                    selection,
		SourceEpoch:                  sourceEpoch,
		PersistenceEpoch:             persistenceEpoch,
		RelationObservationsExplicit: input.RelationObservations != nil,
		ManageUnmanagedMatches:       input.ManageUnmanagedMatches,
	}
	skillDiagnostics := diagnose.SkillRepairDiagnosticsFromSourceEpoch(
		ctx,
		paths,
		runtimeEnvironment.Skills(),
		selection,
		sourceEpoch,
	)
	if err := ctx.Err(); err != nil {
		return commandContext{}, result, err
	}
	result.Diagnostics = append(
		diagnose.HookCommandDiagnostics(environment.Hooks(), selection),
		skillDiagnostics...,
	)
	result.LockOnly = readiness.SelectedUnsupportedProjections(runtimeEnvironment, selection)

	return context, result, nil
}

func selectedLockfilePath(paths daempaths.Paths, lockfilePath string) (string, error) {
	if lockfilePath != "" {
		absolute, err := filepath.Abs(lockfilePath)
		if err != nil {
			return "", fmt.Errorf("resolve lockfile path %q: %w", lockfilePath, err)
		}
		return filepath.Clean(absolute), nil
	}

	return paths.LockfilePath, nil
}

func cloneCommandInput(input CommandInput) CommandInput {
	cloned := input
	cloned.TargetValues = append([]string(nil), input.TargetValues...)
	return cloned
}
