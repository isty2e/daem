package journal

import (
	"fmt"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
)

// Completion freezes capture facts without admitting either wire family or
// granting publication/recovery authority.
type ordinaryCaptureMaterial struct {
	operationID   string
	operation     string
	createdAt     string
	manifest      recoveryRootProvenance
	entries       []recoveryEntry
	before, after durable.Snapshot
	claims        []recoveryClaimTransition
	provisional   []recoveryProvisionalAcquireIntent
	removals      []recoveryRemovalIntent
	initialized   bool
}

type ordinaryCaptureBasis struct {
	operationID   string
	createdAt     time.Time
	before, after durable.Snapshot
	initialized   bool
}

func (journal nativeJournalSnapshots) ordinaryCaptureBasis() (ordinaryCaptureBasis, error) {
	if len(journal.record.Entries) != 0 || len(journal.record.ClaimTransitions) != 0 || len(journal.record.ProvisionalAcquires) != 0 || len(journal.record.RemovalIntents) != 0 {
		return ordinaryCaptureBasis{}, fmt.Errorf("mixed staging requires ordinary facts from the ordinary builder only")
	}
	createdAt, err := time.Parse(time.RFC3339, journal.record.CreatedAt)
	if err != nil || !isSafeRecoveryOperationID(journal.record.OperationID) || journal.record.Operation != recoveryOperationApply {
		return ordinaryCaptureBasis{}, fmt.Errorf("mixed journal has invalid operation metadata")
	}
	return ordinaryCaptureBasis{operationID: journal.record.OperationID, createdAt: createdAt, before: journal.before, after: journal.after, initialized: true}, nil
}

func (basis ordinaryCaptureBasis) OperationID() string      { return basis.operationID }
func (basis ordinaryCaptureBasis) CreatedAt() time.Time     { return basis.createdAt }
func (basis ordinaryCaptureBasis) Before() durable.Snapshot { return basis.before }
func (basis ordinaryCaptureBasis) After() durable.Snapshot  { return basis.after }

func (basis ordinaryCaptureBasis) Validate() error {
	if !basis.initialized {
		return fmt.Errorf("mixed journal has invalid operation metadata")
	}
	return nil
}

func (material ordinaryCaptureMaterial) ordinaryJournal() (recoveryJournal, error) {
	if !material.initialized {
		return recoveryJournal{}, fmt.Errorf("completed ordinary capture evidence is required")
	}
	return recoveryJournal{
		Version: recoveryJournalVersion, OperationID: material.operationID, Operation: material.operation,
		CreatedAt: material.createdAt, ManifestRootProvenance: material.manifest,
		Entries: material.entries, StatefileBefore: material.before, StatefileAfter: material.after,
		ClaimTransitions: material.claims, ProvisionalAcquires: material.provisional, RemovalIntents: material.removals,
	}, nil
}

func (journal nativeJournalSnapshots) withOrdinaryCapture(material ordinaryCaptureMaterial) (nativeJournalSnapshots, error) {
	if !material.initialized || material.operationID != journal.record.OperationID || material.operation != journal.record.Operation || material.createdAt != journal.record.CreatedAt || material.manifest != journal.record.ManifestRootProvenance || !material.before.Equal(journal.before) || !material.after.Equal(journal.after) {
		return nativeJournalSnapshots{}, fmt.Errorf("ordinary backup source does not match the native operation and complete Snapshots")
	}
	record := journal.record
	record.Entries = append([]recoveryEntry{}, material.entries...)
	record.ClaimTransitions = append([]recoveryClaimTransition{}, material.claims...)
	record.ProvisionalAcquires = append([]recoveryProvisionalAcquireIntent{}, material.provisional...)
	record.RemovalIntents = append([]recoveryRemovalIntent{}, material.removals...)
	journal.record = record
	return journal, nil
}
