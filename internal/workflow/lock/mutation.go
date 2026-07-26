package lock

import (
	"context"
	"errors"
	"fmt"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/declaration/transaction"
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
	if err := transaction.RequireClearFileSet(ctx, paths.StateDir); err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	optimisticLocalPaths, err := commandLocalPaths(paths)
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
	if err := transaction.RequireClearFileSet(ctx, paths.StateDir); err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}

	revisions, err := mutation.CaptureRevisionSet(
		ctx,
		lockRevisionRequests(
			paths.ManifestPath,
			outputPath,
			metadataTransactionPath,
			optimisticLocalPaths,
		)...,
	)
	if err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	currentLocalPaths, err := commandLocalPaths(paths)
	if err != nil {
		errorContext.Err = err
		return Result{}, errorContext
	}
	if !equalPathLists(optimisticLocalPaths, currentLocalPaths) {
		errorContext.Err = mutation.StaleSnapshotError{}
		return Result{}, errorContext
	}

	built, err := buildCommandResult(
		ctx,
		input.ManifestPath,
		input.LockfilePath,
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
	if err := commitLockfile(ctx, built.Result.LockfilePath, built.Content, 0o600); err != nil {
		return Result{}, fmt.Errorf("write lockfile: %w", err)
	}
	return built.Result, nil
}

func commandLocalPaths(paths daempaths.Paths) ([]string, error) {
	environment, err := declarationmanifest.LoadSelected(paths)
	if err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}
	return lockgenerate.ConsumedLocalPaths(lockgenerate.Input{
		Paths:       paths,
		Environment: environment,
	})
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
) []mutation.RevisionRequest {
	requests := []mutation.RevisionRequest{
		{Path: manifestPath, Effect: mutation.PathEffectDirectoryEntry},
		{Path: manifestPath, Effect: mutation.PathEffectReferent},
		{Path: lockfilePath, Effect: mutation.PathEffectDirectoryEntry},
		{Path: lockfilePath, Effect: mutation.PathEffectReferent},
		{Path: metadataTransactionPath, Effect: mutation.PathEffectDirectoryEntry},
	}
	for _, path := range localPaths {
		requests = append(requests, mutation.RevisionRequest{Path: path, Effect: mutation.PathEffectReferent})
	}
	return requests
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
