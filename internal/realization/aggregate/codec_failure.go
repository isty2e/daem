package aggregate

import (
	"fmt"
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
)

// CodecFailure is a typed failure without raw host bytes or secret values.
type CodecFailure struct {
	reason      CodecFailureReason
	contentPath ContentPath
}

// NewCodecFailure constructs a redaction-safe typed codec error.
func NewCodecFailure(reason CodecFailureReason, contentPath ContentPath) (*CodecFailure, error) {
	if !reason.valid() {
		return nil, fmt.Errorf("unsupported aggregate codec failure reason %q", reason)
	}
	if contentPath != "" {
		if _, err := ParseContentPath(string(contentPath)); err != nil {
			return nil, err
		}
	}
	return &CodecFailure{reason: reason, contentPath: contentPath}, nil
}

func (failure CodecFailure) Error() string {
	if failure.contentPath == "" {
		return fmt.Sprintf("aggregate codec %s", failure.reason)
	}
	return fmt.Sprintf("aggregate codec %s at %s", failure.reason, failure.contentPath)
}

func (failure CodecFailure) Reason() CodecFailureReason { return failure.reason }
func (failure CodecFailure) ContentPath() ContentPath   { return failure.contentPath }

func (reason CodecFailureReason) valid() bool {
	switch reason {
	case CodecFailureDocumentMalformed,
		CodecFailureDuplicateKey,
		CodecFailureSelectedShapeUnsupported,
		CodecFailureUnsupportedTransport,
		CodecFailureUnsupportedManagedField,
		CodecFailureSecretLiteralForbidden,
		CodecFailureEquivalenceUndefined,
		CodecFailurePreservationUndefined,
		CodecFailureCanonicalInvalid:
		return true
	default:
		return false
	}
}
