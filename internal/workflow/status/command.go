package status

import (
	"context"
	"fmt"
	"os"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/statefile"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/diagnose"
	"github.com/isty2e/daem/internal/effect/journal"
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
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

type CommandInput struct {
	ManifestPath         string
	LockfilePath         string
	TargetValues         []string
	RelationObservations *relationobserve.Batch
}

type CommandResult struct {
	ManifestPath      string
	LockfilePath      string
	LockfileExplicit  bool
	StatefilePath     string
	LockfileMissing   bool
	Reconciliation    reconcile.Result
	Diagnostics       []findings.Diagnostic
	LockOnly          []readiness.UnsupportedProjection
	Inventory         Inventory
	MCPProjections    []mcpobserve.LockedProjectionObservation
	HostRouteAttempts []durableattempt.HostRouteAttempt
}

// HasBlockedRelationActions reports whether relation planning blocks a clean status.
func (result CommandResult) HasBlockedRelationActions() bool {
	return result.Reconciliation.HasBlockedRelations()
}

func Run(ctx context.Context, input CommandInput) (CommandResult, error) {
	loaded, result, err := loadCommandInputs(ctx, input)
	if err != nil {
		return result, err
	}

	assessment, err := readiness.Assess(ctx, readiness.Input{
		Context:                 reconcile.ContextInspect,
		Paths:                   loaded.Paths,
		Resolver:                liveobserve.DestinationResolver(destinationResolver(loaded.Paths).Resolve),
		Environment:             loaded.RuntimeEnvironment,
		Lockfile:                loaded.Lockfile,
		Selection:               loaded.Selection,
		SourceEpoch:             &loaded.SourceEpoch,
		PersistenceEpoch:        &loaded.PersistenceEpoch,
		RelationObservations:    input.RelationObservations,
		Codecs:                  aggregatecodec.Catalog(),
		HookContributionEncoder: hookcodec.CanonicalHookContribution,
		MCPContributionEncoder:  mcpcodec.CanonicalMCPBindingContribution,
	})
	if err != nil {
		if readiness.IsRelationReconciliationError(err) {
			return result, fmt.Errorf("inspect carrier relation status: %w", err)
		}
		return result, err
	}
	mcpObservations, err := readiness.ClassifyMCPProjections(
		loaded.Lockfile,
		loaded.Selection,
		assessment.CurrentState,
		assessment.AggregateEvidence,
		assessment.AggregateFailures,
		assessment.AggregatePreconditions,
		assessment.MCPEffective,
		assessment.MCPProviders,
	)
	if err != nil {
		return result, fmt.Errorf("inspect MCP projection status: %w", err)
	}
	result.Reconciliation = assessment.Reconciliation
	result.Diagnostics = append(
		loaded.Diagnostics,
		diagnose.RetainedSkillDiscoveryDiagnostics(
			ctx,
			loaded.Paths,
			loaded.RuntimeEnvironment.Skills(),
			loaded.Selection,
			assessment.Reconciliation,
		)...,
	)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.LockOnly = readiness.SelectedUnsupportedProjections(
		loaded.RuntimeEnvironment,
		loaded.Selection,
	)
	result.Inventory = BuildInventory(assessment.CurrentState, assessment.Reconciliation, loaded.Selection)
	result.MCPProjections = mcpObservations
	result.HostRouteAttempts = assessment.CurrentState.HostRouteAttempts()

	return result, nil
}

type commandInputs struct {
	Paths              daempaths.Paths
	RuntimeEnvironment desired.Environment
	Lockfile           lock.File
	Selection          targetselection.Selection
	SourceEpoch        lockobserve.SourceEpoch
	PersistenceEpoch   readiness.PersistenceEpoch
	Diagnostics        []findings.Diagnostic
}

func loadCommandInputs(ctx context.Context, input CommandInput) (commandInputs, CommandResult, error) {
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return commandInputs{}, CommandResult{}, err
	}
	result := CommandResult{
		ManifestPath:     paths.ManifestPath,
		LockfilePath:     selectedLockfilePath(paths, input.LockfilePath),
		LockfileExplicit: input.LockfilePath != "",
		StatefilePath:    paths.StatefilePath,
	}

	if err := journal.RequireNoInterruptedApply(ctx, paths.RecoveryDir); err != nil {
		return commandInputs{}, result, err
	}
	if err := transaction.RequireClearFileSet(ctx, paths.StateDir); err != nil {
		return commandInputs{}, result, err
	}

	environment, err := declarationmanifest.LoadSelected(paths)
	if err != nil {
		return commandInputs{}, result, fmt.Errorf("invalid manifest: %w", err)
	}

	locked, lockfileMissing, err := loadStatusLockfile(result.LockfilePath)
	if err != nil {
		return commandInputs{}, result, fmt.Errorf("read lockfile: %w", err)
	}
	result.LockfileMissing = lockfileMissing
	if !lockfileMissing {
		if err := lockrefine.ValidateCurrentExtensionOrder(
			environment.Extensions(),
			locked,
			aggregatecodec.ExtensionOrderIdentityResolver(paths),
		); err != nil {
			return commandInputs{}, result, fmt.Errorf("read lockfile: %w", err)
		}
	}

	// Target selection and readiness share this command-local persistence epoch.
	currentState, err := statefile.LoadOptional(ctx, paths.StatefilePath)
	if err != nil {
		return commandInputs{}, result, err
	}
	carrierStore, err := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
	if err != nil {
		return commandInputs{}, result, err
	}
	globalCarrierClaims, err := carrierStore.LoadForSelectedAuthority(
		ctx,
		paths.StatefilePath,
		paths.ManifestPath,
	)
	if err != nil {
		return commandInputs{}, result, err
	}
	persistenceEpoch := readiness.NewPersistenceEpoch(currentState, globalCarrierClaims)

	availableTargets, err := readiness.FromManifestLockAndState(
		environment,
		locked,
		currentState,
		globalCarrierClaims,
	)
	if err != nil {
		return commandInputs{}, result, fmt.Errorf("%w: %w", targetselection.ErrInvalid, err)
	}
	selection, err := targetselection.ForAvailableTargets(availableTargets, input.TargetValues)
	if err != nil {
		return commandInputs{}, result, fmt.Errorf("%w: %w", targetselection.ErrInvalid, err)
	}

	runtimeEnvironment := environment
	if !lockfileMissing {
		generatedSkills, err := locked.Locked.SkillSetChildren(environment.Skills(), environment.SkillSets())
		if err != nil {
			return commandInputs{}, result, fmt.Errorf("expand skill groups from lockfile: %w", err)
		}
		runtimeEnvironment, err = environment.WithGeneratedSkills(generatedSkills)
		if err != nil {
			return commandInputs{}, result, fmt.Errorf("build runtime desired environment: %w", err)
		}
	}
	sourceEpoch, err := lockobserve.ResolveSourceEpoch(
		ctx,
		paths,
		runtimeEnvironment,
		locked,
		selection,
	)
	if err != nil {
		return commandInputs{}, result, fmt.Errorf("resolve lock observation sources: %w", err)
	}
	skillDiagnostics := diagnose.SkillRepairDiagnosticsFromSourceEpoch(
		ctx,
		paths,
		runtimeEnvironment.Skills(),
		selection,
		sourceEpoch,
	)
	if err := ctx.Err(); err != nil {
		return commandInputs{}, result, err
	}

	return commandInputs{
		Paths:              paths,
		RuntimeEnvironment: runtimeEnvironment,
		Lockfile:           locked,
		Selection:          selection,
		SourceEpoch:        sourceEpoch,
		PersistenceEpoch:   persistenceEpoch,
		Diagnostics: append(
			diagnose.HookCommandDiagnostics(environment.Hooks(), selection),
			skillDiagnostics...,
		),
	}, result, nil
}

func selectedLockfilePath(paths daempaths.Paths, lockfilePath string) string {
	if lockfilePath != "" {
		return lockfilePath
	}

	return paths.LockfilePath
}

func loadStatusLockfile(path string) (lock.File, bool, error) {
	file, err := lockfile.Load(path)
	if err == nil {
		return file, false, nil
	}
	if os.IsNotExist(err) {
		return lock.File{}, true, nil
	}

	return lock.File{}, false, err
}
