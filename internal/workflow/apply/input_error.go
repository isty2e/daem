package apply

import "errors"

type commandInputError struct {
	cause error
}

func (failure commandInputError) Error() string {
	return failure.cause.Error()
}

func (failure commandInputError) Unwrap() error {
	return failure.cause
}

// InputErrorCause returns an error from a selected declaration-input read, not
// an arbitrary internal error with similar wording or an input-like error type.
// Callers must preserve failure-disposition precedence before disclosing it.
func InputErrorCause(err error) (error, bool) {
	var input commandInputError
	if !errors.As(err, &input) {
		return nil, false
	}
	return input.cause, input.cause != nil
}
