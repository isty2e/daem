package mutation

import "errors"

// ReasonCode is a stable machine-readable mutation failure category.
type ReasonCode string

const (
	ReasonStaleSnapshot ReasonCode = "stale_snapshot"
	ReasonStalePlan     ReasonCode = "stale_plan"
	ReasonContention    ReasonCode = "mutation_contended"
	ReasonCanceled      ReasonCode = "mutation_cancelled"
)

func (StaleSnapshotError) Code() ReasonCode { return ReasonStaleSnapshot }

func (StalePlanError) Code() ReasonCode { return ReasonStalePlan }

func (ContentionError) Code() ReasonCode { return ReasonContention }

func (CancellationError) Code() ReasonCode { return ReasonCanceled }

// ReasonCodeOf extracts a stable mutation reason from wrapped or joined errors.
func ReasonCodeOf(err error) (ReasonCode, bool) {
	var coded interface{ Code() ReasonCode }
	if errors.As(err, &coded) {
		return coded.Code(), true
	}
	return "", false
}
