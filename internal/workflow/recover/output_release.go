package recover

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	ownershipobserve "github.com/isty2e/daem/internal/assurance/observe/ownership"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

type OutputReleasePhase string

const (
	OutputReleasePlanned     OutputReleasePhase = "planned"
	OutputReleaseNothing     OutputReleasePhase = "nothing_to_release"
	OutputReleaseReleased    OutputReleasePhase = "released"
	OutputReleaseRefused     OutputReleasePhase = "refused"
	OutputReleaseUnconfirmed OutputReleasePhase = "unconfirmed"
)

// OutputReleasePlan contains only disclosure facts, never write authority.
type OutputReleasePlan struct {
	ManifestPath          string
	RecordedManifests     []string
	Locations             []string
	WithheldLocationCount int
	ClaimCount            int
	BindingCount          int
}

func (plan OutputReleasePlan) Empty() bool { return plan.ClaimCount+plan.BindingCount == 0 }

type outputReleasePreparation struct {
	barrier             recoverygate.EffectAuthority
	paths               daempaths.Paths
	missing             statefile.MissingState
	registryDestination rootedpath.Destination
	outputs             ownership.OwnerOutputs
}

func (outputReleasePreparation) recoveryOperation() {}

func (prepared outputReleasePreparation) disclose() OutputReleasePlan {
	plan := OutputReleasePlan{ManifestPath: prepared.paths.ManifestPath}
	claims, bindings := prepared.outputs.Claims(), prepared.outputs.Bindings()
	plan.ClaimCount, plan.BindingCount = len(claims), len(bindings)
	bound := make(map[pathauthority.Exact]struct{}, len(bindings))
	for _, binding := range bindings {
		plan.RecordedManifests = append(plan.RecordedManifests, binding.Owner().ManifestPath())
		plan.Locations = append(plan.Locations, binding.Destination().String())
		bound[binding.PathAuthority()] = struct{}{}
	}
	unbound := make(map[pathauthority.Exact]struct{})
	for _, claim := range claims {
		plan.RecordedManifests = append(plan.RecordedManifests, claim.Owner().ManifestPath())
		physical := claim.Address().PathAuthority()
		if _, hasLogicalLocation := bound[physical]; !hasLogicalLocation {
			unbound[physical] = struct{}{}
		}
	}
	plan.WithheldLocationCount = len(unbound)
	slices.Sort(plan.RecordedManifests)
	plan.RecordedManifests = slices.Compact(plan.RecordedManifests)
	slices.Sort(plan.Locations)
	plan.Locations = slices.Compact(plan.Locations)
	return plan
}

func planOutputRelease(ctx context.Context, input PlanInput) (outputReleasePreparation, error) {
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return outputReleasePreparation{}, err
	}
	barrier, err := recoverygate.NewEffectAuthority(ctx, paths)
	if err != nil {
		return outputReleasePreparation{}, err
	}
	return observeOutputRelease(ctx, paths, barrier)
}

func observeOutputRelease(ctx context.Context, paths daempaths.Paths, barrier recoverygate.EffectAuthority) (_ outputReleasePreparation, returnErr error) {
	if err := barrier.Validate(ctx); err != nil {
		return outputReleasePreparation{}, err
	}
	budget := physicalwork.NewCaptureBudget(nil)
	missing, err := statefile.ObserveMissingState(ctx, paths.StatefilePath, budget)
	if err != nil {
		return outputReleasePreparation{}, err
	}
	root, destination, err := captureOutputRegistry(paths.OwnershipRegistryPath, budget)
	if err != nil {
		return outputReleasePreparation{}, outputRegistryFailure(err)
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	store, err := ownershipstore.NewRooted(root, destination, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return outputReleasePreparation{}, outputRegistryFailure(err)
	}
	registry, err := store.LoadRecordedRegistry(ctx)
	if err != nil {
		return outputReleasePreparation{}, outputRegistryFailure(err)
	}
	outputs, err := ownershipobserve.MatchMissingStateOutputs(ctx, missing, paths.ManifestPath, registry)
	if err != nil {
		return outputReleasePreparation{}, err
	}
	if !outputs.Empty() {
		if err := requireOutputRegistrySeparation(ctx, paths, destination, registry, budget); err != nil {
			return outputReleasePreparation{}, err
		}
	}
	return outputReleasePreparation{barrier: barrier, paths: paths, missing: missing, registryDestination: destination, outputs: outputs}, nil
}

func (prepared outputReleasePreparation) execute(ctx context.Context, writer mutationfs.RootedEntryCommitter) (phase OutputReleasePhase, returnErr error) {
	if ctx == nil {
		return OutputReleaseRefused, fmt.Errorf("output ownership release context is required")
	}
	if prepared.outputs.Empty() {
		current, err := observeOutputRelease(ctx, prepared.paths, prepared.barrier)
		if err != nil {
			return OutputReleaseRefused, err
		}
		if !current.missing.Equal(prepared.missing) || !current.registryDestination.Equal(prepared.registryDestination) || !current.outputs.Empty() {
			return OutputReleaseRefused, mutation.StaleSnapshotError{}
		}
		return OutputReleaseNothing, nil
	}
	if err := prepared.barrier.Validate(ctx); err != nil {
		return OutputReleaseRefused, err
	}
	store, err := mutation.NewStore(prepared.paths.DataDir)
	if err != nil {
		return OutputReleaseRefused, err
	}
	paths, err := prepared.paths.WithDataDir(store.DataDir())
	if err != nil {
		return OutputReleaseRefused, err
	}
	domains := prepared.barrier.Domains()
	for _, path := range []string{paths.StatefilePath, paths.OwnershipRegistryPath} {
		for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
			domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: path, Effect: effect, Access: mutation.AccessExclusive})
			if err != nil {
				return OutputReleaseRefused, err
			}
			domains = append(domains, domain)
		}
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return OutputReleaseRefused, err
	}
	defer func() { returnErr = errors.Join(returnErr, leases.Release()) }()
	budget := physicalwork.NewCaptureBudget(nil)
	root, destination, err := captureOutputRegistry(paths.OwnershipRegistryPath, budget)
	if err != nil {
		return OutputReleaseRefused, outputRegistryFailure(err)
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	if !prepared.registryDestination.Equal(destination) {
		return OutputReleaseRefused, mutation.StaleSnapshotError{}
	}
	registryStore, err := ownershipstore.NewRooted(root, destination, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return OutputReleaseRefused, outputRegistryFailure(err)
	}

	// Take the State proof and compare the selected rows again under the same
	// lease that protects the registry snapshot supplied to publication.
	var qualificationErr error
	outcome, err := registryStore.ReleaseOwnerOutputs(ctx, prepared.outputs, writer, func(ctx context.Context, registry ownership.Registry) error {
		qualificationErr = prepared.revalidate(ctx, paths, leases, destination, registry, budget)
		return qualificationErr
	})
	switch outcome.State() {
	case mutationfs.CommitOutcomeComplete:
		return OutputReleaseReleased, err
	case mutationfs.CommitOutcomeRetainedRecoverable, mutationfs.CommitOutcomeIndeterminate:
		return OutputReleaseUnconfirmed, outputRegistryFailure(err)
	default:
		if qualificationErr != nil {
			return OutputReleaseRefused, qualificationErr
		}
		return OutputReleaseRefused, outputRegistryFailure(err)
	}
}

func (prepared outputReleasePreparation) revalidate(ctx context.Context, paths daempaths.Paths, leases *mutation.LeaseSet, destination rootedpath.Destination, registry ownership.Registry, budget *physicalwork.Budget) error {
	if matches, err := leases.DomainsMatchCurrent(ctx); !matches || err != nil {
		return errors.Join(mutation.StaleSnapshotError{}, err)
	}
	if err := prepared.barrier.Validate(ctx); err != nil {
		return err
	}
	missing, err := statefile.ObserveMissingState(ctx, paths.StatefilePath, budget)
	if err != nil || !prepared.missing.Equal(missing) {
		return errors.Join(mutation.StaleSnapshotError{}, err)
	}
	current, err := ownershipobserve.MatchMissingStateOutputs(ctx, missing, paths.ManifestPath, registry)
	if err != nil {
		return err
	}
	if !current.Equal(prepared.outputs) {
		return mutation.StaleSnapshotError{}
	}
	return requireOutputRegistrySeparation(ctx, paths, destination, registry, budget)
}

func captureOutputRegistry(path string, budget *physicalwork.Budget) (*rootedpath.CapturedRoot, rootedpath.Destination, error) {
	return rootedpath.CaptureCanonicalDestinationBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
}

func requireOutputRegistrySeparation(ctx context.Context, paths daempaths.Paths, destination rootedpath.Destination, registry ownership.Registry, budget *physicalwork.Budget) error {
	registryPath, err := destination.LexicalPath()
	if err != nil {
		return outputRegistryFailure(err)
	}
	depth := physicalwork.MaximumPhysicalPathDepth
	registryEntry, err := mutation.ObserveDirectoryEntryAuthorityBounded(registryPath, depth, budget)
	if err != nil {
		return outputRegistryFailure(err)
	}
	for _, claim := range registry.Claims() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if registryEntry.MayOverlapExact(claim.Address().PathAuthority()) {
			return fmt.Errorf("the output ownership registry overlaps a recorded output")
		}
	}
	for _, binding := range registry.DocumentBindings() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if registryEntry.MayOverlapExact(binding.PathAuthority()) {
			return fmt.Errorf("the output ownership registry overlaps a recorded output")
		}
	}
	for _, path := range []string{
		paths.ManifestPath, paths.LockfilePath, paths.StatefilePath, paths.CacheDir, paths.SourceCacheDir,
		paths.RecoveryDir, paths.GlobalMetadataDir(), paths.LegacyUserStateDir, paths.CarrierClaimRegistryPath,
		filepath.Join(paths.DataDir, "locks"),
	} {
		if path == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, depth, budget)
		if err != nil {
			return outputRegistryFailure(err)
		}
		_, referent, err := mutation.ObserveFutureReferentAuthorityBounded(path, depth, budget)
		if err != nil {
			return outputRegistryFailure(err)
		}
		if registryEntry.MayOverlap(entry) || registryEntry.MayOverlap(referent) {
			if path == paths.CacheDir {
				return fmt.Errorf("the output ownership registry overlaps the cache directory; check that XDG_DATA_HOME and XDG_CACHE_HOME select separate locations")
			}
			return fmt.Errorf("the output ownership registry overlaps another workspace file or directory")
		}
	}
	return ctx.Err()
}

type outputRegistryVerificationError struct{ cause error }

func (failure outputRegistryVerificationError) Error() string {
	return "daem could not verify or update the output ownership registry"
}

func (failure outputRegistryVerificationError) Unwrap() error { return failure.cause }

func outputRegistryFailure(err error) error {
	if recoverygate.IsCancellation(err) {
		return err
	}
	return outputRegistryVerificationError{cause: err}
}
