// Package contractversion owns the current versions of daem's public,
// serialized contracts.
package contractversion

// Manifest and lockfile schemas evolve independently.
const (
	ManifestSchema = 1
	LockfileSchema = 7
)

// MetadataTransaction identifies bounded file-set recovery evidence.
const MetadataTransaction = 3

// MetadataTransactionReference identifies file-set evidence with one retained
// commit-point after-image.
const MetadataTransactionReference = 4

// MetadataTransactionIntent identifies file-set evidence with a retained
// containing intent and sized before/after target facts.
const MetadataTransactionIntent = 5

// StateRelocation identifies a retired statefile's authority-transfer receipt.
const StateRelocation = 1

// CLI JSON envelope schemas evolve independently. Equal values do not imply a
// shared compatibility sequence.
const (
	CLIErrorJSON               = 2
	VersionJSON                = 1
	InitJSON                   = 1
	ManifestAuthoringJSON      = 7
	AgentConfigCaptureJSON     = 3
	AgentConfigReplacementJSON = 2
	LockComparisonJSON         = 6
	ResourceInventoryJSON      = 4
	OutputInventoryJSON        = 6
	PathInventoryJSON          = 2
	ReconciliationPlanJSON     = 20
	ApplyResultJSON            = 26
	RecoveryJSON               = 11
	StateMigrationJSON         = 1
	MetadataMigrationJSON      = 1
	DoctorJSON                 = 3
	MCPProbeJSON               = 1
	ExtensionRefreshJSON       = 4
)
