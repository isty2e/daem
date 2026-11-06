package applyprogram

import (
	"errors"
	"fmt"
)

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
