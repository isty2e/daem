package fileset

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

// PairRecovery binds the private record and both current images of one selected
// v3 pair. It cannot supply targets other than those independently selected.
type PairRecovery struct{ evidence pairEvidence }

func ObservePairRecovery(ctx context.Context, stateDir, replacedPath, companionPath string) (*PairRecovery, error) {
	observation, err := observePair(ctx, stateDir, replacedPath, companionPath)
	if err != nil || observation == nil {
		return nil, err
	}
	defer observation.close()
	return &PairRecovery{evidence: observation.evidence}, nil
}

func (recovery *PairRecovery) Disposition() RecoveryDisposition { return recovery.evidence.settlement }

func (recovery *PairRecovery) RemovesCompanion() bool {
	return recovery.evidence.settlement == RecoveryRollback && recovery.evidence.removesCompanion
}

// Settle revalidates the same record under the caller's leases before restoring
// or retiring it. attempted reports whether a recovery effect may have started.
func (recovery *PairRecovery) Settle(ctx context.Context, revalidate func(context.Context) error) (attempted bool, err error) {
	if recovery == nil || revalidate == nil {
		return false, fmt.Errorf("file-set settlement requires evidence and revalidation")
	}
	evidence := recovery.evidence
	observation, err := observePair(ctx, evidence.stateDir, evidence.replacedPath, evidence.companionPath)
	if err != nil {
		return false, err
	}
	if observation == nil {
		return false, mutation.StaleSnapshotError{}
	}
	defer observation.close()
	if !evidence.same(observation.evidence) {
		return false, mutation.StaleSnapshotError{}
	}
	switch evidence.settlement {
	case RecoveryFinalize:
		if err := observation.confirmFinalImages(ctx, storagecommit.ConfirmFilePersistence); err != nil {
			return false, err
		}
		if err := revalidate(ctx); err != nil {
			return false, err
		}
		if err := observation.validateFinalImages(ctx); err != nil {
			return false, err
		}
	case RecoveryRollback:
		if err := observation.validateEvidence(ctx); err != nil {
			return false, err
		}
		if err := revalidate(ctx); err != nil {
			return false, err
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		// Once rollback starts, complete it and retire its evidence even if the
		// caller cancels. A failed restore leaves the record for a fresh recovery.
		ctx = context.WithoutCancel(ctx)
		if err := restoreTransaction(ctx, observation.marker, operations{}); err != nil {
			return true, err
		}
		if err := observation.validateRestoredImages(ctx); err != nil {
			return true, err
		}
	default:
		return false, fmt.Errorf("file-set settlement was not qualified")
	}
	retired, err := observation.retireEvidence(ctx)
	return evidence.settlement == RecoveryRollback || retired || errors.Is(err, ErrFileSetRetirementUnconfirmed), err
}

func (observation *pairObservation) validateRestoredImages(ctx context.Context) error {
	images, _, err := observePairImages(ctx, observation.marker, observation.budget)
	if err != nil {
		return err
	}
	for index, image := range images {
		if before, _ := observation.marker.Targets[index].positions(image.hash != "", image.hash); !before {
			return fmt.Errorf("%w: restored files no longer match the recorded before-state", ErrInterruptedFileSetTransaction)
		}
	}
	return nil
}
