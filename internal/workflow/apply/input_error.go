package apply

import (
	"errors"
	"fmt"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
)

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

func validateSelectedExtensionOrder(
	extensions []desiredextension.Extension,
	locked lock.File,
	resolveIdentity lock.ExtensionOrderIdentityResolver,
) error {
	identityResolutionFailed := false
	selectedResolver := resolveIdentity
	if resolveIdentity != nil {
		selectedResolver = func(carrier desiredextension.CarrierKey) (hostrelation.HostLoadIdentity, error) {
			identity, err := resolveIdentity(carrier)
			if err != nil {
				identityResolutionFailed = true
			}
			return identity, err
		}
	}
	err := lockrefine.ValidateCurrentExtensionOrder(extensions, locked, selectedResolver)
	if err == nil {
		return nil
	}
	if identityResolutionFailed {
		return fmt.Errorf("resolve selected extension order identity: %w", err)
	}
	return commandInputError{cause: fmt.Errorf("%w: %w", ErrReadLockfile, err)}
}
