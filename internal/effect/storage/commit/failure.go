package commit

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

const (
	failureUncommitted          = mutationfs.FailureUncommitted
	failureIndeterminateCommit  = mutationfs.FailureIndeterminateCommit
	failureUnsupportedGuarantee = mutationfs.FailureUnsupportedGuarantee
	failureRetainedResidue      = mutationfs.FailureRetainedResidue
)

// ErrRegularFileReadLimitExceeded classifies a bounded regular-file read that
// observed more payload than its caller admitted.
var ErrRegularFileReadLimitExceeded = errors.New("regular file read limit exceeded")

// RegularFileReadLimitError reports the first size known to exceed a bounded
// regular-file read.
type RegularFileReadLimitError struct {
	maximumBytes  int64
	observedBytes int64
}

func (err *RegularFileReadLimitError) Error() string {
	if err == nil {
		return ErrRegularFileReadLimitExceeded.Error()
	}
	return fmt.Sprintf("regular file exceeds %d bytes", err.maximumBytes)
}

func (err *RegularFileReadLimitError) Unwrap() error {
	return ErrRegularFileReadLimitExceeded
}

// Limit returns the admitted payload size in bytes.
func (err *RegularFileReadLimitError) Limit() int64 {
	if err == nil {
		return 0
	}
	return err.maximumBytes
}

// Observed returns the first size known to exceed the admitted limit.
func (err *RegularFileReadLimitError) Observed() int64 {
	if err == nil {
		return 0
	}
	return err.observedBytes
}

func newRegularFileReadLimitError(maximumBytes int64, observedBytes int64) error {
	return &RegularFileReadLimitError{
		maximumBytes:  maximumBytes,
		observedBytes: observedBytes,
	}
}

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

func outcomeFromError(err error) mutationfs.CommitOutcome {
	if err == nil {
		return mustCommitOutcome(mutationfs.CommitOutcomeComplete, nil)
	}

	var concrete *failure
	if !errors.As(err, &concrete) {
		return mustCommitOutcome(mutationfs.CommitOutcomeIndeterminate, nil)
	}

	retainedNames := retainedSiblingNames(concrete.path, concrete.residue)
	switch concrete.kind {
	case failureUncommitted, failureUnsupportedGuarantee:
		if len(retainedNames) != 0 {
			return mustCommitOutcome(
				mutationfs.CommitOutcomeRetainedRecoverable,
				retainedNames,
			)
		}
		return mustCommitOutcome(mutationfs.CommitOutcomeUncommitted, nil)
	case failureRetainedResidue:
		if len(retainedNames) == 0 {
			return mustCommitOutcome(mutationfs.CommitOutcomeIndeterminate, nil)
		}
		return mustCommitOutcome(
			mutationfs.CommitOutcomeRetainedRecoverable,
			retainedNames,
		)
	case failureIndeterminateCommit:
		return mustCommitOutcome(
			mutationfs.CommitOutcomeIndeterminate,
			retainedNames,
		)
	default:
		return mustCommitOutcome(mutationfs.CommitOutcomeIndeterminate, nil)
	}
}

func mustCommitOutcome(
	state mutationfs.CommitOutcomeState,
	retainedNames []string,
) mutationfs.CommitOutcome {
	outcome, err := mutationfs.NewCommitOutcome(state, retainedNames)
	if err != nil {
		panic(fmt.Sprintf("construct storage commit outcome: %v", err))
	}
	return outcome
}

func retainedSiblingNames(primaryPath string, paths []string) []string {
	parent := filepath.Dir(primaryPath)
	names := make([]string, 0, len(paths))
	for _, path := range deduplicateStrings(paths) {
		if filepath.Dir(path) != parent {
			continue
		}
		name := filepath.Base(path)
		if name == "" || name == "." || name == string(filepath.Separator) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
