package filesystem

import "errors"

// FailureKind is the strongest conclusion an Effect caller may draw after a
// guarded filesystem operation fails.
type FailureKind string

const (
	FailureUncommitted          FailureKind = "uncommitted"
	FailureIndeterminateCommit  FailureKind = "indeterminate_commit"
	FailureUnsupportedGuarantee FailureKind = "unsupported_guarantee"
	FailureRetainedResidue      FailureKind = "retained_residue"
)

// ClassifiedFailure exposes only Effect-relevant commit visibility. Concrete
// filesystem phases, paths, residue, and causes remain Boundary diagnostics.
type ClassifiedFailure interface {
	error
	Kind() FailureKind
}

// FailureKindOf returns the Effect-visible classification carried by err.
func FailureKindOf(err error) (FailureKind, bool) {
	var failure ClassifiedFailure
	if !errors.As(err, &failure) {
		return "", false
	}
	return failure.Kind(), true
}

// MayHaveVisibleEffect reports whether the classified failure can follow
// namespace visibility. Unknown errors are conservatively treated as visible.
func MayHaveVisibleEffect(err error) bool {
	if err == nil {
		return false
	}
	kind, classified := FailureKindOf(err)
	if !classified {
		return true
	}
	switch kind {
	case FailureUncommitted, FailureUnsupportedGuarantee:
		return false
	case FailureIndeterminateCommit, FailureRetainedResidue:
		return true
	default:
		return true
	}
}
