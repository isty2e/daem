// Package contractversion owns the current versions of daem's public,
// serialized contracts.
package contractversion

// Manifest and lockfile schemas evolve independently.
const (
	ManifestSchema = 1
	LockfileSchema = 6
)

// MetadataTransaction identifies bounded file-set recovery evidence.
const MetadataTransaction = 3

// StateRelocation identifies a retired statefile's authority-transfer receipt.
const StateRelocation = 1

// CLI JSON envelope schemas evolve independently. Equal values do not imply a
// shared compatibility sequence.
const (
	CLIErrorJSON           = 1
	VersionJSON            = 1
	InitJSON               = 1
	ManifestAuthoringJSON  = 7
	LockComparisonJSON     = 4
	ResourceInventoryJSON  = 3
	OutputInventoryJSON    = 5
	PathInventoryJSON      = 2
	ReconciliationPlanJSON = 17
	ApplyResultJSON        = 23
	RecoveryJSON           = 10
	StateMigrationJSON     = 1
	DoctorJSON             = 2
	MCPProbeJSON           = 1
	ExtensionRefreshJSON   = 4
)
