package delegate

import (
	"fmt"
)

// ReasonCode is a stable machine-readable delegate validation reason.
type ReasonCode string

const (
	ReasonInvalidRunnerKind   ReasonCode = "invalid_runner_kind"
	ReasonInvalidCommand      ReasonCode = "invalid_command"
	ReasonInvalidArgument     ReasonCode = "invalid_argument"
	ReasonInvalidEnvRef       ReasonCode = "invalid_env_ref"
	ReasonInvalidPackageRef   ReasonCode = "invalid_package_ref"
	ReasonMissingPackage      ReasonCode = "missing_package"
	ReasonInvalidDelegatePlan ReasonCode = "invalid_delegate_plan"
)

// ValidationError reports an invalid delegated executable plan state.
type ValidationError struct {
	reason  ReasonCode
	subject string
	message string
}

func validationError(reason ReasonCode, subject string, message string) *ValidationError {
	return &ValidationError{
		reason:  reason,
		subject: subject,
		message: message,
	}
}

// Error implements error.
func (err *ValidationError) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.subject == "" {
		return fmt.Sprintf("%s: %s", err.reason, err.message)
	}
	return fmt.Sprintf("%s: %s: %s", err.reason, err.subject, err.message)
}

// Code returns the stable delegate validation reason.
func (err *ValidationError) Code() ReasonCode {
	if err == nil {
		return ""
	}
	return err.reason
}

// Subject returns the invalid delegate plan subject when one is available.
func (err *ValidationError) Subject() string {
	if err == nil {
		return ""
	}
	return err.subject
}
