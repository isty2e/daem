// Package contractversion owns the current versions of daem's public,
// serialized contracts.
package contractversion

// Manifest and lockfile schemas evolve independently.
const (
	ManifestSchema = 1
	LockfileSchema = 6
)

// CLI JSON envelope schemas evolve independently. Equal values do not imply a
// shared compatibility sequence.
const (
	VersionJSON            = 1
	InitJSON               = 1
	ManifestAuthoringJSON  = 3
	LockComparisonJSON     = 3
	ResourceInventoryJSON  = 1
	OutputInventoryJSON    = 4
	PathInventoryJSON      = 1
	ReconciliationPlanJSON = 11
	ApplyResultJSON        = 16
	RecoveryJSON           = 4
	DoctorJSON             = 1
	MCPProbeJSON           = 1
	ExtensionRefreshJSON   = 2
)
