package journal

import (
	"context"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
)

type ordinaryCaptureMaterial struct {
	OperationID            string
	Operation              string
	CreatedAt              string
	ManifestRootProvenance recoveryRootProvenance
	Entries                []recoveryEntry
	StatefileBefore        durable.Snapshot
	StatefileAfter         durable.Snapshot
	ClaimTransitions       []recoveryClaimTransition
	ProvisionalAcquires    []recoveryProvisionalAcquireIntent
	RemovalIntents         []recoveryRemovalIntent
}

func buildRecoveryJournal(ctx context.Context, paths Paths, operationDir, operationID string, createdAt time.Time, currentState, nextState durable.Snapshot, options CaptureOptions) (recoveryJournal, error) {
	material, err := buildOrdinaryCaptureMaterial(ctx, paths, operationDir, operationID, createdAt, currentState, nextState, options, &ordinaryCaptureWork{})
	if err != nil {
		return recoveryJournal{}, err
	}
	return recoveryJournal{
		Version: recoveryJournalVersion, OperationID: material.OperationID, Operation: material.Operation,
		CreatedAt: material.CreatedAt, ManifestRootProvenance: material.ManifestRootProvenance,
		Entries: material.Entries, StatefileBefore: material.StatefileBefore, StatefileAfter: material.StatefileAfter,
		ClaimTransitions: material.ClaimTransitions, ProvisionalAcquires: material.ProvisionalAcquires,
		RemovalIntents: material.RemovalIntents,
	}, nil
}
