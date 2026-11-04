package record

import (
	"fmt"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
)

// Completion freezes capture facts without admitting either wire family or
// granting publication/recovery authority.
type OrdinaryEvidence struct {
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

type OrdinaryCaptureBasis struct {
	operationID   string
	createdAt     time.Time
	before, after durable.Snapshot
	initialized   bool
}

func (journal Native) OrdinaryCaptureBasis() (OrdinaryCaptureBasis, error) {
	if len(journal.record.Entries) != 0 || len(journal.record.ClaimTransitions) != 0 || len(journal.record.ProvisionalAcquires) != 0 || len(journal.record.RemovalIntents) != 0 {
		return OrdinaryCaptureBasis{}, fmt.Errorf("mixed staging requires ordinary facts from the ordinary builder only")
	}
	createdAt, err := time.Parse(time.RFC3339, journal.record.CreatedAt)
	if err != nil || !IsSafeOperationID(journal.record.OperationID) || journal.record.Operation != OperationApply {
		return OrdinaryCaptureBasis{}, fmt.Errorf("mixed journal has invalid operation metadata")
	}
	return OrdinaryCaptureBasis{operationID: journal.record.OperationID, createdAt: createdAt, before: journal.before, after: journal.after, initialized: true}, nil
}

func (basis OrdinaryCaptureBasis) OperationID() string      { return basis.operationID }
func (basis OrdinaryCaptureBasis) CreatedAt() time.Time     { return basis.createdAt }
func (basis OrdinaryCaptureBasis) Before() durable.Snapshot { return basis.before }
func (basis OrdinaryCaptureBasis) After() durable.Snapshot  { return basis.after }

func (basis OrdinaryCaptureBasis) Validate() error {
	if !basis.initialized {
		return fmt.Errorf("mixed journal has invalid operation metadata")
	}
	return nil
}

func (material OrdinaryEvidence) EncodeOrdinary(statefileBefore, statefileAfter []byte) ([]byte, error) {
	if !material.initialized {
		return nil, fmt.Errorf("completed ordinary capture evidence is required")
	}
	journal := recoveryJournal{
		Version: OrdinaryVersion, OperationID: material.operationID, Operation: material.operation,
		CreatedAt: material.createdAt, ManifestRootProvenance: material.manifest,
		Entries: material.entries, StatefileBefore: material.before, StatefileAfter: material.after,
		ClaimTransitions: material.claims, ProvisionalAcquires: material.provisional, RemovalIntents: material.removals,
	}
	return marshalRecoveryJournalWithStateContent(journal, statefileBefore, statefileAfter)
}

func (journal Native) WithOrdinaryEvidence(material OrdinaryEvidence) (Native, error) {
	if !material.initialized || material.operationID != journal.record.OperationID || material.operation != journal.record.Operation || material.createdAt != journal.record.CreatedAt || material.manifest != journal.record.ManifestRootProvenance || !material.before.Equal(journal.before) || !material.after.Equal(journal.after) {
		return Native{}, fmt.Errorf("ordinary backup source does not match the native operation and complete Snapshots")
	}
	record := journal.record
	record.Entries = append([]recoveryEntry{}, material.entries...)
	record.ClaimTransitions = append([]recoveryClaimTransition{}, material.claims...)
	record.ProvisionalAcquires = append([]recoveryProvisionalAcquireIntent{}, material.provisional...)
	record.RemovalIntents = append([]recoveryRemovalIntent{}, material.removals...)
	journal.record = record
	return journal, nil
}
