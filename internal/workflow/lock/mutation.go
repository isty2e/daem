package lock

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	lockgenerate "github.com/isty2e/daem/internal/workflow/lock/generate"
	"github.com/isty2e/daem/internal/workflow/workspacecleanup"
)

func runLockMutation(ctx context.Context, input LockInput) (result Result, returnErr error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("lock context is required")
	}
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return Result{}, err
	}
	stateDirAuthority, err := recoverygate.CaptureStateDir(ctx, paths.StateDir)
	if err != nil {
		return Result{}, err
	}
	outputPath := outputLockfilePath(input.LockfilePath, paths)
	errorContext := CommandError{
		ManifestPath:     paths.ManifestPath,
		LockfilePath:     outputPath,
		ExplicitLockfile: input.LockfilePath != "",
	}
	metadataTransactionPath, err := fileset.FileSetAuthorityPath(paths.StateDir)
	if err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	globalMetadataPath, err := recoverygate.GlobalMetadataAuthorityPath(paths)
	if err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	var cleanup *workspacecleanup.Plan
	var cleanupSteps []workspacecleanup.Step
	defer func() {
		if len(cleanupSteps) != 0 && returnErr != nil {
			returnErr = afterMetadataRetirement{cause: returnErr, steps: cleanupSteps}
		}
	}()
	if fenceErr := lockRequireStateDirClear(ctx, paths, stateDirAuthority); fenceErr != nil {
		if outputPath != paths.LockfilePath || !stateDirAuthority.PresentAtCapture() || !localPublishedFileSet(fenceErr) {
			errorContext.Err = fenceErr
			return Result{}, errorContext
		}
		cleanup, cleanupSteps, err = finishLockMetadata(ctx, paths, stateDirAuthority)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && !recoverygate.IsBarrierRefusal(err) && !recoverygate.IsCancellation(err) {
				err = fmt.Errorf("%w: could not verify the recovery record or selected files: %w", fileset.ErrInterruptedFileSetTransaction, err)
			}
			errorContext.Err = err
			return Result{}, errorContext
		}
	}
	optimisticLocalPaths, err := commandLocalPaths(ctx, paths)
	if err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	program := compileLockOperationProgram(
		paths.ManifestPath,
		outputPath,
		metadataTransactionPath,
		globalMetadataPath,
		optimisticLocalPaths,
		paths.StateDir,
		stateDirAuthority.PresentAtCapture(),
	)
	domains, err := lowerLockDomainSteps(program.DomainSteps())
	if err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	defer func() {
		if err := leases.Release(); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	} else if !matches {
		errorContext.Err = mutation.StaleSnapshotError{}
		return Result{}, errorContext
	}
	if err := lockRequireStateDirClear(ctx, paths, stateDirAuthority); err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}

	revisionRequests, err := program.RevisionRequests()
	if err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	revisions, err := mutation.CaptureRevisionSet(ctx, revisionRequests...)
	if err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	validateFinalizedSelection := func(ctx context.Context) error {
		if cleanup == nil {
			return nil
		}
		if _, err := stateDirAuthority.ExistingPath(ctx); err != nil {
			return err
		}
		return cleanup.RequireSameDeclarationSelection(revisions)
	}
	if err := validateFinalizedSelection(ctx); err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	if err := recoverygate.RequireUserStateMigrated(ctx, paths); err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	environment, err := loadLockEnvironment(ctx, paths)
	if err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	generation := lockgenerate.Input{
		Paths:                paths,
		Environment:          environment,
		UsePersistentCache:   true,
		MaxParallelSourceOps: input.MaxParallelSourceOps,
		SourceEvents:         input.SourceEvents,
		Events:               lockBuildProgressSink(input.LockEvents),
	}
	currentLocalPaths, err := lockgenerate.ConsumedLocalPaths(generation)
	if err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	if !slices.Equal(optimisticLocalPaths, currentLocalPaths) {
		errorContext.Err = mutation.StaleSnapshotError{}
		return Result{}, errorContext
	}
	if err := lockRequireStateDirClear(ctx, paths, stateDirAuthority); err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	publication, previous, err := observeLockPublication(ctx, revisions, outputPath)
	if err != nil {
		errorContext.Err = fmt.Errorf("read lockfile: %w", err)
		return Result{}, errorContext
	}
	if previous.status == PreviousDamaged {
		if err := checkRebuildInputs(ctx, outputPath, paths.ManifestPath, currentLocalPaths, &revisions); err != nil {
			errorContext.Err = err
			return Result{}, errorContext
		}
	}

	validateAuthority := func(ctx context.Context) error {
		matches, err := revisions.MatchesCurrent(ctx)
		if err != nil {
			return err
		}
		if !matches {
			return mutation.StaleSnapshotError{}
		}
		matches, err = leases.DomainsMatchCurrent(ctx)
		if err != nil {
			return err
		}
		if !matches {
			return mutation.StaleSnapshotError{}
		}
		if err := validateFinalizedSelection(ctx); err != nil {
			return err
		}
		return lockRequireStateDirClear(ctx, paths, stateDirAuthority)
	}
	generation.PreparePersistentCache = func(ctx context.Context) error {
		if err := validateAuthority(ctx); err != nil {
			return err
		}
		if _, err := stateDirAuthority.EnsureOwnedIncarnation(ctx); err != nil {
			return lockStateDirError(err)
		}
		return validateAuthority(ctx)
	}

	built, err := buildCommandResult(ctx, errorContext, generation, previous)
	if err != nil {
		return Result{}, err
	}
	if input.PrepareResult != nil {
		if err := input.PrepareResult(built.Result); err != nil {
			errorContext.Err = err
			return Result{}, errorContext
		}
	}

	if err := validateAuthority(ctx); err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	if err := publication.commit(ctx, built.Content); err != nil {
		return Result{}, fmt.Errorf("write lockfile: %w", err)
	}
	return built.Result, nil
}

func lockRequireStateDirClear(
	ctx context.Context,
	paths daempaths.Paths,
	authority recoverygate.StateDirAuthority,
) error {
	if err := recoverygate.RequireGlobalMetadataClear(ctx, paths); err != nil {
		return err
	}
	return lockStateDirError(authority.RequireClear(ctx))
}

func lockStateDirError(err error) error {
	if errors.Is(err, recoverygate.ErrStateDirAppeared) {
		return errors.Join(mutation.StaleSnapshotError{}, err)
	}
	return err
}

func commandLocalPaths(ctx context.Context, paths daempaths.Paths) ([]string, error) {
	environment, err := loadLockEnvironment(ctx, paths)
	if err != nil {
		return nil, err
	}
	return lockgenerate.ConsumedLocalPaths(lockgenerate.Input{
		Paths:       paths,
		Environment: environment,
	})
}

func compileLockOperationProgram(
	manifestPath string,
	lockfilePath string,
	metadataTransactionPath string,
	globalMetadataPath string,
	localPaths []string,
	stateDirPath string,
	stateDirPresent bool,
) operationplan.LockProgram {
	return operationplan.CompileLock(operationplan.LockInput{
		ManifestPath:            manifestPath,
		LockfilePath:            lockfilePath,
		MetadataTransactionPath: metadataTransactionPath,
		GlobalMetadataPath:      globalMetadataPath,
		LocalPaths:              localPaths,
		StateDirPath:            stateDirPath,
		StateDirPresent:         stateDirPresent,
		DocumentMaximumBytes:    declarationartifact.MaximumBytes,
	})
}

func lowerLockDomainSteps(steps []operationplan.DomainStep) ([]mutation.Domain, error) {
	domains := make([]mutation.Domain, 0, len(steps))
	for _, step := range steps {
		if domain, ok := step.Compiled(); ok {
			domains = append(domains, domain)
			continue
		}
		request, ok := step.Path()
		if !ok {
			return nil, fmt.Errorf("lock operation domain step is not a path request")
		}
		logical, logicalPath := request.Logical()
		if !logicalPath {
			return nil, fmt.Errorf("lock operation path-domain request is not logical")
		}
		domain, err := mutation.NewLogicalPathDomain(logical)
		if err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	return domains, nil
}
