package topology

import (
	"fmt"
)

// ReasonCode is a stable machine-readable topology validation reason.
type ReasonCode string

const (
	ReasonInvalidSubject      ReasonCode = "invalid_subject"
	ReasonDuplicateSubject    ReasonCode = "duplicate_subject"
	ReasonDanglingEdge        ReasonCode = "dangling_edge"
	ReasonDuplicateEdge       ReasonCode = "duplicate_edge"
	ReasonInvalidEdgeEndpoint ReasonCode = "invalid_edge_endpoint"
	ReasonCyclicRelation      ReasonCode = "cyclic_relation"
	ReasonMissingProvider     ReasonCode = "missing_provider"
	ReasonMultipleProviders   ReasonCode = "multiple_providers"
)

// ValidationError reports an invalid structural graph state.
type ValidationError struct {
	reason  ReasonCode
	subject string
	message string
}

func validationError(reason ReasonCode, subject string, message string) *ValidationError {
	return &ValidationError{reason: reason, subject: subject, message: message}
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

// Code returns the stable topology validation reason.
func (err *ValidationError) Code() ReasonCode {
	if err == nil {
		return ""
	}
	return err.reason
}

// Subject returns the invalid graph subject when one is available.
func (err *ValidationError) Subject() string {
	if err == nil {
		return ""
	}
	return err.subject
}
