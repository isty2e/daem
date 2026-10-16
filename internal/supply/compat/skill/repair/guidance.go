package repair

// GuidanceError retains the validation cause separately from completed repair
// facts. Error renders the cause; presentation owns repair instructions.
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
	return err.cause.Error()
}

func (err *GuidanceError) Unwrap() error { return err.cause }

func (err *GuidanceError) Classification() Classification { return err.classification }
