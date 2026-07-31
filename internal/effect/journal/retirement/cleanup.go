package retirement

import "fmt"

// CleanupClassification is the public recovery classification reserved for
// cleanup-only journal retirement.
type CleanupClassification string

// CleanupActionKind is the public recovery action reserved for cleanup-only
// journal retirement.
type CleanupActionKind string

const (
	ClassificationRetainedCleanupResidue   CleanupClassification = "retained_cleanup_residue"
	ClassificationLegacyTombstoneMigration CleanupClassification = "legacy_tombstone_migration"
	ActionFinalizeJournalCleanup           CleanupActionKind     = "finalize_journal_cleanup"
	ActionMigrateLegacyJournalTombstone    CleanupActionKind     = "migrate_legacy_journal_tombstone"
)

// CleanupAuthority carries exact journal-retirement semantic identity and, for
// legacy migration, the validated source entry name. It carries no physical
// entry identity or filesystem capability.
type CleanupAuthority struct {
	record              Record
	residuePresent      bool
	legacyTombstoneName string
}

func (authority CleanupAuthority) valid() bool {
	if !authority.record.valid() {
		return false
	}
	if authority.legacyTombstoneName != "" {
		name := InspectName(authority.legacyTombstoneName)
		return name.kind == NameLegacyTombstone &&
			name.valid() &&
			authority.record.phase == PhasePrepared &&
			!authority.residuePresent
	}
	return authority.record.phase != PhasePrepared || authority.residuePresent
}

// OperationID returns the exact correlated operation.
func (authority CleanupAuthority) OperationID() string {
	return authority.record.identity.operationID
}

// JournalAuthorityFingerprint returns complete journal correlation.
func (authority CleanupAuthority) JournalAuthorityFingerprint() string {
	return authority.record.identity.journalAuthorityFingerprint
}

// Phase returns the currently durable cleanup phase.
func (authority CleanupAuthority) Phase() Phase {
	return authority.record.phase
}

// ControlName returns the only control entry this semantic authority may cover.
func (authority CleanupAuthority) ControlName() string {
	return authority.record.identity.ControlName()
}

// ResidueName returns the only journal residue this authority may cover.
func (authority CleanupAuthority) ResidueName() string {
	return authority.record.identity.ResidueName()
}

// GCName returns the only final GC name this authority may cover.
func (authority CleanupAuthority) GCName() string {
	return authority.record.identity.GCName()
}

// ResiduePresent reports the classified residue observation.
func (authority CleanupAuthority) ResiduePresent() bool {
	return authority.residuePresent
}

// RequiresLegacyMigration reports whether cleanup must first convert a
// validated v0.1 tombstone into the canonical residue namespace.
func (authority CleanupAuthority) RequiresLegacyMigration() bool {
	return authority.valid() && authority.legacyTombstoneName != ""
}

// LegacyTombstoneName returns the exact validated v0.1 source entry.
func (authority CleanupAuthority) LegacyTombstoneName() string {
	if !authority.RequiresLegacyMigration() {
		return ""
	}
	return authority.legacyTombstoneName
}

// RequiresPhaseAdvance reports whether cleanup must first persist finalizing.
func (authority CleanupAuthority) RequiresPhaseAdvance() bool {
	return authority.record.phase == PhasePrepared
}

// CurrentRecord returns the exact durable record selected by this authority.
func (authority CleanupAuthority) CurrentRecord() (Record, error) {
	if !authority.valid() {
		return Record{}, fmt.Errorf("journal cleanup authority is uninitialized")
	}
	return authority.record, nil
}

func (authority CleanupAuthority) equal(other CleanupAuthority) bool {
	return authority.valid() && other.valid() &&
		authority.record.Equal(other.record) &&
		authority.residuePresent == other.residuePresent &&
		authority.legacyTombstoneName == other.legacyTombstoneName
}

// CleanupPlan is the sole cleanup-only recovery prescription.
type CleanupPlan struct {
	authority CleanupAuthority
}

func (plan CleanupPlan) valid() bool {
	return plan.authority.valid()
}

// Classification returns the stable public cleanup classification.
func (plan CleanupPlan) Classification() CleanupClassification {
	if !plan.valid() {
		return ""
	}
	if plan.authority.RequiresLegacyMigration() {
		return ClassificationLegacyTombstoneMigration
	}
	return ClassificationRetainedCleanupResidue
}

// Action returns the stable public cleanup action.
func (plan CleanupPlan) Action() CleanupActionKind {
	if !plan.valid() {
		return ""
	}
	if plan.authority.RequiresLegacyMigration() {
		return ActionMigrateLegacyJournalTombstone
	}
	return ActionFinalizeJournalCleanup
}

// Authority returns semantic identity without physical mutation capability.
func (plan CleanupPlan) Authority() CleanupAuthority {
	return plan.authority
}

// SameExecutionAuthority reports whether two plans authorize the same cleanup
// phase and exact retirement artifacts.
func (plan CleanupPlan) SameExecutionAuthority(other CleanupPlan) bool {
	return plan.valid() && other.valid() && plan.authority.equal(other.authority)
}
