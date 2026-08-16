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

// CLI JSON envelope schemas evolve independently. Equal values do not imply a
// shared compatibility sequence.
const (
	VersionJSON            = 1
	InitJSON               = 1
	ManifestAuthoringJSON  = 4
	LockComparisonJSON     = 4
	ResourceInventoryJSON  = 2
	OutputInventoryJSON    = 4
	PathInventoryJSON      = 1
	ReconciliationPlanJSON = 12
	ApplyResultJSON        = 18
	RecoveryJSON           = 7
	DoctorJSON             = 1
	MCPProbeJSON           = 1
	ExtensionRefreshJSON   = 3
)
