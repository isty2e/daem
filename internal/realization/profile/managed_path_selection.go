package profile

import (
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
)

// SelectedManagedPathPlacement pairs one static placement with the exact
// canonical target set consuming it in one desired projection.
type SelectedManagedPathPlacement struct {
	placement       ManagedPathPlacement
	consumerTargets []target.Target
}

func newSelectedManagedPathPlacement(
	placement ManagedPathPlacement,
	consumerTargets []target.Target,
) (SelectedManagedPathPlacement, error) {
	if err := placement.validate(); err != nil {
		return SelectedManagedPathPlacement{}, err
	}
	canonicalTargets, err := target.CanonicalSet(consumerTargets)
	if err != nil {
		return SelectedManagedPathPlacement{}, fmt.Errorf("selected managed path placement consumers: %w", err)
	}
	selected := SelectedManagedPathPlacement{
		placement:       placement,
		consumerTargets: canonicalTargets,
	}
	if err := selected.validate(); err != nil {
		return SelectedManagedPathPlacement{}, err
	}
	return selected, nil
}

// ManagedPathPlacementsForSelections selects and coalesces one admitted
// placement per target. Missing selection entries use that target's default.
func ManagedPathPlacementsForSelections(
	resourceKind entity.Kind,
	scope target.Scope,
	targets []target.Target,
	requestedRoots map[target.Target]string,
) ([]SelectedManagedPathPlacement, error) {
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
	for requestedTarget := range requestedRoots {
		parsedTarget, err := target.ParseTarget(string(requestedTarget))
		if err != nil {
			return nil, fmt.Errorf("managed path placement selection target %q: %w", requestedTarget, err)
		}
		if !slices.Contains(canonicalTargets, parsedTarget) {
			return nil, fmt.Errorf(
				"managed path placement selection target %q is not a consumer",
				requestedTarget,
			)
		}
	}

	placements := make(map[string]SelectedManagedPathPlacement, len(canonicalTargets))
	placementIDsByAddress := make(map[managedPathPlacementAddress]string, len(canonicalTargets))
	for _, selectedTarget := range canonicalTargets {
		selectedProfile := Profile(selectedTarget)
		requestedRoot, explicit := requestedRoots[selectedTarget]
		candidate, err := selectedProfile.DefaultPlacement(resourceKind, scope)
		if err != nil {
			return nil, err
		}
		if explicit {
			selected, admitted := selectedProfile.PlacementAt(resourceKind, scope, requestedRoot)
			if !admitted {
				return nil, fmt.Errorf(
					"%s target %q scope %q placement %q is not admitted; admitted roots: %s",
					resourceKind,
					selectedTarget,
					scope,
					requestedRoot,
					strings.Join(managedPathPlacementRoots(selectedProfile, resourceKind, scope), ", "),
				)
			}
			candidate = selected
		}
		if err := candidate.validate(); err != nil {
			return nil, fmt.Errorf("target %q %s placement: %w", selectedTarget, resourceKind, err)
		}

		if err := addManagedPathPlacement(placements, placementIDsByAddress, candidate); err != nil {
			return nil, err
		}
	}

	result := make([]SelectedManagedPathPlacement, 0, len(placements))
	for _, placement := range placements {
		result = append(result, placement)
	}
	sort.Slice(result, func(left int, right int) bool { return result[left].ID() < result[right].ID() })
	return result, nil
}

// ManagedPathPlacementForConsumers selects one exact persisted placement only
// when every consumer target currently admits that placement identity.
func ManagedPathPlacementForConsumers(
	resourceKind entity.Kind,
	scope target.Scope,
	placementID string,
	consumerTargets []target.Target,
) (SelectedManagedPathPlacement, error) {
	if _, err := target.ParseScope(string(scope)); err != nil {
		return SelectedManagedPathPlacement{}, err
	}
	canonicalTargets, err := target.CanonicalSet(consumerTargets)
	if err != nil {
		return SelectedManagedPathPlacement{}, fmt.Errorf("managed path placement consumers: %w", err)
	}
	if len(canonicalTargets) == 0 {
		return SelectedManagedPathPlacement{}, fmt.Errorf("managed path placement requires at least one consumer")
	}

	var selected SelectedManagedPathPlacement
	for index, consumer := range canonicalTargets {
		candidate, admitted := Profile(consumer).placementByID(resourceKind, scope, placementID)
		if !admitted {
			return SelectedManagedPathPlacement{}, fmt.Errorf(
				"%s placement %q is not selected by its consumers: target %q scope %q does not admit it",
				resourceKind,
				placementID,
				consumer,
				scope,
			)
		}
		if index == 0 {
			selected = candidate
			continue
		}
		selected, err = MergeManagedPathPlacements(selected, candidate)
		if err != nil {
			return SelectedManagedPathPlacement{}, err
		}
	}
	return selected, nil
}

func (profile TargetProfile) placementByID(
	resourceKind entity.Kind,
	scope target.Scope,
	placementID string,
) (SelectedManagedPathPlacement, bool) {
	var selected ManagedPathPlacement
	matches := 0
	for _, placement := range profile.Placements(resourceKind, scope) {
		if placement.ID() != placementID {
			continue
		}
		selected = placement
		matches++
	}
	if matches != 1 {
		return SelectedManagedPathPlacement{}, false
	}
	result, err := newSelectedManagedPathPlacement(selected, []target.Target{profile.selectedTarget})
	return result, err == nil
}

func managedPathPlacementRoots(
	profile TargetProfile,
	resourceKind entity.Kind,
	scope target.Scope,
) []string {
	placements := profile.Placements(resourceKind, scope)
	roots := make([]string, 0, len(placements))
	for _, placement := range placements {
		roots = append(roots, placement.Root().String())
	}
	sort.Strings(roots)
	return roots
}

// MergeManagedPathPlacements coalesces consumers only when both values name
// the same physical placement.
func MergeManagedPathPlacements(
	left SelectedManagedPathPlacement,
	right SelectedManagedPathPlacement,
) (SelectedManagedPathPlacement, error) {
	if err := left.validate(); err != nil {
		return SelectedManagedPathPlacement{}, err
	}
	if err := right.validate(); err != nil {
		return SelectedManagedPathPlacement{}, err
	}
	if !left.placement.sameStaticPlacement(right.placement) {
		return SelectedManagedPathPlacement{}, fmt.Errorf(
			"managed path placement id %q has conflicting static facts",
			left.ID(),
		)
	}
	consumerTargets, err := target.CanonicalSet(append(left.consumerTargets, right.consumerTargets...))
	if err != nil {
		return SelectedManagedPathPlacement{}, fmt.Errorf("merge managed path placement consumer targets: %w", err)
	}
	left.consumerTargets = consumerTargets
	return left, nil
}

type managedPathPlacementAddress struct {
	scope target.Scope
	root  output.Destination
}

func addManagedPathPlacement(
	placements map[string]SelectedManagedPathPlacement,
	placementIDsByAddress map[managedPathPlacementAddress]string,
	candidate SelectedManagedPathPlacement,
) error {
	address := managedPathPlacementAddress{scope: candidate.Scope(), root: candidate.Root()}
	if existingID, occupied := placementIDsByAddress[address]; occupied && existingID != candidate.ID() {
		return fmt.Errorf(
			"managed path placement ids %q and %q claim the same %s root %q",
			existingID,
			candidate.ID(),
			candidate.Scope(),
			candidate.Root(),
		)
	}

	existing, shared := placements[candidate.ID()]
	if shared {
		if !existing.placement.sameStaticPlacement(candidate.placement) {
			return fmt.Errorf("managed path placement id %q has conflicting static facts", candidate.ID())
		}
		consumerTargets, err := target.CanonicalSet(append(existing.consumerTargets, candidate.consumerTargets...))
		if err != nil {
			return fmt.Errorf("coalesce managed path placement consumer targets: %w", err)
		}
		existing.consumerTargets = consumerTargets
		placements[candidate.ID()] = existing
		placementIDsByAddress[address] = candidate.ID()
		return nil
	}

	placements[candidate.ID()] = candidate
	placementIDsByAddress[address] = candidate.ID()
	return nil
}

// ID returns the selected static placement identity.
func (selected SelectedManagedPathPlacement) ID() string { return selected.placement.ID() }

// ConsumerTargets returns the canonical target set sharing this projection.
func (selected SelectedManagedPathPlacement) ConsumerTargets() []target.Target {
	return append([]target.Target(nil), selected.consumerTargets...)
}

// Scope returns the selected placement locality.
func (selected SelectedManagedPathPlacement) Scope() target.Scope {
	return selected.placement.Scope()
}

// ResourceKind returns the desired family selected at this placement.
func (selected SelectedManagedPathPlacement) ResourceKind() entity.Kind {
	return selected.placement.ResourceKind()
}

// ContentKind returns the selected placement content shape.
func (selected SelectedManagedPathPlacement) ContentKind() realization.PathProjectionContentKind {
	return selected.placement.ContentKind()
}

// Root returns the selected portable placement root.
func (selected SelectedManagedPathPlacement) Root() output.Destination {
	return selected.placement.Root()
}

// ChildDestination returns the canonical child path below the selected root.
func (selected SelectedManagedPathPlacement) ChildDestination(component string) (output.Destination, error) {
	return selected.placement.ChildDestination(component)
}

// ChildName returns the canonical child represented by destination.
func (selected SelectedManagedPathPlacement) ChildName(destination output.Destination) (string, error) {
	return selected.placement.ChildName(destination)
}

// Realize constructs one exact occupancy for this selected placement.
func (selected SelectedManagedPathPlacement) Realize(
	destination output.Destination,
	mode realization.PathProjectionMode,
	writeRoute OperationRoute,
) (realization.RealizationSpec, error) {
	if err := selected.validate(); err != nil {
		return realization.RealizationSpec{}, err
	}
	if err := validatePlacementRoute(selected.placement, writeRoute, OperationWrite); err != nil {
		return realization.RealizationSpec{}, err
	}
	placement := selected.placement
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
		ConsumerTargets:        selected.consumerTargets,
		Scope:                  placement.scope,
		Destination:            destination,
		ContentKind:            placement.contentKind,
		PlacementMode:          mode,
		PermissionPolicy:       placement.permissionPolicyFor(mode),
		AdapterContractVersion: writeRoute.AdapterContractVersion(),
	})
}

func (selected SelectedManagedPathPlacement) validate() error {
	if err := selected.placement.validate(); err != nil {
		return err
	}
	return validateTargetSet(selected.consumerTargets)
}

// ManagedFilePlacementFor selects one exact admitted file placement for a
// target. Discovery and runtime rows are never write authority.
func ManagedFilePlacementFor(
	resourceKind entity.Kind,
	selectedTarget target.Target,
	scope target.Scope,
	destination output.Destination,
) (SelectedManagedPathPlacement, error) {
	if _, err := target.ParseTarget(string(selectedTarget)); err != nil {
		return SelectedManagedPathPlacement{}, err
	}
	if _, err := target.ParseScope(string(scope)); err != nil {
		return SelectedManagedPathPlacement{}, err
	}
	if err := destination.Validate(); err != nil {
		return SelectedManagedPathPlacement{}, fmt.Errorf(
			"%s target %q scope %q destination %q is not an admitted file placement: %w",
			resourceKind,
			selectedTarget,
			scope,
			destination,
			err,
		)
	}
	if err := destination.ValidateScope(scope); err != nil {
		return SelectedManagedPathPlacement{}, fmt.Errorf(
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
		return SelectedManagedPathPlacement{}, fmt.Errorf(
			"%s target %q scope %q destination %q is not an admitted file placement",
			resourceKind,
			selectedTarget,
			scope,
			destination,
		)
	}
	if matches != 1 {
		return SelectedManagedPathPlacement{}, fmt.Errorf(
			"%s target %q scope %q destination %q has multiple admitted file placements",
			resourceKind,
			selectedTarget,
			scope,
			destination,
		)
	}

	placement, err := newSelectedManagedPathPlacement(selected, []target.Target{selectedTarget})
	if err != nil {
		return SelectedManagedPathPlacement{}, fmt.Errorf(
			"target %q %s file placement: %w",
			selectedTarget,
			resourceKind,
			err,
		)
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
) (SelectedManagedPathPlacement, error) {
	defaultPlacement, err := Profile(selectedTarget).DefaultPlacement(resourceKind, scope)
	if err != nil {
		return SelectedManagedPathPlacement{}, err
	}
	if relativePath == "" {
		return defaultPlacement, nil
	}
	trimmed := strings.TrimSpace(relativePath)
	if trimmed == "" || trimmed != relativePath {
		return SelectedManagedPathPlacement{}, fmt.Errorf("relative file path must be non-empty and trimmed")
	}
	if strings.Contains(trimmed, "\\") || strings.HasPrefix(trimmed, "~") || path.IsAbs(trimmed) {
		return SelectedManagedPathPlacement{}, fmt.Errorf("relative file path %q must be slash-separated and relative to the target scope root", relativePath)
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != trimmed {
		return SelectedManagedPathPlacement{}, fmt.Errorf("relative file path %q must be canonical and stay inside the target scope root", relativePath)
	}
	value := cleaned
	if scope == target.ScopeGlobal {
		value = path.Join(path.Dir(defaultPlacement.Root().String()), cleaned)
	}
	destination, err := output.Parse(value)
	if err != nil {
		return SelectedManagedPathPlacement{}, err
	}
	return ManagedFilePlacementFor(resourceKind, selectedTarget, scope, destination)
}
