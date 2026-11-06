package filesystem

import "errors"

type indeterminateEffectError struct{ cause error }

func (failure *indeterminateEffectError) Error() string     { return failure.cause.Error() }
func (failure *indeterminateEffectError) Unwrap() error     { return failure.cause }
func (failure *indeterminateEffectError) Kind() FailureKind { return FailureIndeterminateCommit }

// MarkEffectIndeterminate preserves uncertainty discovered after a possible
// effect, even when its cause describes an uncommitted verification operation.
func MarkEffectIndeterminate(err error) error {
	if err == nil {
		return nil
	}
	return &indeterminateEffectError{cause: err}
}

func HasIndeterminateEffect(err error) bool {
	var failure *indeterminateEffectError
	return errors.As(err, &failure)
}
