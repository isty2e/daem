package commit

import (
	"errors"
	"fmt"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

const (
	failureUncommitted          = mutationfs.FailureUncommitted
	failureIndeterminateCommit  = mutationfs.FailureIndeterminateCommit
	failureUnsupportedGuarantee = mutationfs.FailureUnsupportedGuarantee
	failureRetainedResidue      = mutationfs.FailureRetainedResidue
)

// failure reports one storage commit boundary failure.
type failure struct {
	kind    mutationfs.FailureKind
	phase   string
	path    string
	residue []string
	cause   error
}

// Kind returns the commit-state classification.
func (failure *failure) Kind() mutationfs.FailureKind {
	if failure == nil {
		return ""
	}
	return failure.kind
}

func (failure *failure) Error() string {
	if failure == nil {
		return "<nil>"
	}
	message := fmt.Sprintf("storage commit %s at %s for %q", failure.kind, failure.phase, failure.path)
	if len(failure.residue) != 0 {
		message += fmt.Sprintf(" (residue: %v)", failure.residue)
	}
	if failure.cause != nil {
		message += ": " + failure.cause.Error()
	}
	return message
}

// Unwrap returns the underlying filesystem, context, or injected error.
func (failure *failure) Unwrap() error { return failure.cause }

func newFailure(kind mutationfs.FailureKind, phase phase, path string, cause error, residue ...string) error {
	return &failure{
		kind:    kind,
		phase:   string(phase),
		path:    path,
		residue: deduplicateStrings(residue),
		cause:   cause,
	}
}

type unsupportedError struct {
	detail string
	cause  error
}

func (err *unsupportedError) Error() string {
	if err.cause == nil {
		return err.detail
	}
	return err.detail + ": " + err.cause.Error()
}

func (err *unsupportedError) Unwrap() error { return err.cause }

func unsupported(detail string, cause error) error {
	return &unsupportedError{detail: detail, cause: cause}
}

func isUnsupported(err error) bool {
	var target *unsupportedError
	return errors.As(err, &target)
}

func deduplicateStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func failureBeforeVisibility(failedPhase phase, path string, cause error) error {
	kind := failureUncommitted
	if isUnsupported(cause) {
		kind = failureUnsupportedGuarantee
	}
	return newFailure(kind, failedPhase, path, cause)
}
