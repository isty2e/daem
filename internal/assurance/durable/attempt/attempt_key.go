package attempt

import (
	"fmt"

	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// DelegateAttemptKey identifies one replaceable delegate-attempt history row.
type DelegateAttemptKey struct {
	subject topology.SubjectID
	target  target.Target
	scope   target.Scope
}

// Validate rejects a zero or forged delegate-attempt key.
func (key DelegateAttemptKey) Validate() error {
	return validateAttemptIdentity(
		key.subject,
		topology.SubjectProjection,
		key.target,
		key.scope,
		"delegate attempt key",
	)
}

// HostRouteAttemptKey identifies one replaceable host-route history row.
type HostRouteAttemptKey struct {
	subject   topology.SubjectID
	target    target.Target
	scope     target.Scope
	operation lock.OperationKind
	routeID   string
}

// Validate rejects a zero or forged host-route-attempt key.
func (key HostRouteAttemptKey) Validate() error {
	if err := validateAttemptIdentity(
		key.subject,
		topology.SubjectHostRelation,
		key.target,
		key.scope,
		"host route attempt key",
	); err != nil {
		return err
	}
	if _, err := lock.ParseOperationKind(string(key.operation)); err != nil {
		return fmt.Errorf("host route attempt key operation: %w", err)
	}
	return validateCanonicalIdentityText(key.routeID, "host route attempt key route id")
}
