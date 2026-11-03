package aggregate

import (
	"fmt"

	"github.com/isty2e/daem/internal/output"
)

// CodecFailureReason identifies one redaction-safe codec failure class.
type CodecFailureReason string

const (
	CodecFailureDocumentMalformed        CodecFailureReason = "document_malformed"
	CodecFailureDuplicateKey             CodecFailureReason = "duplicate_key"
	CodecFailureSelectedShapeUnsupported CodecFailureReason = "selected_shape_unsupported"
	CodecFailureUnsupportedTransport     CodecFailureReason = "unsupported_transport"
	CodecFailureUnsupportedManagedField  CodecFailureReason = "unsupported_managed_field"
	CodecFailureSecretLiteralForbidden   CodecFailureReason = "secret_literal_forbidden"
	CodecFailureEquivalenceUndefined     CodecFailureReason = "equivalence_undefined"
	CodecFailurePreservationUndefined    CodecFailureReason = "preservation_undefined"
	CodecFailureCanonicalInvalid         CodecFailureReason = "canonical_contribution_invalid"
	CodecFailureOperationCanceled        CodecFailureReason = "operation_canceled"
	CodecFailureResourceLimitExceeded    CodecFailureReason = "resource_limit_exceeded"
)

// CodecFailure is a typed failure without raw host bytes or values. A root
// location is document-wide; it grants no output ownership.
type CodecFailure struct {
	reason   CodecFailureReason
	location output.KeyPath
}

func NewCodecFailure(reason CodecFailureReason, location output.KeyPath) (*CodecFailure, error) {
	failure := CodecFailure{reason: reason, location: location}
	if err := failure.Validate(); err != nil {
		return nil, err
	}
	return &failure, nil
}

func (failure CodecFailure) Validate() error {
	return failure.reason.validate()
}

func (failure CodecFailure) Error() string {
	if failure.location.IsRoot() {
		return fmt.Sprintf("aggregate codec %s", failure.reason)
	}
	return fmt.Sprintf("aggregate codec %s at %q", failure.reason, failure.location.String())
}

func (failure CodecFailure) Reason() CodecFailureReason { return failure.reason }
func (failure CodecFailure) Location() output.KeyPath   { return failure.location }

func (reason CodecFailureReason) validate() error {
	switch reason {
	case CodecFailureDocumentMalformed,
		CodecFailureDuplicateKey,
		CodecFailureSelectedShapeUnsupported,
		CodecFailureUnsupportedTransport,
		CodecFailureUnsupportedManagedField,
		CodecFailureSecretLiteralForbidden,
		CodecFailureEquivalenceUndefined,
		CodecFailurePreservationUndefined,
		CodecFailureCanonicalInvalid,
		CodecFailureOperationCanceled,
		CodecFailureResourceLimitExceeded:
		return nil
	default:
		return fmt.Errorf("unsupported aggregate codec failure reason %q", reason)
	}
}
