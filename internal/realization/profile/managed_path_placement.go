package profile

import (
	"fmt"
	"path"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
)

// ManagedPathPlacement is one static physical placement. It owns no target
// admission, default-selection, desired entity, current path, or effect fact.
type ManagedPathPlacement struct {
	id               string
	resourceKind     entity.Kind
	scope            target.Scope
	root             output.Destination
	contentKind      realization.PathProjectionContentKind
	permissionPolicy realization.PathPermissionPolicy
}

// ManagedPathPlacementInput carries one placement-axis fact and no operation route.
type ManagedPathPlacementInput struct {
	ID           string
	ResourceKind entity.Kind
	Scope        target.Scope
	Root         string
	ContentKind  realization.PathProjectionContentKind
}

// NewManagedPathPlacement constructs one canonical placement fact.
func NewManagedPathPlacement(input ManagedPathPlacementInput) (ManagedPathPlacement, error) {
	root, err := output.Parse(strings.TrimSpace(input.Root))
	if err != nil {
		return ManagedPathPlacement{}, fmt.Errorf("managed path placement root: %w", err)
	}
	placement := ManagedPathPlacement{
		id:               strings.TrimSpace(input.ID),
		resourceKind:     input.ResourceKind,
		scope:            input.Scope,
		root:             root,
		contentKind:      input.ContentKind,
		permissionPolicy: managedPathPermissionPolicy(input.ContentKind),
	}
	if err := placement.validate(); err != nil {
		return ManagedPathPlacement{}, err
	}
	return placement, nil
}

// PlacementAdmission states that one target may select one static placement.
// Default selection belongs to this target-relative fact, not to the placement.
type PlacementAdmission struct {
	selectedTarget target.Target
	placementID    string
	defaultChoice  bool
}

// NewPlacementAdmission constructs one canonical target-placement admission.
func NewPlacementAdmission(
	selectedTarget target.Target,
	placementID string,
	defaultChoice bool,
) (PlacementAdmission, error) {
	parsedTarget, err := target.ParseTarget(string(selectedTarget))
	if err != nil {
		return PlacementAdmission{}, err
	}
	admission := PlacementAdmission{
		selectedTarget: parsedTarget,
		placementID:    strings.TrimSpace(placementID),
		defaultChoice:  defaultChoice,
	}
	if err := admission.Validate(); err != nil {
		return PlacementAdmission{}, err
	}
	return admission, nil
}

// Target returns the target that admits the placement.
func (admission PlacementAdmission) Target() target.Target { return admission.selectedTarget }

// PlacementID returns the admitted static placement identity.
func (admission PlacementAdmission) PlacementID() string { return admission.placementID }

// Default reports whether omission selects this placement for the target.
func (admission PlacementAdmission) Default() bool { return admission.defaultChoice }

// Validate rejects forged or partially initialized admissions.
func (admission PlacementAdmission) Validate() error {
	if _, err := target.ParseTarget(string(admission.selectedTarget)); err != nil {
		return err
	}
	return validateProfileToken("placement admission id", admission.placementID)
}

// ID returns the stable physical placement identity.
func (placement ManagedPathPlacement) ID() string { return placement.id }

// Scope returns the placement locality.
func (placement ManagedPathPlacement) Scope() target.Scope { return placement.scope }

// ResourceKind returns the desired family placed at this root.
func (placement ManagedPathPlacement) ResourceKind() entity.Kind { return placement.resourceKind }

// ContentKind returns the exact shape admitted below this placement.
func (placement ManagedPathPlacement) ContentKind() realization.PathProjectionContentKind {
	return placement.contentKind
}

// Root returns the portable placement root.
func (placement ManagedPathPlacement) Root() output.Destination { return placement.root }

// ChildDestination returns the canonical child path below this placement root.
func (placement ManagedPathPlacement) ChildDestination(component string) (output.Destination, error) {
	if err := placement.validate(); err != nil {
		return output.Destination{}, err
	}
	if placement.contentKind != realization.PathProjectionDirectory {
		return output.Destination{}, fmt.Errorf("managed path placement %q does not admit child destinations", placement.id)
	}
	if strings.TrimSpace(component) == "" || strings.TrimSpace(component) != component ||
		component == "." || component == ".." || strings.ContainsAny(component, `/\`) ||
		path.Clean(component) != component {
		return output.Destination{}, fmt.Errorf("managed path child %q must be one canonical path component", component)
	}
	destination, err := output.Parse(path.Join(placement.root.String(), component))
	if err != nil {
		return output.Destination{}, err
	}
	if err := destination.ValidateScope(placement.scope); err != nil {
		return output.Destination{}, err
	}
	return destination, nil
}

// ChildName returns the one canonical child represented by destination.
func (placement ManagedPathPlacement) ChildName(destination output.Destination) (string, error) {
	if err := placement.validate(); err != nil {
		return "", err
	}
	if placement.contentKind != realization.PathProjectionDirectory {
		return "", fmt.Errorf("managed path placement %q does not admit child destinations", placement.id)
	}
	destinationText := destination.String()
	prefix := placement.root.String() + "/"
	if !strings.HasPrefix(destinationText, prefix) {
		return "", fmt.Errorf("managed path destination %q is outside placement %q", destination, placement.id)
	}
	child := strings.TrimPrefix(destinationText, prefix)
	canonical, err := placement.ChildDestination(child)
	if err != nil {
		return "", err
	}
	if canonical != destination {
		return "", fmt.Errorf("managed path destination %q is not canonical", destination)
	}
	return child, nil
}

func managedPathPermissionPolicy(contentKind realization.PathProjectionContentKind) realization.PathPermissionPolicy {
	if contentKind == realization.PathProjectionFile {
		return realization.PathPermissionsExecutableClass
	}
	return realization.PathPermissionsNone
}

func (placement ManagedPathPlacement) permissionPolicyFor(mode realization.PathProjectionMode) realization.PathPermissionPolicy {
	if mode != realization.PathProjectionCopy {
		return realization.PathPermissionsNone
	}
	return placement.permissionPolicy
}

func (placement ManagedPathPlacement) validate() error {
	if err := validateProfileToken("placement id", placement.id); err != nil {
		return err
	}
	if _, err := entity.ParseKind(string(placement.resourceKind)); err != nil {
		return err
	}
	if _, err := target.ParseScope(string(placement.scope)); err != nil {
		return err
	}
	if err := placement.root.ValidateScope(placement.scope); err != nil {
		return err
	}
	if placement.contentKind != realization.PathProjectionFile && placement.contentKind != realization.PathProjectionDirectory {
		return fmt.Errorf("managed path placement content kind %q is unsupported", placement.contentKind)
	}
	if placement.permissionPolicy != managedPathPermissionPolicy(placement.contentKind) {
		return fmt.Errorf("managed path placement permission policy %q does not match content kind", placement.permissionPolicy)
	}
	return nil
}

func (placement ManagedPathPlacement) sameStaticPlacement(other ManagedPathPlacement) bool {
	return placement.id == other.id &&
		placement.resourceKind == other.resourceKind &&
		placement.scope == other.scope &&
		placement.root == other.root &&
		placement.contentKind == other.contentKind &&
		placement.permissionPolicy == other.permissionPolicy
}

func validatePlacementRoute(
	placement ManagedPathPlacement,
	route OperationRoute,
	operation Operation,
) error {
	if err := route.Validate(); err != nil {
		return err
	}
	if !route.Correlates(placement.resourceKind, placement.id, operation) {
		return fmt.Errorf(
			"operation route %q/%q does not correlate with %s placement %q",
			route.Operation(),
			route.RouteID(),
			placement.resourceKind,
			placement.id,
		)
	}
	return nil
}
