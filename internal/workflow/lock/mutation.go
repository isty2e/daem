package lock

import (
	"context"
	"errors"
	"fmt"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
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
	stateDirAuthority, err := transaction.CaptureStateDirAuthority(ctx, paths.StateDir)
	if err != nil {
		return Result{}, err
	}
	outputPath := outputLockfilePath(input.LockfilePath, paths)
	errorContext := CommandError{
		ManifestPath:     paths.ManifestPath,
		LockfilePath:     outputPath,
		ExplicitLockfile: input.LockfilePath != "",
	}
	metadataTransactionPath, err := transaction.FileSetAuthorityPath(paths.StateDir)
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
	domains, err := lockMutationDomains(
		paths.ManifestPath,
		outputPath,
		metadataTransactionPath,
		optimisticLocalPaths,
	)
	if err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	stateDirDomains, err := lockStateDirDomains(
		paths.StateDir,
		stateDirAuthority.PresentAtCapture(),
	)
	if err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	domains = append(domains, stateDirDomains...)
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

	revisionRequests, err := lockRevisionRequests(
		paths.ManifestPath,
		outputPath,
		metadataTransactionPath,
		optimisticLocalPaths,
	)
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
	authority transaction.StateDirAuthority,
) error {
	return lockStateDirError(authority.RequireClear(ctx))
}

func lockStateDirError(err error) error {
	if errors.Is(err, transaction.ErrStateDirAppeared) {
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

func lockStateDirDomains(stateDir string, present bool) ([]mutation.Domain, error) {
	access := mutation.AccessShared
	if !present {
		access = mutation.AccessExclusive
	}
	domains := make([]mutation.Domain, 0, 2)
	for _, effect := range []mutation.PathEffect{
		mutation.PathEffectDirectoryEntry,
		mutation.PathEffectReferent,
	} {
		domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{
			Path: stateDir, Access: access, Effect: effect,
		})
		if err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	return domains, nil
}

func lockMutationDomains(
	manifestPath string,
	lockfilePath string,
	metadataTransactionPath string,
	localPaths []string,
) ([]mutation.Domain, error) {
	requests := []mutation.LogicalPathRequest{
		{Path: manifestPath, Access: mutation.AccessShared, Effect: mutation.PathEffectDirectoryEntry},
		{Path: manifestPath, Access: mutation.AccessShared, Effect: mutation.PathEffectReferent},
		{Path: lockfilePath, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry},
		{Path: lockfilePath, Access: mutation.AccessShared, Effect: mutation.PathEffectReferent},
		{Path: metadataTransactionPath, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry},
	}
	for _, path := range localPaths {
		requests = append(requests, mutation.LogicalPathRequest{Path: path, Access: mutation.AccessShared, Effect: mutation.PathEffectReferent})
	}
	domains := make([]mutation.Domain, 0, len(requests))
	for _, request := range requests {
		domain, err := mutation.NewLogicalPathDomain(request)
		if err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	return domains, nil
}

func lockRevisionRequests(
	manifestPath string,
	lockfilePath string,
	metadataTransactionPath string,
	localPaths []string,
) ([]mutation.RevisionRequest, error) {
	requests, err := mutation.BoundedFileRevisionRequests(
		declarationartifact.MaximumBytes,
		manifestPath,
		lockfilePath,
	)
	if err != nil {
		return nil, err
	}
	requests = append(
		requests,
		mutation.NewBoundedContentRevisionRequest(
			metadataTransactionPath,
			mutation.PathEffectDirectoryEntry,
		),
	)
	for _, path := range localPaths {
		requests = append(
			requests,
			mutation.NewBoundedContentRevisionRequest(path, mutation.PathEffectReferent),
		)
	}
	return requests, nil
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
