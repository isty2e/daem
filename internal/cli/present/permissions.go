package clipresent

import (
	"context"
	"errors"
	"os"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/subprocess/executioncontext"
)

func permissionReason(cause error) string {
	if cause == nil {
		return ""
	}
	if errors.Is(cause, context.Canceled) {
		return "canceled"
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	var protection *ownership.ProtectionRefusal
	if errors.As(cause, &protection) {
		return string(protection.Kind())
	}
	var observation *executioncontext.ExecutionObservationError
	if errors.As(cause, &observation) {
		return string(observation.Kind())
	}
	var root *rootedpath.Failure
	if errors.As(cause, &root) {
		return string(root.Kind())
	}
	if errors.Is(cause, os.ErrPermission) {
		return "permission_access_denied"
	}
	return "permission_safety_unresolved"
}
