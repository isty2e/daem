package metadataconversion

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

// Cleanup holds only inactive-artifact selection. It never reconstructs or
// acquires authority over the original conversion's target cohort.
type Cleanup struct {
	paths daempaths.Paths
	plan  *fileset.PublicationCleanup
}

func InspectCleanup(ctx context.Context, paths daempaths.Paths, budget fileset.PhysicalWorkBudget) (Cleanup, error) {
	plan, err := fileset.InspectPublicationCleanup(ctx, paths.GlobalMetadataDir(), budget)
	if err != nil {
		return Cleanup{}, err
	}
	paths, err = paths.WithDataDir(filepath.Dir(plan.Root()))
	if err != nil {
		plan.Close()
		return Cleanup{}, err
	}
	return Cleanup{paths: paths, plan: plan}, nil
}

func (cleanup Cleanup) Paths() []string    { return cleanup.plan.Paths() }
func (cleanup Cleanup) Deferred() []string { return cleanup.plan.Deferred() }
func (cleanup Cleanup) Close()             { cleanup.plan.Close() }

func (cleanup Cleanup) Disclosure() Disclosure {
	return Disclosure{ManifestPath: cleanup.paths.ManifestPath, DataDir: cleanup.paths.DataDir}
}

func (cleanup Cleanup) Execute(ctx context.Context) (returnErr error) {
	if ctx == nil || cleanup.plan == nil {
		return fmt.Errorf("metadata cleanup is not initialized")
	}
	global, err := recoverygate.GlobalMetadataAuthorityPath(cleanup.paths)
	if err != nil {
		return err
	}
	var domains []mutation.Domain
	for _, path := range []string{global, cleanup.paths.GlobalMetadataDir()} {
		for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
			domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{
				Path: path, Effect: effect, Access: mutation.AccessExclusive,
			})
			if err != nil {
				return err
			}
			domains = append(domains, domain)
		}
	}
	store, err := mutation.NewStore(cleanup.paths.DataDir)
	if err != nil {
		return err
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, leases.Release()) }()
	if err := validateHeldMetadataLeases(ctx, leases); err != nil {
		return err
	}
	return cleanup.plan.Execute(ctx)
}
