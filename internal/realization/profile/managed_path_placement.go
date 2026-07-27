package profile

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
)

// ManagedPathPlacement is one static physical placement shared by one or more
// target consumers. It contains no desired entity, current path, or effect fact.
type ManagedPathPlacement struct {
	id               string
	consumerTargets  []target.Target
	resourceKind     entity.Kind
	scope            target.Scope
	root             output.Destination
	defaultPlacement bool
	contentKind      realization.PathProjectionContentKind
	permissionPolicy realization.PathPermissionPolicy
}

// ManagedPathPlacementInput carries one placement-axis fact and no operation route.
type ManagedPathPlacementInput struct {
	ID               string
	ConsumerTargets  []target.Target
	ResourceKind     entity.Kind
	Scope            target.Scope
	Root             string
	DefaultPlacement bool
	ContentKind      realization.PathProjectionContentKind
}

// NewManagedPathPlacement constructs one canonical placement fact.
func NewManagedPathPlacement(input ManagedPathPlacementInput) (ManagedPathPlacement, error) {
	consumerTargets, err := target.CanonicalSet(input.ConsumerTargets)
	if err != nil {
		return ManagedPathPlacement{}, fmt.Errorf("managed path placement consumer targets: %w", err)
	}
	root, err := output.Parse(strings.TrimSpace(input.Root))
	if err != nil {
		return ManagedPathPlacement{}, fmt.Errorf("managed path placement root: %w", err)
	}
	placement := ManagedPathPlacement{
		id:               strings.TrimSpace(input.ID),
		consumerTargets:  consumerTargets,
		resourceKind:     input.ResourceKind,
		scope:            input.Scope,
		root:             root,
		defaultPlacement: input.DefaultPlacement,
		contentKind:      input.ContentKind,
		permissionPolicy: managedPathPermissionPolicy(input.ContentKind),
	}
	if err := placement.validate(); err != nil {
		return ManagedPathPlacement{}, err
	}
	return placement, nil
}

// ManagedPathPlacementsFor selects and coalesces default path placements for
// one resource kind, scope, and target set.
func ManagedPathPlacementsFor(
	resourceKind entity.Kind,
	scope target.Scope,
	targets []target.Target,
) ([]ManagedPathPlacement, error) {
	if _, err := target.ParseScope(string(scope)); err != nil {
		return nil, err
	}
	canonicalTargets, err := target.CanonicalSet(targets)
	if err != nil {
		return nil, fmt.Errorf("managed path placement targets: %w", err)
	}
	if len(canonicalTargets) == 0 {
		return nil, fmt.Errorf("managed path placement requires at least one target")
	}

	placements := make(map[string]ManagedPathPlacement, len(canonicalTargets))
	placementIDsByAddress := make(map[managedPathPlacementAddress]string, len(canonicalTargets))
	for _, selectedTarget := range canonicalTargets {
		candidate, err := Profile(selectedTarget).DefaultPlacement(resourceKind, scope)
		if err != nil {
			return nil, err
		}
		if err := candidate.validate(); err != nil {
			return nil, fmt.Errorf("target %q %s placement: %w", selectedTarget, resourceKind, err)
		}

		if err := addManagedPathPlacement(placements, placementIDsByAddress, candidate); err != nil {
			return nil, err
		}
	}

	result := make([]ManagedPathPlacement, 0, len(placements))
	for _, placement := range placements {
		result = append(result, placement)
	}
	sort.Slice(result, func(left int, right int) bool { return result[left].id < result[right].id })
	return result, nil
}

// MergeManagedPathPlacements coalesces consumers only when both values name
// the same physical placement.
func MergeManagedPathPlacements(
	left ManagedPathPlacement,
	right ManagedPathPlacement,
) (ManagedPathPlacement, error) {
	if err := left.validate(); err != nil {
		return ManagedPathPlacement{}, err
	}
	if err := right.validate(); err != nil {
		return ManagedPathPlacement{}, err
	}
	if !left.sameStaticPlacement(right) {
		return ManagedPathPlacement{}, fmt.Errorf("managed path placement id %q has conflicting static facts", left.id)
	}
	consumerTargets, err := target.CanonicalSet(append(left.consumerTargets, right.consumerTargets...))
	if err != nil {
		return ManagedPathPlacement{}, fmt.Errorf("merge managed path placement consumer targets: %w", err)
	}
	left.consumerTargets = consumerTargets
	return left, nil
}

type managedPathPlacementAddress struct {
	scope target.Scope
	root  output.Destination
}

func addManagedPathPlacement(
	placements map[string]ManagedPathPlacement,
	placementIDsByAddress map[managedPathPlacementAddress]string,
	candidate ManagedPathPlacement,
) error {
	address := managedPathPlacementAddress{scope: candidate.scope, root: candidate.root}
	if existingID, occupied := placementIDsByAddress[address]; occupied && existingID != candidate.id {
		return fmt.Errorf(
			"managed path placement ids %q and %q claim the same %s root %q",
			existingID,
			candidate.id,
			candidate.scope,
			candidate.root,
		)
	}

	existing, shared := placements[candidate.id]
	if shared {
		if !existing.sameStaticPlacement(candidate) {
			return fmt.Errorf("managed path placement id %q has conflicting static facts", candidate.id)
		}
		consumerTargets, err := target.CanonicalSet(append(existing.consumerTargets, candidate.consumerTargets...))
		if err != nil {
			return fmt.Errorf("coalesce managed path placement consumer targets: %w", err)
		}
		existing.consumerTargets = consumerTargets
		placements[candidate.id] = existing
		placementIDsByAddress[address] = candidate.id
		return nil
	}

	placements[candidate.id] = candidate
	placementIDsByAddress[address] = candidate.id
	return nil
}

// ID returns the stable physical placement identity.
func (placement ManagedPathPlacement) ID() string { return placement.id }

// ConsumerTargets returns the canonical target set sharing this placement.
func (placement ManagedPathPlacement) ConsumerTargets() []target.Target {
	return append([]target.Target(nil), placement.consumerTargets...)
}

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

// Default reports whether this placement is selected when no destination is authored.
func (placement ManagedPathPlacement) Default() bool { return placement.defaultPlacement }

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

// Realize constructs one exact occupancy for this placement.
func (placement ManagedPathPlacement) Realize(
	destination output.Destination,
	mode realization.PathProjectionMode,
	writeRoute OperationRoute,
) (realization.RealizationSpec, error) {
	if err := placement.validate(); err != nil {
		return realization.RealizationSpec{}, err
	}
	if err := validatePlacementRoute(placement, writeRoute, OperationWrite); err != nil {
		return realization.RealizationSpec{}, err
	}
	switch placement.contentKind {
	case realization.PathProjectionFile:
		if destination != placement.root {
			return realization.RealizationSpec{}, fmt.Errorf(
				"managed file destination %q does not match placement %q path %q",
				destination,
				placement.id,
				placement.root,
			)
		}
	case realization.PathProjectionDirectory:
		if _, err := placement.ChildName(destination); err != nil {
			return realization.RealizationSpec{}, err
		}
	}
	return realization.NewManagedPathProjection(realization.ManagedPathProjectionInput{
		PlacementID:            placement.id,
		ConsumerTargets:        placement.consumerTargets,
		Scope:                  placement.scope,
		Destination:            destination,
		ContentKind:            placement.contentKind,
		PlacementMode:          mode,
		PermissionPolicy:       placement.permissionPolicyFor(mode),
		AdapterContractVersion: writeRoute.AdapterContractVersion(),
	})
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
	if err := validateTargetSet(placement.consumerTargets); err != nil {
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
		placement.defaultPlacement == other.defaultPlacement &&
		placement.contentKind == other.contentKind &&
		placement.permissionPolicy == other.permissionPolicy
}

// ManagedFilePlacementFor selects one exact admitted file placement for a
// target. Discovery and runtime rows are never write authority.
func ManagedFilePlacementFor(
	resourceKind entity.Kind,
	selectedTarget target.Target,
	scope target.Scope,
	destination output.Destination,
) (ManagedPathPlacement, error) {
	if _, err := target.ParseTarget(string(selectedTarget)); err != nil {
		return ManagedPathPlacement{}, err
	}
	if _, err := target.ParseScope(string(scope)); err != nil {
		return ManagedPathPlacement{}, err
	}
	if err := destination.Validate(); err != nil {
		return ManagedPathPlacement{}, fmt.Errorf(
			"%s target %q scope %q destination %q is not an admitted file placement: %w",
			resourceKind,
			selectedTarget,
			scope,
			destination,
			err,
		)
	}
	if err := destination.ValidateScope(scope); err != nil {
		return ManagedPathPlacement{}, fmt.Errorf(
			"%s target %q scope %q destination %q is not an admitted file placement: %w",
			resourceKind,
			selectedTarget,
			scope,
			destination,
			err,
		)
	}

	var selected ManagedPathPlacement
	matches := 0
	for _, placement := range Profile(selectedTarget).Placements(resourceKind, scope) {
		if placement.root != destination || placement.contentKind != realization.PathProjectionFile {
			continue
		}
		selected = placement
		matches++
	}
	if matches == 0 {
		return ManagedPathPlacement{}, fmt.Errorf(
			"%s target %q scope %q destination %q is not an admitted file placement",
			resourceKind,
			selectedTarget,
			scope,
			destination,
		)
	}
	if matches != 1 {
		return ManagedPathPlacement{}, fmt.Errorf(
			"%s target %q scope %q destination %q has multiple admitted file placements",
			resourceKind,
			selectedTarget,
			scope,
			destination,
		)
	}

	placement := selected
	if err := placement.validate(); err != nil {
		return ManagedPathPlacement{}, fmt.Errorf("target %q %s file placement: %w", selectedTarget, resourceKind, err)
	}
	return placement, nil
}

// ManagedFilePlacementForRelativePath resolves one authored scope-relative
// file path and selects its exact admitted placement.
func ManagedFilePlacementForRelativePath(
	resourceKind entity.Kind,
	selectedTarget target.Target,
	scope target.Scope,
	relativePath string,
) (ManagedPathPlacement, error) {
	defaultPlacement, err := Profile(selectedTarget).DefaultPlacement(resourceKind, scope)
	if err != nil {
		return ManagedPathPlacement{}, err
	}
	if relativePath == "" {
		return defaultPlacement, nil
	}
	trimmed := strings.TrimSpace(relativePath)
	if trimmed == "" || trimmed != relativePath {
		return ManagedPathPlacement{}, fmt.Errorf("relative file path must be non-empty and trimmed")
	}
	if strings.Contains(trimmed, "\\") || strings.HasPrefix(trimmed, "~") || path.IsAbs(trimmed) {
		return ManagedPathPlacement{}, fmt.Errorf("relative file path %q must be slash-separated and relative to the target scope root", relativePath)
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != trimmed {
		return ManagedPathPlacement{}, fmt.Errorf("relative file path %q must be canonical and stay inside the target scope root", relativePath)
	}
	value := cleaned
	if scope == target.ScopeGlobal {
		value = path.Join(path.Dir(defaultPlacement.root.String()), cleaned)
	}
	destination, err := output.Parse(value)
	if err != nil {
		return ManagedPathPlacement{}, err
	}
	return ManagedFilePlacementFor(resourceKind, selectedTarget, scope, destination)
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
