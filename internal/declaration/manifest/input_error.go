package manifest

import "errors"

type selectedInputError struct {
	cause error
}

func (failure selectedInputError) Error() string {
	return failure.cause.Error()
}

func (failure selectedInputError) Unwrap() error {
	return failure.cause
}

// InputErrorCause identifies selected declaration read or validation failures,
// excluding failures from contextual carrier-source resolution.
// Callers must preserve cancellation and recovery precedence before disclosure.
func InputErrorCause(err error) (error, bool) {
	var input selectedInputError
	if !errors.As(err, &input) {
		return nil, false
	}
	return input.cause, input.cause != nil
}
