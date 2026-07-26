package realization

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

// PathPermissionPolicy identifies which regular-file permission facts are
// semantic for convergence. Directory and link projections use none.
type PathPermissionPolicy string

const (
	PathPermissionsNone            PathPermissionPolicy = "none"
	PathPermissionsExecutableClass PathPermissionPolicy = "executable-class"
	PathPermissionsExact           PathPermissionPolicy = "exact"
)

// ExactPathPermissionMode is one explicitly present complete permission-bit
// value. Its zero value means absent; an explicit 0000 value remains valid.
type ExactPathPermissionMode struct {
	fileMode os.FileMode
	present  bool
}

// NewExactPathPermissionMode constructs an explicitly present permission mode.
func NewExactPathPermissionMode(fileMode os.FileMode) (ExactPathPermissionMode, error) {
	if fileMode != fileMode.Perm() {
		return ExactPathPermissionMode{}, fmt.Errorf("exact path permission mode must contain permission bits only")
	}
	return ExactPathPermissionMode{fileMode: fileMode, present: true}, nil
}

// Validate rejects absent or forged exact permission modes.
func (mode ExactPathPermissionMode) Validate() error {
	if !mode.present {
		return fmt.Errorf("exact path permission mode is required")
	}
	if mode.fileMode != mode.fileMode.Perm() {
		return fmt.Errorf("exact path permission mode must contain permission bits only")
	}
	return nil
}

// FileMode returns the complete permission bits.
func (mode ExactPathPermissionMode) FileMode() os.FileMode { return mode.fileMode }

// Equal reports whether both values carry the same explicit permission bits.
func (mode ExactPathPermissionMode) Equal(other ExactPathPermissionMode) bool {
	return mode.present == other.present && mode.fileMode == other.fileMode
}

func (mode ExactPathPermissionMode) isZero() bool {
	return mode == (ExactPathPermissionMode{})
}

// AcceptsMode reports whether observed permission bits satisfy the policy
// after content identity has already proved the executable class.
func (policy PathPermissionPolicy) AcceptsMode(expected os.FileMode, observed os.FileMode) bool {
	switch policy {
	case PathPermissionsNone, PathPermissionsExecutableClass:
		return true
	case PathPermissionsExact:
		return expected.Perm() == observed.Perm()
	default:
		return false
	}
}

// ManagedPathProjectionInput carries one concrete managed path realization.
type ManagedPathProjectionInput struct {
	PlacementID            string
	ConsumerTargets        []target.Target
	Scope                  target.Scope
	Destination            string
	ContentKind            PathProjectionContentKind
	PlacementMode          PathProjectionMode
	PermissionPolicy       PathPermissionPolicy
	ExactPermissionMode    ExactPathPermissionMode
	AdapterContractVersion string
}

// ManagedPathProjection is one exact desired occupancy of a host path.
type ManagedPathProjection struct {
	placementID            string
	consumerTargets        []target.Target
	scope                  target.Scope
	destination            output.Portable
	contentKind            PathProjectionContentKind
	placementMode          PathProjectionMode
	permissionPolicy       PathPermissionPolicy
	exactPermissionMode    ExactPathPermissionMode
	adapterContractVersion string
}

// NewManagedPathProjection constructs a path realization with no current-state facts.
func NewManagedPathProjection(input ManagedPathProjectionInput) (RealizationSpec, error) {
	destination, err := output.Parse(input.Destination)
	if err != nil {
		return RealizationSpec{}, fmt.Errorf("managed path projection: %w", err)
	}
	consumerTargets, err := target.CanonicalSet(input.ConsumerTargets)
	if err != nil {
		return RealizationSpec{}, fmt.Errorf("managed path projection consumer targets: %w", err)
	}
	projection := ManagedPathProjection{
		placementID:            strings.TrimSpace(input.PlacementID),
		consumerTargets:        consumerTargets,
		scope:                  input.Scope,
		destination:            destination,
		contentKind:            input.ContentKind,
		placementMode:          input.PlacementMode,
		permissionPolicy:       input.PermissionPolicy,
		exactPermissionMode:    input.ExactPermissionMode,
		adapterContractVersion: strings.TrimSpace(input.AdapterContractVersion),
	}
	if err := projection.validate(); err != nil {
		return RealizationSpec{}, err
	}
	return RealizationSpec{kind: RealizationManagedPathProjection, pathProjection: &projection}, nil
}

func managedPathProjectionsEqual(left ManagedPathProjection, right ManagedPathProjection) bool {
	return left.placementID == right.placementID &&
		left.scope == right.scope &&
		left.destination == right.destination &&
		left.contentKind == right.contentKind &&
		left.placementMode == right.placementMode &&
		left.permissionPolicy == right.permissionPolicy &&
		left.exactPermissionMode.Equal(right.exactPermissionMode) &&
		left.adapterContractVersion == right.adapterContractVersion &&
		slices.Equal(left.consumerTargets, right.consumerTargets)
}

func cloneManagedPathProjection(value ManagedPathProjection) ManagedPathProjection {
	value.consumerTargets = append([]target.Target(nil), value.consumerTargets...)
	return value
}

func (projection ManagedPathProjection) PlacementID() string { return projection.placementID }
func (projection ManagedPathProjection) ConsumerTargets() []target.Target {
	return append([]target.Target(nil), projection.consumerTargets...)
}
func (projection ManagedPathProjection) Scope() target.Scope { return projection.scope }
func (projection ManagedPathProjection) Destination() string { return projection.destination.String() }

func (projection ManagedPathProjection) ContentKind() PathProjectionContentKind {
	return projection.contentKind
}

func (projection ManagedPathProjection) PlacementMode() PathProjectionMode {
	return projection.placementMode
}

func (projection ManagedPathProjection) PermissionPolicy() PathPermissionPolicy {
	return projection.permissionPolicy
}

func (projection ManagedPathProjection) ExactPermissionMode() (ExactPathPermissionMode, bool) {
	if !projection.exactPermissionMode.present {
		return ExactPathPermissionMode{}, false
	}
	return projection.exactPermissionMode, true
}

func (projection ManagedPathProjection) AdapterContractVersion() string {
	return projection.adapterContractVersion
}

// ValidateManagedPathPermissionState checks the durable permission facts for
// one managed path without reconstructing family semantics.
func ValidateManagedPathPermissionState(
	contentKind PathProjectionContentKind,
	policy PathPermissionPolicy,
	fileMode os.FileMode,
) error {
	switch contentKind {
	case PathProjectionDirectory:
		if policy != PathPermissionsNone || fileMode != 0 {
			return fmt.Errorf("managed directory state must not carry file permission facts")
		}
		return nil
	case PathProjectionFile:
	default:
		return fmt.Errorf("managed path content kind %q is unsupported", contentKind)
	}

	switch policy {
	case PathPermissionsExecutableClass:
		if fileMode != 0 {
			return fmt.Errorf("executable-class permission state must not carry an exact file mode")
		}
	case PathPermissionsExact:
		if fileMode != fileMode.Perm() {
			return fmt.Errorf("exact permission state requires permission bits only")
		}
	default:
		return fmt.Errorf("managed file permission policy %q is unsupported", policy)
	}
	return nil
}
