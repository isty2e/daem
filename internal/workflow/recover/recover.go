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
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output/hostpath"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
)

type PlanInput struct {
	ManifestPath string
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
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return recoveryPreparation{}, err
	}
	if err := transaction.RequireClearFileSet(ctx, paths.StateDir); err != nil {
		return recoveryPreparation{}, err
	}
	ownershipRegistry, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		return recoveryPreparation{}, err
	}
	stateReader := stateReaderForPath(paths.StatefilePath)

	plan, err := journal.LoadActivePlanWithOptions(
		ctx,
		journalPaths(paths),
		journal.PlanLoadOptions{
			Filesystem: storagecommit.Adapter{},
			Resolver: hostpath.NewResolverWithManagedDataRoot(
				paths.ManifestRoot,
				paths.DataDir,
			).Resolve,
			OwnershipRegistry: ownershipRegistry.Load,
			Codecs:            aggregatecodec.Catalog(),
			StateCodec:        statefile.Codec{},
			StateReader:       stateReader,
		},
	)
	if err != nil {
		return recoveryPreparation{}, err
	}

	operationEvidence, err := recoveryOperationFingerprint(paths, plan)
	if err != nil {
		return recoveryPreparation{}, err
	}
	planned := recoveryPreparation{
		plan:              plan,
		paths:             paths,
		input:             input,
		operationEvidence: operationEvidence,
	}
	if !plan.Blocked() && !plan.HasErrors() {
		authorityEvidence, err := buildRecoveryAuthorityEvidence(paths, plan)
		if err != nil {
			return recoveryPreparation{}, fmt.Errorf("derive recovery mutation authority: %w", err)
		}
		planned.authorityEvidence = authorityEvidence
	}
	return planned, nil
}

func Execute(ctx context.Context, prepared *PreparedRecovery) (returnErr error) {
	execution, err := prepared.beginExecution()
	if err != nil {
		return err
	}
	if ctx == nil {
		return fmt.Errorf("recovery context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if execution.plan.Blocked() || execution.plan.HasErrors() {
		return fmt.Errorf("recovery is blocked")
	}
	visibleOperation, err := recoveryOperationFingerprint(execution.paths, execution.plan)
	if err != nil || !execution.operationEvidence.Equal(visibleOperation) {
		return errors.Join(mutation.StaleSnapshotError{}, err)
	}
	visibleAuthority, err := buildRecoveryAuthorityEvidence(execution.paths, execution.plan)
	if err != nil || !execution.authorityEvidence.authorityFingerprint.Equal(visibleAuthority.authorityFingerprint) {
		return errors.Join(mutation.StaleSnapshotError{}, err)
	}
	store, err := mutation.NewStore(execution.paths.DataDir)
	if err != nil {
		return err
	}
	effectPaths, err := execution.paths.WithDataDir(store.DataDir())
	if err != nil {
		return err
	}
	effectStateReader := stateReaderForPath(effectPaths.StatefilePath)
	leases, err := store.Acquire(ctx, execution.authorityEvidence.domains...)
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
	revisions, err := mutation.CaptureRevisionSet(ctx, execution.authorityEvidence.revisions...)
	if err != nil {
		return err
	}

	current, err := planRecovery(ctx, execution.input)
	if err != nil {
		return errors.Join(mutation.StaleSnapshotError{}, err)
	}
	if current.plan.Blocked() || current.plan.HasErrors() {
		return errors.Join(mutation.StaleSnapshotError{}, fmt.Errorf("recovery is blocked by current evidence"))
	}
	if !execution.operationEvidence.Equal(current.operationEvidence) ||
		!execution.authorityEvidence.authorityFingerprint.Equal(current.authorityEvidence.authorityFingerprint) ||
		!execution.plan.SameExecutionAuthority(current.plan) {
		return mutation.StaleSnapshotError{}
	}
	if matches, err := revisions.MatchesCurrent(ctx); err != nil {
		return err
	} else if !matches {
		return mutation.StaleSnapshotError{}
	}
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return err
	} else if !matches {
		return mutation.StaleSnapshotError{}
	}

	validateBeforeEffects := func(ctx context.Context, authority mutation.PhysicalAuthoritySet) error {
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
		covered, err := leases.CoversPhysicalAuthority(authority)
		if err != nil {
			return err
		}
		if !covered {
			return mutation.StaleSnapshotError{}
		}
		return nil
	}
	return execute.ExecuteRecoveryPlanWithOptions(ctx, current.plan, executePaths(effectPaths), execute.RecoveryOptions{
		ValidateBeforeEffects: validateBeforeEffects,
		Resolver: hostpath.NewResolverWithManagedDataRoot(
			effectPaths.ManifestRoot,
			effectPaths.DataDir,
		).Resolve,
		OwnershipRegistryBinder: ownershipstore.BindRooted,
		Codecs:                  aggregatecodec.Catalog(),
		StateCodec:              statefile.Codec{},
		StateReader:             effectStateReader,
		Filesystem:              storagecommit.Adapter{},
	})
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
