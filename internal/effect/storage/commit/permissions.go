package commit

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

// PermissionRestrictionPolicy interprets observations from the actual opened FD.
// The canonical protection model supplies this pure query, not storage policy.
type PermissionRestrictionPolicy func(userID, groupID, observedMode uint32) (restrictedMode uint32, expanded bool, err error)

type FilePermissionMetadata struct {
	UserID  uint32
	GroupID uint32
	Mode    uint32
}

// RootedPermissionRestriction applies one canonical protection policy under
// caller-supplied current quiescence and writer exclusion.
type RootedPermissionRestriction struct {
	path       string
	capability rootedpath.CommitCapability
	policy     PermissionRestrictionPolicy
}

// NewRootedPermissionRestriction retains capability on construction failure.
// RestrictRootedFilePermissions consumes it on every attempted operation.
func NewRootedPermissionRestriction(
	capability rootedpath.CommitCapability,
	policy PermissionRestrictionPolicy,
) (RootedPermissionRestriction, error) {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return RootedPermissionRestriction{}, err
	}
	if policy == nil {
		return RootedPermissionRestriction{}, fmt.Errorf("canonical permission restriction policy is required")
	}
	return RootedPermissionRestriction{path: path, capability: capability, policy: policy}, nil
}

func (request RootedPermissionRestriction) restriction(userID, groupID, current uint32) (uint32, bool, error) {
	desired, expanded, err := request.policy(userID, groupID, current)
	if err != nil {
		return 0, false, unsupported("recorded permission policy did not qualify current metadata", err)
	}
	if current & ^uint32(0o777) != 0 || desired & ^current != 0 || (current^desired)&0o711 != 0 || expanded != (current != desired) {
		return 0, false, unsupported("permission policy exceeded the same-FD restriction envelope", nil)
	}
	return desired, expanded, nil
}

func (request RootedPermissionRestriction) validate() error {
	if request.capability == nil || request.path == "" || request.policy == nil {
		return fmt.Errorf("rooted permission restriction is uninitialized")
	}
	return validateRootedCapability(request.path, request.capability)
}

type PermissionRestrictionResult struct {
	expanded   bool
	restricted bool
	absent     bool
}

func (result PermissionRestrictionResult) Expanded() bool { return result.expanded }

// Restricted reports an acknowledged chmod, not durable safety settlement.
// Any returned error still requires the caller to retain its protection fence.
func (result PermissionRestrictionResult) Restricted() bool { return result.restricted }

// ObservedAbsent is a read-only fact, not durable absence or safety settlement.
func (result PermissionRestrictionResult) ObservedAbsent() bool { return result.absent }
