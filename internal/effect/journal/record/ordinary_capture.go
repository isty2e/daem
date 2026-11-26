package record

import (
	"fmt"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/supply/artifact"
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

func restoresRemovedProjectionRoot(entries []recoveryEntry) bool {
	for _, entry := range entries {
		if entry.ContentPath != "" && entry.Before.PathExisted && !entry.ExpectedAfter.PathExisted {
			return true
		}
	}
	return false
}

func usesInstalledEvidence(before, after durable.Snapshot, entries []recoveryEntry) bool {
	if before.HasInstalledHashes() || after.HasInstalledHashes() {
		return true
	}
	for _, entry := range entries {
		if entry.Before.TreeModes != "" || artifact.ContentHash(entry.Before.ContentHash).IsInstalled() || artifact.ContentHash(entry.ExpectedAfter.ContentHash).IsInstalled() {
			return true
		}
	}
	return false
}

func validateInstalledEvidenceVersion(version int, before, after durable.Snapshot, entries []recoveryEntry) error {
	if version != ordinaryInstalledVersion && version != nativeInstalledVersion && version != nativeWholeModeVersion && usesInstalledEvidence(before, after, entries) {
		return fmt.Errorf("recovery journal version %d cannot carry installed-content evidence", version)
	}
	return nil
}

func nativeCaptureVersion(before, after durable.Snapshot) int {
	if usesInstalledEvidence(before, after, nil) {
		return nativeInstalledVersion
	}
	return native17Version
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
	if usesInstalledEvidence(material.before, material.after, material.entries) {
		journal.Version = ordinaryInstalledVersion
	} else if restoresRemovedProjectionRoot(journal.Entries) {
		journal.Version = ordinaryRootRestoreVersion
	}
	return marshalRecoveryJournalWithStateContent(journal, statefileBefore, statefileAfter)
}

func (journal Native) WithOrdinaryEvidence(material OrdinaryEvidence) (Native, error) {
	if !material.initialized || material.operationID != journal.record.OperationID || material.operation != journal.record.Operation || material.createdAt != journal.record.CreatedAt || material.manifest != journal.record.ManifestRootProvenance || !material.before.Equal(journal.before) || !material.after.Equal(journal.after) {
		return Native{}, fmt.Errorf("ordinary backup source does not match the native operation and complete Snapshots")
	}
	record := journal.record
	if restoresRemovedProjectionRoot(material.entries) {
		if journal.current == nil {
			return Native{}, fmt.Errorf("publishing a removed projection root requires a bound native journal")
		}
		record.Version = max(record.Version, nativeRootRestoreVersion)
	}
	if usesInstalledEvidence(material.before, material.after, material.entries) {
		if journal.current == nil {
			return Native{}, fmt.Errorf("installed-content evidence requires a bound native journal")
		}
		record.Version = max(record.Version, nativeInstalledVersion)
	}
	record.Entries = append([]recoveryEntry{}, material.entries...)
	record.ClaimTransitions = append([]recoveryClaimTransition{}, material.claims...)
	record.ProvisionalAcquires = append([]recoveryProvisionalAcquireIntent{}, material.provisional...)
	record.RemovalIntents = append([]recoveryRemovalIntent{}, material.removals...)
	journal.record = record
	return journal, nil
}
