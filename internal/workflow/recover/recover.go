package recover

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/recoverygate"
)

type PlanInput struct {
	ManifestPath string
}

// ExecuteOptions supplies effect-boundary dependencies without changing
// recovery planning, authority acquisition, or revalidation.
type ExecuteOptions struct {
	Filesystem mutationfs.Store
}

// Plan prepares one pointer-owned recovery capability. Its Disclosure method
// returns a defensive presentation snapshot.
func Plan(ctx context.Context, input PlanInput) (*PreparedRecovery, error) {
	planned, err := planRecovery(ctx, input)
	if err != nil {
		return nil, err
	}
	return newPreparedRecovery(planned), nil
}

func planRecovery(ctx context.Context, input PlanInput) (recoveryPreparation, error) {
	return planRecoveryWithFilesystem(ctx, input, storagecommit.Adapter{})
}

func recoverySelectionInterruption(selection journal.RecoverablePlan) error {
	switch selection.AuthorityKind() {
	case journal.RecoveryAuthorityActiveJournal:
		return journal.ErrInterruptedApply
	case journal.RecoveryAuthorityJournalCleanup:
		return journal.ErrIncompleteJournalCleanup
	default:
		return nil
	}
}

func planRecoveryWithFilesystem(
	ctx context.Context,
	input PlanInput,
	filesystem mutationfs.Reader,
) (recoveryPreparation, error) {
	return planRecoveryWithFilesystemAndFence(
		ctx,
		input,
		filesystem,
		func(ctx context.Context, authority transaction.StateDirAuthority) error {
			return authority.RequireClear(ctx)
		},
	)
}

func planRecoveryWithFilesystemAndFence(
	ctx context.Context,
	input PlanInput,
	filesystem mutationfs.Reader,
	observeFileSet func(context.Context, transaction.StateDirAuthority) error,
) (recoveryPreparation, error) {
	if filesystem == nil {
		return recoveryPreparation{}, fmt.Errorf("recovery planning filesystem is required")
	}
	if observeFileSet == nil {
		return recoveryPreparation{}, fmt.Errorf("recovery file-set observation is required")
	}
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return recoveryPreparation{}, err
	}
	stateDir, stateDirErr := transaction.CaptureStateDirAuthority(ctx, paths.StateDir)
	if err := ctx.Err(); err != nil {
		return recoveryPreparation{}, err
	}
	recoverable, journalErr := loadRecoverySelection(ctx, paths, filesystem)
	if err := ctx.Err(); err != nil {
		return recoveryPreparation{}, err
	}
	if journalErr == nil && recoverable.AuthorityKind() == journal.RecoveryAuthorityJournalCleanup {
		return finishRecoveryPreparation(paths, input, recoverable, transaction.StateDirAuthority{}, false, transaction.FileSetFenceClear)
	}

	fenceErr := stateDirErr
	if fenceErr == nil {
		fenceErr = observeFileSet(ctx, stateDir)
	}
	if err := ctx.Err(); err != nil {
		return recoveryPreparation{}, err
	}
	fenceKind := transaction.FileSetFenceKindOf(fenceErr)
	blocksRecovery := fenceKind == transaction.FileSetFenceAccessUnprovable ||
		fenceKind == transaction.FileSetFenceInvalidEvidence
	if journalErr != nil {
		if errors.Is(journalErr, journal.ErrNoRecoverableJournal) && !blocksRecovery {
			return recoveryPreparation{}, journalErr
		}
		return recoveryPreparation{}, recoverygate.Combine(journalErr, fenceErr)
	}
	if recoverable.AuthorityKind() != journal.RecoveryAuthorityActiveJournal {
		return recoveryPreparation{}, fmt.Errorf(
			"recovery authority kind %q is unsupported",
			recoverable.AuthorityKind(),
		)
	}
	journalInterruption := recoverySelectionInterruption(recoverable)
	if blocksRecovery {
		return recoveryPreparation{}, recoverygate.Combine(journalInterruption, fenceErr)
	}
	return finishRecoveryPreparation(paths, input, recoverable, stateDir, true, fenceKind)
}

func finishRecoveryPreparation(
	paths daempaths.Paths,
	input PlanInput,
	plan journal.RecoverablePlan,
	stateDir transaction.StateDirAuthority,
	activeStateDir bool,
	fileSetFence transaction.FileSetFenceKind,
) (recoveryPreparation, error) {
	operationEvidence, err := recoveryOperationFingerprint(paths, plan, stateDir, activeStateDir)
	if err != nil {
		return recoveryPreparation{}, err
	}
	planned := recoveryPreparation{
		plan:              plan,
		paths:             paths,
		input:             input,
		operationEvidence: operationEvidence,
		stateDirAuthority: stateDir,
		activeStateDir:    activeStateDir,
		fileSetFence:      fileSetFence,
	}
	if !plan.Blocked() && !plan.HasErrors() {
		authorityEvidence, err := buildRecoveryAuthorityEvidence(paths, plan, stateDir, activeStateDir)
		if err != nil {
			return recoveryPreparation{}, fmt.Errorf("derive recovery mutation authority: %w", err)
		}
		planned.authorityEvidence = authorityEvidence
	}
	return planned, nil
}

func loadRecoverySelection(
	ctx context.Context,
	paths daempaths.Paths,
	filesystem mutationfs.Reader,
) (journal.RecoverablePlan, error) {
	stateReader := stateReaderForPath(paths.StatefilePath)
	registry, err := ownershipstore.NewRecoveryReader(paths.OwnershipRegistryPath)
	if err != nil {
		return nil, err
	}

	return journal.LoadRecoverablePlanWithOptions(
		ctx,
		journalPaths(paths),
		journal.PlanLoadOptions{
			Filesystem:        filesystem,
			Resolver:          destinationResolver(paths).Resolve,
			OwnershipRegistry: registry,
			Codecs:            aggregatecodec.Catalog(),
			StateCodec:        statefile.Codec{},
			StateReader:       stateReader,
		},
	)
}

// Execute executes one prepared recovery with explicit effect dependencies.
func Execute(
	ctx context.Context,
	prepared *PreparedRecovery,
	options ExecuteOptions,
) (result ExecutionResult, returnErr error) {
	execution, err := prepared.beginExecution()
	if err != nil {
		return ExecutionResult{}, err
	}
	result, err = retainedExecutionResult(execution.plan)
	result = result.withFileSetFence(execution.fileSetFence)
	if err != nil {
		return ExecutionResult{}, err
	}
	defer func() {
		returnErr = result.SemanticError(returnErr)
	}()
	if ctx == nil {
		return result, fmt.Errorf("recovery context is required")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if execution.plan.Blocked() || execution.plan.HasErrors() {
		return result, fmt.Errorf("recovery is blocked")
	}
	if execution.activeStateDir {
		if err := execution.stateDirAuthority.Validate(ctx); err != nil {
			return result, err
		}
	}
	filesystem := options.Filesystem
	if filesystem == nil {
		filesystem = storagecommit.Adapter{}
	}
	visibleOperation, err := recoveryOperationFingerprint(
		execution.paths,
		execution.plan,
		execution.stateDirAuthority,
		execution.activeStateDir,
	)
	if err != nil || !execution.operationEvidence.Equal(visibleOperation) {
		return result, errors.Join(mutation.StaleSnapshotError{}, err)
	}
	visibleAuthority, err := buildRecoveryAuthorityEvidence(
		execution.paths,
		execution.plan,
		execution.stateDirAuthority,
		execution.activeStateDir,
	)
	if err != nil || !execution.authorityEvidence.authorityFingerprint.Equal(visibleAuthority.authorityFingerprint) {
		return result, errors.Join(mutation.StaleSnapshotError{}, err)
	}
	store, err := mutation.NewStore(execution.paths.DataDir)
	if err != nil {
		return result, err
	}
	effectPaths, err := execution.paths.WithDataDir(store.DataDir())
	if err != nil {
		return result, err
	}
	leases, err := store.Acquire(ctx, execution.authorityEvidence.domains...)
	if err != nil {
		return result, err
	}
	defer func() {
		if err := leases.Release(); err != nil {
			result = result.withExecutionFailure()
			returnErr = errors.Join(returnErr, err)
		}
	}()
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return result, err
	} else if !matches {
		return result, mutation.StaleSnapshotError{}
	}
	current, err := planRecovery(ctx, execution.input)
	if err != nil {
		return result, errors.Join(mutation.StaleSnapshotError{}, err)
	}
	if current.plan.Blocked() || current.plan.HasErrors() {
		return result, errors.Join(mutation.StaleSnapshotError{}, fmt.Errorf("recovery is blocked by current evidence"))
	}
	if !execution.operationEvidence.Equal(current.operationEvidence) ||
		!execution.authorityEvidence.authorityFingerprint.Equal(current.authorityEvidence.authorityFingerprint) ||
		!execution.plan.SameExecutionAuthority(current.plan) {
		return result, mutation.StaleSnapshotError{}
	}
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return result, err
	} else if !matches {
		return result, mutation.StaleSnapshotError{}
	}

	validateCurrentAuthority := func(ctx context.Context) error {
		matches, err := leases.DomainsMatchCurrent(ctx)
		if err != nil {
			return err
		}
		if !matches {
			return mutation.StaleSnapshotError{}
		}
		if execution.activeStateDir {
			return execution.stateDirAuthority.Validate(ctx)
		}
		return nil
	}
	validateBeforeActiveEffects := func(
		ctx context.Context,
		authority mutation.PhysicalAuthoritySet,
	) error {
		if err := validateCurrentAuthority(ctx); err != nil {
			return err
		}
		covered, err := leases.CoversPhysicalAuthority(authority)
		if err != nil {
			return err
		}
		if !covered {
			return mutation.StaleSnapshotError{}
		}
		return nil
	}
	validateVisibilityAuthority := func(ctx context.Context) error {
		matches, err := leases.VisibilityAuthorityMatchesCurrent(ctx)
		if err != nil {
			return err
		}
		if !matches {
			return mutation.StaleSnapshotError{}
		}
		return nil
	}
	acceptVisibilityChanges := func(ctx context.Context) error {
		accepted, err := leases.AcceptVisibilityChanges(ctx)
		if err != nil {
			return err
		}
		if !accepted {
			return mutation.StaleSnapshotError{}
		}
		return nil
	}
	switch current.plan.AuthorityKind() {
	case journal.RecoveryAuthorityActiveJournal:
		active, ok := journal.ActiveRecoveryPlan(current.plan)
		if !ok {
			return result, fmt.Errorf("active recovery selection is unavailable")
		}
		activeAuthority, ok := journal.ActiveRecoveryJournalAuthority(current.plan)
		if !ok {
			return result, fmt.Errorf("active recovery journal authority is unavailable")
		}
		err := execute.ExecuteRecoveryPlanWithOptions(
			ctx,
			active,
			executePaths(effectPaths),
			execute.RecoveryOptions{
				ValidateBeforeEffects:       validateBeforeActiveEffects,
				ValidateVisibilityAuthority: validateVisibilityAuthority,
				AcceptVisibilityChanges:     acceptVisibilityChanges,
				ActiveJournalAuthority:      activeAuthority,
				Resolver:                    destinationResolver(effectPaths).Resolve,
				OwnershipRegistryBinder:     ownershipstore.BindRooted,
				Codecs:                      aggregatecodec.Catalog(),
				StateCodec:                  statefile.Codec{},
				StateReader:                 stateReaderForPath(effectPaths.StatefilePath),
				Filesystem:                  filesystem,
			},
		)
		return classifyPostExecutionAuthority(
			ctx,
			execution,
			filesystem,
			result,
			err,
		)
	case journal.RecoveryAuthorityJournalCleanup:
		cleanup, ok := journal.JournalCleanupPlan(current.plan)
		if !ok {
			return result, fmt.Errorf("journal cleanup selection is unavailable")
		}
		err := execute.ExecuteJournalCleanupWithOptions(
			ctx,
			cleanup,
			execute.JournalCleanupPaths{
				RecoveryDir: effectPaths.RecoveryDir,
			},
			execute.JournalCleanupOptions{
				ValidateBeforeEffects: validateCurrentAuthority,
				Filesystem:            filesystem,
			},
		)
		return classifyPostExecutionAuthority(
			ctx,
			execution,
			filesystem,
			result,
			err,
		)
	default:
		return result, fmt.Errorf(
			"recovery authority kind %q is unsupported",
			current.plan.AuthorityKind(),
		)
	}
}

func classifyPostExecutionAuthority(
	ctx context.Context,
	execution recoveryPreparation,
	filesystem mutationfs.Reader,
	prior ExecutionResult,
	executionErr error,
) (ExecutionResult, error) {
	fileSetFence, fileSetErr := postExecutionFileSetFence(ctx, execution)
	if fileSetErr != nil {
		executionErr = errors.Join(executionErr, fileSetErr)
	}
	current, classificationErr := loadRecoverySelection(ctx, execution.paths, filesystem)
	if classificationErr == nil {
		result, err := retainedExecutionResult(current)
		result = result.withFileSetFence(fileSetFence)
		if err != nil {
			return unknownExecutionResult(prior.OperationID()), errors.Join(executionErr, err)
		}
		if result.OperationID() != prior.OperationID() {
			return unknownExecutionResult(prior.OperationID()), errors.Join(
				executionErr,
				fmt.Errorf("recovery authority changed operation identity after execution"),
			)
		}
		if executionErr == nil {
			executionErr = fmt.Errorf(
				"recovery execution returned success while durable authority remains",
			)
		}
		return result.withExecutionFailure(), executionErr
	}
	if errors.Is(classificationErr, journal.ErrNoRecoverableJournal) {
		return retiredExecutionResult(prior.OperationID(), executionErr == nil).withFileSetFence(fileSetFence), executionErr
	}
	return unknownExecutionResult(prior.OperationID()).withFileSetFence(fileSetFence), errors.Join(
		executionErr,
		fmt.Errorf("classify durable recovery authority after execution: %w", classificationErr),
	)
}

func postExecutionFileSetFence(
	ctx context.Context,
	execution recoveryPreparation,
) (transaction.FileSetFenceKind, error) {
	if !execution.activeStateDir {
		return transaction.FileSetFenceClear, nil
	}
	fenceErr := execution.stateDirAuthority.RequireClear(ctx)
	kind := transaction.FileSetFenceKindOf(fenceErr)
	switch kind {
	case transaction.FileSetFencePublishedTransaction,
		transaction.FileSetFenceAbandonedResidue,
		transaction.FileSetFenceCensusLimit:
		return kind, nil
	case transaction.FileSetFenceClear:
		return kind, fenceErr
	default:
		return kind, fenceErr
	}
}

func stateReaderForPath(path string) durable.SnapshotReader {
	return func(ctx context.Context) (durable.Snapshot, error) {
		return statefile.LoadOptional(ctx, path)
	}
}

func journalPaths(paths daempaths.Paths) journal.Paths {
	return journal.Paths{
		RecoveryDir:   paths.RecoveryDir,
		StatefilePath: paths.StatefilePath,
		ManifestRoot:  paths.ManifestRoot,
		DataDir:       paths.DataDir,
	}
}

func executePaths(paths daempaths.Paths) execute.Paths {
	return execute.Paths{
		RecoveryDir:           paths.RecoveryDir,
		StateDir:              paths.StateDir,
		StatefilePath:         paths.StatefilePath,
		ManifestRoot:          paths.ManifestRoot,
		DataDir:               paths.DataDir,
		OwnershipRegistryPath: paths.OwnershipRegistryPath,
	}
}
