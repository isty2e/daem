package lock

import (
	"context"
	"errors"
	"fmt"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	lockgenerate "github.com/isty2e/daem/internal/workflow/lock/generate"
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
	if err := lockRequireStateDirClear(ctx, stateDirAuthority); err != nil {
		errorContext.Err = err
		return Result{}, errorContext
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
	if err := lockRequireStateDirClear(ctx, stateDirAuthority); err != nil {
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
	currentLocalPaths, err := commandLocalPaths(ctx, paths)
	if err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	if !equalPathLists(optimisticLocalPaths, currentLocalPaths) {
		errorContext.Err = mutation.StaleSnapshotError{}
		return Result{}, errorContext
	}
	if err := lockRequireStateDirClear(ctx, stateDirAuthority); err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	validateCacheAuthority := func(ctx context.Context) error {
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
		return lockRequireStateDirClear(ctx, stateDirAuthority)
	}
	preparePersistentCache := func(ctx context.Context) error {
		if err := validateCacheAuthority(ctx); err != nil {
			return err
		}
		if _, err := stateDirAuthority.EnsureOwnedIncarnation(ctx); err != nil {
			return lockStateDirError(err)
		}
		return validateCacheAuthority(ctx)
	}

	built, err := buildCommandResult(
		ctx,
		input.ManifestPath,
		input.LockfilePath,
		true,
		preparePersistentCache,
		true,
		commandMaxParallelSourceOps(input.MaxParallelSourceOps),
		input.SourceEvents,
		input.LockEvents,
	)
	if err != nil {
		return Result{}, err
	}
	matches, err := revisions.MatchesCurrent(ctx)
	if err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	if !matches {
		errorContext.Err = mutation.StaleSnapshotError{}
		return Result{}, errorContext
	}
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	} else if !matches {
		errorContext.Err = mutation.StaleSnapshotError{}
		return Result{}, errorContext
	}
	if err := ctx.Err(); err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	if err := lockRequireStateDirClear(ctx, stateDirAuthority); err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	if err := commitLockfile(ctx, built.Result.LockfilePath, built.Content, 0o600); err != nil {
		return Result{}, fmt.Errorf("write lockfile: %w", err)
	}
	return built.Result, nil
}

func lockRequireStateDirClear(
	ctx context.Context,
	authority recoverygate.StateDirAuthority,
) error {
	return lockStateDirError(authority.RequireClear(ctx))
}

func lockStateDirError(err error) error {
	if errors.Is(err, recoverygate.ErrStateDirAppeared) {
		return errors.Join(mutation.StaleSnapshotError{}, err)
	}
	return err
}

func commandLocalPaths(ctx context.Context, paths daempaths.Paths) ([]string, error) {
	environment, err := declarationmanifest.LoadSelected(ctx, paths)
	if err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
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
	localPaths []string,
	stateDirPath string,
	stateDirPresent bool,
) operationplan.LockProgram {
	return operationplan.CompileLock(operationplan.LockInput{
		ManifestPath:            manifestPath,
		LockfilePath:            lockfilePath,
		MetadataTransactionPath: metadataTransactionPath,
		LocalPaths:              localPaths,
		StateDirPath:            stateDirPath,
		StateDirPresent:         stateDirPresent,
		DocumentMaximumBytes:    declarationartifact.MaximumBytes,
	})
}

func lowerLockDomainSteps(steps []operationplan.DomainStep) ([]mutation.Domain, error) {
	domains := make([]mutation.Domain, 0, len(steps))
	for _, step := range steps {
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

func equalPathLists(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
