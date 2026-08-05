package rootedpath

import (
	"fmt"
)

// FailureKind identifies why rooted-path authority could not be established
// or retained.
type FailureKind string

const (
	// FailureInvalidRoot means the physical root representation is invalid.
	FailureInvalidRoot FailureKind = "invalid_rooted_path_root"
	// FailureInvalidDestination means the root-relative destination is invalid.
	FailureInvalidDestination FailureKind = "invalid_rooted_path_destination"
	// FailureRootUnavailable means the selected root cannot be opened as a directory.
	FailureRootUnavailable FailureKind = "rooted_path_root_unavailable"
	// FailureRootReplaced means the selected path no longer names the captured root object.
	FailureRootReplaced FailureKind = "rooted_path_root_replaced"
	// FailureMountChanged means a captured root or ancestor mount identity no longer matches.
	FailureMountChanged FailureKind = "rooted_path_destination_mount_changed"
	// FailureRecoveryEvidenceUnavailable means durable recovery evidence could not be observed.
	FailureRecoveryEvidenceUnavailable FailureKind = "rooted_path_recovery_evidence_unavailable"
	// FailureAncestorSymlink means an existing destination ancestor is a symlink.
	FailureAncestorSymlink FailureKind = "rooted_path_destination_ancestor_symlink"
	// FailureDanglingAncestorSymlink means an existing destination ancestor is a dangling symlink.
	FailureDanglingAncestorSymlink FailureKind = "rooted_path_destination_dangling_ancestor_symlink"
	// FailureFinalSymlink means the final destination entry is a symlink.
	FailureFinalSymlink FailureKind = "rooted_path_destination_final_symlink"
	// FailureAncestorNotDirectory means an existing destination ancestor is not a directory.
	FailureAncestorNotDirectory FailureKind = "rooted_path_destination_ancestor_not_directory"
	// FailureAncestorChanged means destination ancestry changed after it was observed.
	FailureAncestorChanged FailureKind = "rooted_path_destination_ancestor_changed"
	// FailureIndeterminateBinding means root or ancestor binding changed after visibility.
	FailureIndeterminateBinding FailureKind = "rooted_path_destination_binding_indeterminate"
	// FailureUnsupportedPlatform means the platform cannot provide the required authority guarantee.
	FailureUnsupportedPlatform FailureKind = "rooted_path_authority_unsupported"
)

// Failure is a typed rooted-path authority failure. Path is diagnostic
// provenance only and never grants mutation authority.
type Failure struct {
	kind   FailureKind
	path   string
	detail string
	cause  error
}

// Kind returns the stable failure classification.
func (failure *Failure) Kind() FailureKind {
	if failure == nil {
		return ""
	}
	return failure.kind
}

// Path returns the diagnostic path associated with the failure.
func (failure *Failure) Path() string {
	if failure == nil {
		return ""
	}
	return failure.path
}

func (failure *Failure) Error() string {
	if failure == nil {
		return "captured root authority failure"
	}
	message := string(failure.kind)
	if failure.path != "" {
		message += fmt.Sprintf(" at %q", failure.path)
	}
	if failure.detail != "" {
		message += ": " + failure.detail
	}
	if failure.cause != nil {
		message += ": " + failure.cause.Error()
	}
	return message
}

// Unwrap returns the boundary error that caused the authority failure.
func (failure *Failure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

// NewBoundaryFailure constructs a typed containment result discovered by the
// native storage adapter while traversing a destination capability.
func NewBoundaryFailure(kind FailureKind, path string, detail string, cause error) *Failure {
	return newFailure(kind, path, detail, cause)
}

func newFailure(kind FailureKind, path string, detail string, cause error) *Failure {
	return &Failure{kind: kind, path: path, detail: detail, cause: cause}
}
