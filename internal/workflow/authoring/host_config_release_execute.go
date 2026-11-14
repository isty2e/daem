package authoring

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/execute/nativeoperation"
	"github.com/isty2e/daem/internal/effect/fileset"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/metadataconversion"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/operationplan"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/recoverygate"
)

func UnmanageHostConfig(ctx context.Context, request UnmanageHostConfigRequest) (result UnmanageHostConfigResult, resultErr error) {
	if ctx == nil {
		return result, fmt.Errorf("unmanage context is required")
	}
	if err := request.validate(); err != nil {
		return result, err
	}
	contributions, err := aggregatecodec.ForContributions(ctx, aggregatecodec.ContractIDs(), nativeAuthoringDocumentLimits())
	if err != nil {
		return result, err
	}
	paths, err := daempaths.Resolve(request.ManifestPath)
	if err != nil {
		return result, err
	}
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		return result, err
	}
	paths, err = paths.WithDataDir(store.DataDir())
	if err != nil {
		return result, err
	}
	barrier, err := recoverygate.NewEffectAuthority(ctx, paths)
	if err != nil {
		return result, err
	}
	if request.Mode == UnmanageModeWrite {
		if err := recoverHostConfigAuthoringFileSet(ctx, paths, barrier, store); err != nil {
			return result, err
		}
	}
	if err := barrier.Validate(ctx); err != nil {
		return result, err
	}
	budget := physicalwork.NewCaptureBudget(nil)
	optimistic, err := buildHostConfigRelease(ctx, paths, request, contributions, budget)
	if err != nil {
		return result, err
	}
	if request.Mode == UnmanageModeDryRun {
		return optimistic.result(false), nil
	}
	var conversionErr error
	if optimistic.management != nil {
		conversionErr = metadataconversion.ConvertIfNeeded(ctx, barrier)
		if recoverygate.IsCancellation(conversionErr) {
			return result, conversionErr
		}
		if err := barrier.Validate(ctx); err != nil {
			return result, errors.Join(err, conversionErr)
		}
		optimistic, err = buildHostConfigRelease(ctx, paths, request, contributions, budget)
		if err != nil {
			return result, err
		}
	}
	created := time.Now().UTC()
	id := journalpublication.OperationID(created)
	directory := filepath.Join(paths.RecoveryDir, id)
	marker, err := fileset.FileSetAuthorityPath(paths.StateDir)
	if err != nil {
		return result, err
	}
	allPaths := []string{paths.ManifestPath, paths.LockfilePath, paths.StatefilePath, paths.OwnershipRegistryPath, directory, filepath.Join(directory, journalrecord.FileName), marker}
	for _, file := range optimistic.files {
		allPaths = append(allPaths, file.path)
	}
	domains, err := lowerAuthoringDomainSteps(operationplan.CompileMetadataDomains(operationplan.MetadataDomainInput{TargetPaths: allPaths, MarkerPath: marker, TrailingDomains: barrier.Domains()}))
	if err != nil {
		return result, err
	}
	for _, path := range allPaths {
		for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
			domain, err := mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{Path: path, Access: mutation.AccessExclusive, Effect: effect, Target: "metadata", Scope: "project"})
			if err != nil {
				return result, err
			}
			domains = append(domains, domain)
		}
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, leases.Release(), ctx.Err()) }()
	validateLeases := func(ctx context.Context) error {
		matches, err := leases.DomainsMatchCurrent(ctx)
		if err != nil {
			return err
		}
		if !matches {
			return mutation.StaleSnapshotError{}
		}
		if err := recoverygate.RequireGlobalMetadataClear(ctx, paths); err != nil {
			if conversionErr != nil && recoverygate.IsGlobalMetadataRefusal(err) {
				return errors.Join(err, fmt.Errorf("automatic metadata conversion: %w", conversionErr))
			}
			return err
		}
		return nil
	}
	if err := validateLeases(ctx); err != nil {
		return result, err
	}
	if err := barrier.Validate(ctx); err != nil {
		return result, err
	}
	current, err := buildHostConfigRelease(ctx, paths, request, contributions, budget)
	if err != nil {
		return result, err
	}
	if !optimistic.same(current) {
		return result, mutation.StaleSnapshotError{}
	}
	revisionPaths := []string{paths.ManifestPath, paths.LockfilePath, paths.StatefilePath}
	for _, file := range current.files {
		revisionPaths = append(revisionPaths, file.path)
	}
	requests, err := mutation.BoundedFileRevisionRequests(declarationartifact.MaximumBytes, revisionPaths...)
	if err != nil {
		return result, err
	}
	revisions, err := mutation.CaptureRevisionSet(ctx, requests...)
	if err != nil {
		return result, err
	}
	if err := ensureStateDirForAuthoringEffect(ctx, barrier, revisions, leases); err != nil {
		return result, err
	}
	if err := validateLeases(ctx); err != nil {
		return result, err
	}
	stateDir, err := recoverygate.CaptureStateDirBounded(ctx, paths.StateDir, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return result, err
	}
	files, err := current.fileTargets()
	if err != nil {
		return result, err
	}
	matches, err := revisions.MatchesCurrent(ctx)
	if err != nil {
		return result, err
	}
	if !matches {
		return result, mutation.StaleSnapshotError{}
	}
	if err := barrier.Validate(ctx); err != nil {
		return result, err
	}
	if current.management == nil {
		if err := fileset.CommitFileSet(ctx, fileset.FileSetInput{StateDir: paths.StateDir, Targets: files}); err != nil {
			return result, err
		}
		return current.result(true), nil
	}
	release, err := current.release(ctx, id, created, budget)
	if err != nil {
		return result, err
	}
	physicalCodec, err := statefile.NewPhysicalCodec(current.codec)
	if err != nil {
		return result, err
	}
	local := journalrecord.NativeReleaseControlPaths{Manifest: current.files[0].path, DefaultLock: current.files[1].path, State: current.files[2].path}
	err = nativeoperation.ExecuteNativeRelease(ctx, release, local, files, execute.Paths{
		ManifestRoot: paths.ManifestRoot, StateDir: paths.StateDir, StatefilePath: paths.StatefilePath, RecoveryDir: paths.RecoveryDir, DataDir: paths.DataDir, OwnershipRegistryPath: paths.OwnershipRegistryPath,
	}, nativeoperation.ReleaseOptions{
		Filesystem: storagecommit.Adapter{}, RegistryBinder: ownershipstore.BindOwnReleaseRooted, Budget: budget, StateCodec: current.codec,
		StateReader: func(ctx context.Context) (durable.Snapshot, error) {
			return statefile.LoadOptionalWithOptions(ctx, paths.StatefilePath, statefile.ReadOptions{Codec: physicalCodec, PhysicalPathBudget: budget})
		},
		ValidateBeforeEffects: func(ctx context.Context, authority mutation.PhysicalAuthoritySet) error {
			if err := validateLeases(ctx); err != nil {
				return err
			}
			if err := stateDir.Validate(ctx); err != nil {
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
		},
		ValidateVisibilityAuthority: func(ctx context.Context) error {
			matches, err := leases.VisibilityAuthorityMatchesCurrent(ctx)
			if err != nil {
				return err
			}
			if !matches {
				return mutation.StaleSnapshotError{}
			}
			return nil
		},
		AcceptVisibilityChanges: func(ctx context.Context) error {
			accepted, err := leases.AcceptVisibilityChanges(ctx)
			if err != nil {
				return err
			}
			if !accepted {
				return mutation.StaleSnapshotError{}
			}
			return nil
		},
	})
	if err != nil {
		return result, err
	}
	return current.result(true), nil
}

func recoverHostConfigAuthoringFileSet(ctx context.Context, paths daempaths.Paths, barrier recoverygate.EffectAuthority, store mutation.Store) (resultErr error) {
	observation := barrier.Validate(ctx)
	if observation == nil {
		return nil
	}
	state := recoverygate.StateOf(observation)
	if state.Journal() != journal.InterruptionClear || state.FileSet() != fileset.FileSetFencePublishedTransaction {
		return observation
	}
	marker, err := fileset.FileSetAuthorityPath(paths.StateDir)
	if err != nil {
		return err
	}
	domains, err := lowerAuthoringDomainSteps(operationplan.CompileMetadataDomains(operationplan.MetadataDomainInput{TargetPaths: []string{paths.ManifestPath, paths.LockfilePath, paths.StatefilePath}, MarkerPath: marker, TrailingDomains: barrier.Domains()}))
	if err != nil {
		return err
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, leases.Release()) }()
	if err := barrier.ValidateFileSetRecovery(ctx); err != nil {
		return err
	}
	declarations, err := declarationartifact.CaptureRecoveryTargets(ctx, paths.ManifestPath, paths.LockfilePath)
	if err != nil {
		return err
	}
	matches, err := leases.DomainsMatchCurrent(ctx)
	if err != nil {
		return err
	}
	if !matches {
		return mutation.StaleSnapshotError{}
	}
	return fileset.RecoverFileSet(ctx, paths.StateDir, append(declarations.Paths(), paths.StatefilePath))
}

func (candidate hostConfigReleaseCandidate) fileTargets() ([]fileset.FileTarget, error) {
	var result []fileset.FileTarget
	for _, file := range candidate.files {
		var target fileset.FileTarget
		var err error
		if file.write && file.role == journalrecord.NativeReleaseState {
			target, err = fileset.NewFileCommitPointWrite(file.path, file.after)
		} else if file.write {
			target, err = fileset.NewFileWrite(file.path, file.after)
		} else {
			target, err = fileset.NewFileRetain(file.path)
		}
		if err != nil {
			return nil, err
		}
		result = append(result, target)
	}
	return result, nil
}

func (candidate hostConfigReleaseCandidate) release(ctx context.Context, id string, created time.Time, budget *physicalwork.Budget) (journalrecord.NativeRelease, error) {
	if candidate.management == nil {
		return journalrecord.NativeRelease{}, fmt.Errorf("host-config release has no retained management")
	}
	input := journalrecord.NativeReleaseInput{OperationID: id, CreatedAt: created, Owner: candidate.owner, Before: candidate.before, Management: *candidate.management, Namespace: candidate.namespace, OwnClaimsBefore: candidate.claims, BindingBefore: candidate.binding}
	for _, file := range candidate.files {
		observation, err := mutation.ObserveDirectoryEntryAuthorityBounded(file.path, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return journalrecord.NativeRelease{}, err
		}
		var namespace journalrecord.NativeReleaseNamespace
		if file.role == journalrecord.NativeReleaseState {
			namespace, err = journalrecord.NewNativeReleaseExactNamespace(candidate.owner.StatefileAuthority())
		} else if exact, present := observation.Exact(); present {
			namespace, err = journalrecord.NewNativeReleaseExactNamespace(exact)
		} else {
			missing, _ := observation.Provisional()
			namespace, err = journalrecord.NewNativeReleaseProvisionalNamespace(missing)
		}
		if err != nil {
			return journalrecord.NativeRelease{}, err
		}
		value := journalrecord.NativeReleaseFileInput{Role: file.role, Target: namespace, BeforeExists: file.exists, Write: file.write}
		if file.exists {
			value.BeforeSHA256, value.BeforeMode = journalrecord.Fingerprint(file.content), file.mode
		}
		if file.write {
			value.AfterSHA256 = journalrecord.Fingerprint(file.after)
		}
		intent, err := journalrecord.NewNativeReleaseFileIntent(value)
		if err != nil {
			return journalrecord.NativeRelease{}, err
		}
		input.Files = append(input.Files, intent)
	}
	return journalrecord.NewNativeRelease(ctx, input, candidate.codec)
}

func (candidate hostConfigReleaseCandidate) result(written bool) UnmanageHostConfigResult {
	result := UnmanageHostConfigResult{
		ManifestPath: candidate.paths.ManifestPath, LockfilePath: candidate.paths.LockfilePath, StatefilePath: candidate.paths.StatefilePath, RegistryPath: candidate.paths.OwnershipRegistryPath, ResourceID: candidate.request.ID, Target: candidate.selected, Scope: candidate.scope, Original: append([]byte(nil), candidate.files[0].content...), Content: append([]byte(nil), candidate.files[0].after...), Mode: candidate.request.Mode,
		ManifestStatus: UnmanageManifestStatusUnchanged, LockfileStatus: LockfileStatusUnchanged, ManagementStatus: UnmanageManagementStatusNotPresent, StatefileStatus: UnmanageStateStatusUnchanged, RegistryStatus: UnmanageStateStatusUnchanged,
	}
	if candidate.manifestChanged {
		if written {
			result.ManifestStatus = UnmanageManifestStatusRemoved
		} else {
			result.ManifestStatus = UnmanageManifestStatusWouldRemove
		}
	}
	if candidate.files[1].write {
		if written {
			result.LockfileStatus = LockfileStatusWritten
		} else {
			result.LockfileStatus = LockfileStatusWouldWrite
		}
	}
	if candidate.management != nil {
		if written {
			result.ManagementStatus, result.StatefileStatus = UnmanageManagementStatusReleased, UnmanageStateStatusWritten
		} else {
			result.ManagementStatus, result.StatefileStatus = UnmanageManagementStatusWouldRelease, UnmanageStateStatusWouldWrite
		}
	}
	if candidate.registryChanged {
		if written {
			result.RegistryStatus = UnmanageStateStatusWritten
		} else {
			result.RegistryStatus = UnmanageStateStatusWouldWrite
		}
	}
	return result
}
