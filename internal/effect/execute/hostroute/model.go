package hostroute

import (
	"fmt"

	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/topology"
)

// ReasonCode classifies why a host route command could not be constructed.
type ReasonCode string

const (
	ReasonUnsupportedAction      ReasonCode = "unsupported_action"
	ReasonMissingWorkDir         ReasonCode = "missing_workdir"
	ReasonLockedSubjectMissing   ReasonCode = "locked_subject_missing"
	ReasonLockedSubjectAmbiguous ReasonCode = "locked_subject_ambiguous"
	ReasonUnsupportedRoute       ReasonCode = "unsupported_route"
	ReasonInvalidLockedRecord    ReasonCode = "invalid_locked_record"
	ReasonRouteRequestMismatch   ReasonCode = "route_request_mismatch"
	ReasonTargetMismatch         ReasonCode = "target_mismatch"
	ReasonScopeMismatch          ReasonCode = "scope_mismatch"
	ReasonRelationKeyMismatch    ReasonCode = "relation_subject_key_mismatch"
	ReasonUnsupportedScope       ReasonCode = "unsupported_scope"
	ReasonUnsupportedSource      ReasonCode = "unsupported_source"
)

// BuildInput carries already-planned route facts into host-native request
// construction. It does not grant authority to execute the returned request.
type BuildInput struct {
	Action   reconciliation.RelationAction
	Lockfile lock.File
	WorkDir  string
}

// OperationBuildInput carries one admitted locked carrier contract and exact
// operation into host-native request construction. It grants no execution
// authority.
type OperationBuildInput struct {
	Contract  lock.LockedSubjectContract
	Operation lock.OperationKind
	WorkDir   string
}

// Command is a structured host command request plus the route identity it
// implements.
type Command struct {
	subject       topology.SubjectID
	routeRequest  realizationdelegate.Request
	attempt       subprocess.CommandAttemptRequest
	disclosure    Disclosure
	hasDisclosure bool
}

// Subject returns the locked subject selected by the route action.
func (command Command) Subject() topology.SubjectID {
	return command.subject
}

// RouteRequest returns the locked route request identity lowered by the adapter.
func (command Command) RouteRequest() realizationdelegate.Request {
	return command.routeRequest
}

// AttemptRequest returns a defensive copy of the structured command attempt.
func (command Command) AttemptRequest() subprocess.CommandAttemptRequest {
	attempt := command.attempt
	attempt.Args = append([]string(nil), command.attempt.Args...)
	attempt.EnvRefs = append([]subprocess.CommandEnvRef(nil), command.attempt.EnvRefs...)
	return attempt
}

// Disclosure returns the adapter-owned effect envelope when this operation
// supplies one.
func (command Command) Disclosure() (Disclosure, bool) {
	return command.disclosure, command.hasDisclosure
}

// ValidationError is a stable pre-launch host route construction diagnostic.
type ValidationError struct {
	code    ReasonCode
	subject topology.SubjectID
	detail  string
}

func newValidationError(
	code ReasonCode,
	subject topology.SubjectID,
	format string,
	args ...any,
) *ValidationError {
	return &ValidationError{
		code:    code,
		subject: subject,
		detail:  fmt.Sprintf(format, args...),
	}
}

// Error returns a stable diagnostic suitable for logs and tests.
func (err *ValidationError) Error() string {
	if err == nil {
		return ""
	}
	if err.subject.IsZero() {
		return fmt.Sprintf("%s: %s", err.code, err.detail)
	}
	return fmt.Sprintf(
		"%s: subject=%s/%s/%s: %s",
		err.code,
		err.subject.Kind(),
		err.subject.Namespace(),
		err.subject.Key(),
		err.detail,
	)
}

// Code returns the stable reason code.
func (err *ValidationError) Code() ReasonCode {
	if err == nil {
		return ""
	}
	return err.code
}

// Subject returns the locked subject associated with this diagnostic, if any.
func (err *ValidationError) Subject() topology.SubjectID {
	if err == nil {
		return topology.SubjectID{}
	}
	return err.subject
}
