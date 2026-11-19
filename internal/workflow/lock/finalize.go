package lock

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/operationplan"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

// MetadataRecoveryStatus is the portion of a lock failure relevant to the
// selected declaration pair. A published fence does not prove eligibility.
type MetadataRecoveryStatus uint8

const (
	MetadataRecoveryNotApplicable MetadataRecoveryStatus = iota
	MetadataRecoveryPending
	MetadataRecoveryDifferentPair
	MetadataRecoveryUnfinishedPair
	MetadataRecoveryRetired
	MetadataRecoveryRetirementUnconfirmed
)

type afterMetadataRetirement struct{ cause error }

func (err afterMetadataRetirement) Error() string { return err.cause.Error() }
func (err afterMetadataRetirement) Unwrap() error { return err.cause }

func MetadataRecoveryStatusOf(err error) MetadataRecoveryStatus {
	var retired afterMetadataRetirement
	switch {
	case errors.As(err, &retired):
		return MetadataRecoveryRetired
	case errors.Is(err, fileset.ErrFileSetRetirementUnconfirmed):
		return MetadataRecoveryRetirementUnconfirmed
	case !localPublishedFileSet(err):
		return MetadataRecoveryNotApplicable
	case errors.Is(err, fileset.ErrDifferentFinalizationPair):
		return MetadataRecoveryDifferentPair
	case errors.Is(err, fileset.ErrUnfinishedFinalizationPair):
		return MetadataRecoveryUnfinishedPair
	default:
		return MetadataRecoveryPending
	}
}

func localPublishedFileSet(err error) bool {
	return !recoverygate.IsCancellation(err) && !recoverygate.IsGlobalMetadataRefusal(err) &&
		fileset.FileSetFenceKindOf(err) == fileset.FileSetFencePublishedTransaction
}

func finishLockMetadata(ctx context.Context, paths daempaths.Paths, stateDir recoverygate.StateDirAuthority, markerPath string) (pair *transaction.FinalizationPair, retired bool, returnErr error) {
	barrier, err := recoverygate.NewEffectAuthorityForStateDir(ctx, paths, stateDir)
	if err != nil {
		return nil, false, err
	}
	steps := operationplan.CompileMetadataDomains(operationplan.MetadataDomainInput{
		TargetPaths:     []string{paths.ManifestPath, paths.LockfilePath},
		MarkerPath:      markerPath,
		TrailingDomains: barrier.Domains(),
	})
	domains, err := lowerLockDomainSteps(steps)
	if err != nil {
		return nil, false, err
	}
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		return nil, false, err
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return nil, false, err
	}
	defer func() { returnErr = errors.Join(returnErr, leases.Release()) }()

	physicalStateDir := filepath.Dir(markerPath)
	validate := func(ctx context.Context) error {
		matches, err := leases.DomainsMatchCurrent(ctx)
		if err != nil {
			return err
		}
		if !matches {
			return mutation.StaleSnapshotError{}
		}
		current, err := stateDir.ExistingPath(ctx)
		if err != nil {
			return err
		}
		if current != physicalStateDir {
			return mutation.StaleSnapshotError{}
		}
		if err := recoverygate.RequireUserStateMigrated(ctx, paths); err != nil {
			return err
		}
		return barrier.ValidateFileSetRecovery(ctx)
	}
	if err := validate(ctx); err != nil {
		return nil, false, err
	}
	selection, err := transaction.CaptureFinalizationPair(ctx, paths.ManifestPath, paths.LockfilePath)
	if err != nil {
		return nil, false, err
	}
	controls := []string{
		markerPath, paths.RecoveryDir, paths.StatefilePath, paths.GlobalMetadataDir(),
		paths.OwnershipRegistryPath, paths.CarrierClaimRegistryPath, paths.LegacyUserStateDir,
	}
	retired, err = selection.RetireSettled(ctx, physicalStateDir, controls, physicalwork.NewPathBudget(), validate)
	if err != nil {
		return nil, retired, err
	}
	if err := barrier.Validate(ctx); err != nil {
		return nil, retired, err
	}
	return &selection, retired, nil
}
