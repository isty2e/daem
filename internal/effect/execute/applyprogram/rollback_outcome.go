package applyprogram

import (
	"errors"
	"fmt"
)

// RollbackFailure retains a compensating failure separately from the error that
// triggered rollback. Unwrapping an apply error still identifies its primary cause.
type RollbackFailure struct{ Cause error }

func (failure RollbackFailure) Error() string        { return failure.Cause.Error() }
func (failure RollbackFailure) rollbackCause() error { return failure.Cause }

func FailedRollbackCause(err error) error {
	var failure interface{ rollbackCause() error }
	if errors.As(err, &failure) {
		return failure.rollbackCause()
	}
	return nil
}

type rollbackError struct {
	primary           error
	retirementFailure error
}

func NewRollbackError(primary error, retirementFailure error) error {
	return &rollbackError{primary: primary, retirementFailure: retirementFailure}
}

func (failure *rollbackError) Error() string {
	if failure.retirementFailure != nil {
		return fmt.Sprintf("%v; host changes rolled back; retire recovery journal failed: %v", failure.primary, failure.retirementFailure)
	}
	return fmt.Sprintf("%v; host changes rolled back", failure.primary)
}

func (failure *rollbackError) Unwrap() []error {
	if failure.retirementFailure != nil {
		return []error{failure.primary, failure.retirementFailure}
	}
	return []error{failure.primary}
}

// HostChangesRolledBack reports whether an unsuccessful apply restored every host change.
func HostChangesRolledBack(err error) bool {
	var failure *rollbackError
	return errors.As(err, &failure)
}
