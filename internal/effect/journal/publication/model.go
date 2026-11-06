package publication

import (
	"time"
)

// Paths identifies the host paths recovery needs without depending on CLI command state.
type Paths struct {
	RecoveryDir   string
	StatefilePath string
	ManifestRoot  string
	DataDir       string
}

// CaptureResult identifies the committed recovery journal for one interrupted operation.
// Stable persistence remains governed by the storage durability contract.
type CaptureResult struct {
	OperationID       string
	Directory         string
	JournalPath       string
	RecordFingerprint string
}

// OperationID returns a stable apply recovery operation identifier for a timestamp.
func OperationID(createdAt time.Time) string {
	return createdAt.UTC().Format("20060102T150405.000000000Z") + "-apply"
}

func PlanningOperationID() string {
	return OperationID(time.Unix(0, 0).UTC())
}
