package readiness

import "errors"

type relationReconciliationError struct {
	cause error
}

func newRelationReconciliationError(cause error) error {
	return relationReconciliationError{cause: cause}
}

func (failure relationReconciliationError) Error() string {
	return failure.cause.Error()
}

func (failure relationReconciliationError) Unwrap() error {
	return failure.cause
}

// IsRelationReconciliationError reports whether readiness failed while classifying
// carrier relation actions.
func IsRelationReconciliationError(err error) bool {
	var failure relationReconciliationError
	return errors.As(err, &failure)
}
