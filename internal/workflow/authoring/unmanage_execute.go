package authoring

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	daempaths "github.com/isty2e/daem/internal/paths"
)

// UnmanageExtension removes one selected declaration and exact daem management
// authority while deliberately preserving host state.
func UnmanageExtension(
	ctx context.Context,
	request UnmanageExtensionRequest,
) (UnmanageExtensionResult, error) {
	validated, err := request.validate()
	if err != nil {
		return UnmanageExtensionResult{}, err
	}
	request = validated
	if ctx == nil {
		return UnmanageExtensionResult{}, fmt.Errorf("unmanage context is required")
	}
	if err := ctx.Err(); err != nil {
		return UnmanageExtensionResult{}, err
	}
	paths, err := daempaths.Resolve(request.ManifestPath)
	if err != nil {
		return UnmanageExtensionResult{}, err
	}
	request.ManifestPath = paths.ManifestPath
	if err := journal.RequireNoInterruptedApply(ctx, paths.RecoveryDir); err != nil {
		return UnmanageExtensionResult{}, err
	}
	if request.Mode == UnmanageModeDryRun {
		if err := transaction.RequireClearFileSet(ctx, paths.StateDir); err != nil {
			return UnmanageExtensionResult{}, err
		}
	} else {
		lockfilePath := request.LockfilePath
		if lockfilePath == "" {
			lockfilePath = paths.LockfilePath
		}
		if err := recoverUnmanageFileSetBeforeRead(ctx, paths, []string{
			paths.ManifestPath,
			lockfilePath,
			paths.StatefilePath,
			paths.CarrierClaimRegistryPath,
		}); err != nil {
			return UnmanageExtensionResult{}, err
		}
	}
	buildLockfile := request.Mode == UnmanageModeDryRun
	optimistic, err := buildUnmanageCandidate(ctx, request, paths, buildLockfile)
	if err != nil {
		return UnmanageExtensionResult{}, err
	}
	if optimistic.request.Mode == UnmanageModeDryRun {
		return resultFromCandidate(optimistic, false), nil
	}
	return commitUnmanageCandidate(ctx, optimistic)
}

func recoverUnmanageFileSetBeforeRead(
	ctx context.Context,
	paths daempaths.Paths,
	targetPaths []string,
) (returnErr error) {
	if err := transaction.RequireClearFileSet(ctx, paths.StateDir); err == nil {
		return nil
	}
	markerPath, err := transaction.FileSetAuthorityPath(paths.StateDir)
	if err != nil {
		return err
	}
	domains, err := unmanageMutationDomains(
		targetPaths,
		markerPath,
		nil,
		paths.RecoveryDir,
	)
	if err != nil {
		return err
	}
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		return err
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return err
	}
	defer func() {
		if err := leases.Release(); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return err
	} else if !matches {
		return mutation.StaleSnapshotError{}
	}
	if err := journal.RequireNoInterruptedApply(ctx, paths.RecoveryDir); err != nil {
		return err
	}
	if err := transaction.RecoverFileSet(ctx, paths.StateDir, targetPaths); err != nil {
		return err
	}
	return transaction.RequireClearFileSet(ctx, paths.StateDir)
}

func commitUnmanageCandidate(
	ctx context.Context,
	optimistic unmanageCandidate,
) (result UnmanageExtensionResult, returnErr error) {
	paths := optimistic.document.Paths
	markerPath, err := transaction.FileSetAuthorityPath(paths.StateDir)
	if err != nil {
		return UnmanageExtensionResult{}, err
	}
	declarationPaths := []string{
		optimistic.document.Path,
		optimistic.lockfile.Path(),
	}
	persistencePaths := []string{
		paths.StatefilePath,
		paths.CarrierClaimRegistryPath,
	}
	targetPaths := append(append([]string(nil), declarationPaths...), persistencePaths...)
	domains, err := unmanageMutationDomains(
		targetPaths,
		markerPath,
		optimistic.localPaths,
		paths.RecoveryDir,
	)
	if err != nil {
		return UnmanageExtensionResult{}, err
	}
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		return UnmanageExtensionResult{}, err
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return UnmanageExtensionResult{}, err
	}
	defer func() {
		if err := leases.Release(); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return UnmanageExtensionResult{}, err
	} else if !matches {
		return UnmanageExtensionResult{}, mutation.StaleSnapshotError{}
	}
	if err := journal.RequireNoInterruptedApply(ctx, paths.RecoveryDir); err != nil {
		return UnmanageExtensionResult{}, err
	}
	if err := transaction.RecoverFileSet(ctx, paths.StateDir, targetPaths); err != nil {
		return UnmanageExtensionResult{}, err
	}
	revisionRequests, err := unmanageRevisionRequests(
		declarationPaths,
		persistencePaths,
		markerPath,
		optimistic.localPaths,
		paths.RecoveryDir,
	)
	if err != nil {
		return UnmanageExtensionResult{}, err
	}
	revisions, err := mutation.CaptureRevisionSet(ctx, revisionRequests...)
	if err != nil {
		return UnmanageExtensionResult{}, err
	}
	current, err := buildUnmanageCandidate(
		ctx,
		optimistic.request,
		paths,
		true,
	)
	if err != nil {
		return UnmanageExtensionResult{}, err
	}
	if !equalPaths(optimistic.localPaths, current.localPaths) ||
		current.document.Path != optimistic.document.Path ||
		current.lockfile.Path() != optimistic.lockfile.Path() {
		return UnmanageExtensionResult{}, mutation.StaleSnapshotError{}
	}
	if matches, err := revisions.MatchesCurrent(ctx); err != nil {
		return UnmanageExtensionResult{}, err
	} else if !matches {
		return UnmanageExtensionResult{}, mutation.StaleSnapshotError{}
	}
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return UnmanageExtensionResult{}, err
	} else if !matches {
		return UnmanageExtensionResult{}, mutation.StaleSnapshotError{}
	}

	targets, err := fileTargets(current)
	if err != nil {
		return UnmanageExtensionResult{}, err
	}
	if err := transaction.CommitFileSet(ctx, transaction.FileSetInput{
		StateDir: paths.StateDir,
		Targets:  targets,
	}); err != nil {
		return UnmanageExtensionResult{}, err
	}
	return resultFromCandidate(current, true), nil
}

func fileTargets(current unmanageCandidate) ([]transaction.FileTarget, error) {
	stateContent, err := statefile.Marshal(current.nextState)
	if err != nil {
		return nil, fmt.Errorf("marshal unmanage statefile: %w", err)
	}
	registryContent, err := carrierclaim.Marshal(current.nextRegistry)
	if err != nil {
		return nil, fmt.Errorf("marshal unmanage carrier registry: %w", err)
	}
	specs := []struct {
		path    string
		content []byte
		write   bool
	}{
		{current.document.Path, current.manifestContent, current.manifestChanged},
		{
			current.lockfile.Path(),
			current.lockfile.Content(),
			current.lockfile.Status() == LockfileStatusWouldWrite,
		},
		{current.document.Paths.StatefilePath, stateContent, current.stateChanged},
		{
			current.document.Paths.CarrierClaimRegistryPath,
			registryContent,
			current.registryChanged,
		},
	}
	targets := make([]transaction.FileTarget, 0, len(specs))
	for _, spec := range specs {
		var target transaction.FileTarget
		var err error
		switch {
		case spec.path == current.document.Paths.CarrierClaimRegistryPath && spec.write:
			target, err = transaction.NewFileCommitPointWrite(spec.path, spec.content)
		case spec.write:
			target, err = transaction.NewFileWrite(spec.path, spec.content)
		default:
			target, err = transaction.NewFileRetain(spec.path)
		}
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func unmanageRevisionRequests(
	declarationPaths []string,
	persistencePaths []string,
	markerPath string,
	localPaths []string,
	recoveryDir string,
) ([]mutation.RevisionRequest, error) {
	requests, err := mutation.BoundedFileRevisionRequests(
		declarationartifact.MaximumBytes,
		declarationPaths...,
	)
	if err != nil {
		return nil, err
	}
	for _, path := range persistencePaths {
		requests = append(
			requests,
			mutation.NewBoundedContentRevisionRequest(path, mutation.PathEffectDirectoryEntry),
			mutation.NewBoundedContentRevisionRequest(path, mutation.PathEffectReferent),
		)
	}
	requests = append(
		requests,
		mutation.NewBoundedContentRevisionRequest(markerPath, mutation.PathEffectDirectoryEntry),
	)
	requests = append(
		requests,
		mutation.NewBoundedContentRevisionRequest(recoveryDir, mutation.PathEffectDirectoryEntry),
		mutation.NewBoundedContentRevisionRequest(recoveryDir, mutation.PathEffectReferent),
	)
	for _, path := range localPaths {
		requests = append(
			requests,
			mutation.NewBoundedContentRevisionRequest(path, mutation.PathEffectReferent),
		)
	}
	return requests, nil
}

func unmanageMutationDomains(
	targetPaths []string,
	markerPath string,
	localPaths []string,
	recoveryDir string,
) ([]mutation.Domain, error) {
	domains, err := metadataMutationDomains(targetPaths, markerPath, localPaths)
	if err != nil {
		return nil, err
	}
	for _, effect := range []mutation.PathEffect{
		mutation.PathEffectDirectoryEntry,
		mutation.PathEffectReferent,
	} {
		domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{
			Path:   recoveryDir,
			Access: mutation.AccessShared,
			Effect: effect,
		})
		if err != nil {
			return nil, fmt.Errorf("build unmanage recovery domain: %w", err)
		}
		domains = append(domains, domain)
	}
	return domains, nil
}

func resultFromCandidate(current unmanageCandidate, committed bool) UnmanageExtensionResult {
	selectedTarget, selectedScope := selectedAxes(current.selected)
	manifestStatus := UnmanageManifestStatusUnchanged
	if current.manifestChanged {
		manifestStatus = UnmanageManifestStatusWouldRemove
		if committed {
			manifestStatus = UnmanageManifestStatusRemoved
		}
	}
	managementStatus := UnmanageManagementStatusNotPresent
	if current.stateChanged || current.registryChanged {
		managementStatus = UnmanageManagementStatusWouldRelease
		if committed {
			managementStatus = UnmanageManagementStatusReleased
		}
	}
	lockfileStatus := current.lockfile.Status()
	if committed && lockfileStatus == LockfileStatusWouldWrite {
		lockfileStatus = LockfileStatusWritten
	}
	return UnmanageExtensionResult{
		ManifestPath:                 current.document.Path,
		LockfilePath:                 current.lockfile.Path(),
		StatefilePath:                current.document.Paths.StatefilePath,
		RegistryPath:                 current.document.Paths.CarrierClaimRegistryPath,
		Original:                     append([]byte(nil), current.document.Content...),
		Content:                      append([]byte(nil), current.manifestContent...),
		ResourceID:                   current.request.ID,
		Target:                       selectedTarget,
		Scope:                        selectedScope,
		ManifestStatus:               manifestStatus,
		LockfileStatus:               lockfileStatus,
		ManagementStatus:             managementStatus,
		StatefileStatus:              changedStateStatus(current.stateChanged, committed),
		RegistryStatus:               changedStateStatus(current.registryChanged, committed),
		HostStateRetained:            true,
		AmbientConsumersUnobservable: selectedScope == "global",
		DeclarationPresent:           current.selected.declaration != nil,
		Mode:                         current.request.Mode,
	}
}

func changedStateStatus(changed bool, committed bool) UnmanageStateStatus {
	if !changed {
		return UnmanageStateStatusUnchanged
	}
	if committed {
		return UnmanageStateStatusWritten
	}
	return UnmanageStateStatusWouldWrite
}

func equalPaths(left []string, right []string) bool {
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
