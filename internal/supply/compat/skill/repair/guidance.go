package repair

import (
	"fmt"
	"strings"
)

// GuidanceError retains the validation cause and completed repair classification
// separately from their raw diagnostic rendering.
type GuidanceError struct {
	cause          error
	classification Classification
}

// WithGuidance annotates a cause only when classification found manual or
// mechanical work. It does not perform repair or change the underlying cause.
func WithGuidance(cause error, classification Classification) error {
	if cause == nil {
		return nil
	}
	switch classification.Repairability() {
	case RepairabilityMechanical, RepairabilityManual:
		return &GuidanceError{cause: cause, classification: classification}
	default:
		return cause
	}
}

func (err *GuidanceError) Error() string {
	if err.classification.Repairability() == RepairabilityMechanical {
		return fmt.Sprintf(
			"%v; repairability=mechanical; next: set compat_repair = true on this manifest resource and rerun daem lock; repair actions: %s",
			err.cause, strings.Join(err.classification.Actions(), "; "),
		)
	}
	return fmt.Sprintf("%v; repairability=manual; manual edit required: %s", err.cause, strings.Join(err.classification.ManualReasons(), "; "))
}

func (err *GuidanceError) Unwrap() error { return err.cause }

func (err *GuidanceError) Classification() Classification { return err.classification }
