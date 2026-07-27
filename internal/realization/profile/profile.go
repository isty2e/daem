package profile

import (
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

// TargetProfile is an immutable projection of independent static facts for one target.
// It chooses compatible facts but never combines their axes into a wider record.
type TargetProfile struct {
	selectedTarget  target.Target
	supports        map[entity.Kind]Support
	realizations    map[entity.Kind]realization.RealizationKind
	placements      []ManagedPathPlacement
	discoveries     []DiscoveryLocation
	runtime         []RuntimeLocation
	operationRoutes []OperationRoute
	mcpPlacements   []aggregate.MCPPlacement
	delegatedRoutes []DelegatedRouteProfile
}

// Profile returns the finite static profile for one target. Unknown targets
// produce an empty profile whose queries conservatively return no admission.
func Profile(selectedTarget target.Target) TargetProfile {
	supports := profileSupports(selectedTarget)
	mcpPlacements := profileMCPPlacements(selectedTarget)
	delegatedRoutes := profileDelegatedRoutes(selectedTarget)
	profile := TargetProfile{
		selectedTarget:  selectedTarget,
		supports:        supports,
		placements:      profilePlacements(selectedTarget),
		discoveries:     profileDiscoveries(selectedTarget),
		runtime:         profileRuntimeLocations(selectedTarget),
		operationRoutes: profileRoutes(selectedTarget, delegatedRoutes),
		mcpPlacements:   mcpPlacements,
		delegatedRoutes: delegatedRoutes,
	}
	profile.realizations = profileRealizations(supports, len(mcpPlacements), len(delegatedRoutes))
	if _, err := target.ParseTarget(string(selectedTarget)); err == nil {
		if err := profile.validate(); err != nil {
			panic(fmt.Sprintf("invalid target profile %q: %v", selectedTarget, err))
		}
	}
	return profile
}

// Support returns the explicit structural support fact for a resource.
func (profile TargetProfile) Support(resourceKind entity.Kind) (Support, bool) {
	support, ok := profile.supports[resourceKind]
	return support, ok
}

// Supports reports whether the target directly admits the resource.
func (profile TargetProfile) Supports(resourceKind entity.Kind) bool {
	support, ok := profile.Support(resourceKind)
	return ok && support.Supported()
}

// ResourceSupports returns structured resource support facts in stable order.
func (profile TargetProfile) ResourceSupports() []Support {
	result := make([]Support, 0, len(resourceKinds))
	for _, resourceKind := range resourceKinds {
		if support, ok := profile.supports[resourceKind]; ok {
			result = append(result, support)
		}
	}
	return result
}

// RealizationKind returns the structural realization selected for one desired family.
func (profile TargetProfile) RealizationKind(kind entity.Kind) (realization.RealizationKind, bool) {
	selected, ok := profile.realizations[kind]
	return selected, ok
}

// Placements returns write-authoritative managed-path placements for one resource and scope.
func (profile TargetProfile) Placements(resourceKind entity.Kind, scope target.Scope) []ManagedPathPlacement {
	result := make([]ManagedPathPlacement, 0)
	for _, placement := range profile.placements {
		if placement.ResourceKind() == resourceKind && placement.Scope() == scope {
			result = append(result, cloneManagedPathPlacement(placement))
		}
	}
	return result
}

// DefaultPlacement returns the single default write placement for one resource and scope.
func (profile TargetProfile) DefaultPlacement(resourceKind entity.Kind, scope target.Scope) (ManagedPathPlacement, error) {
	placements := profile.Placements(resourceKind, scope)
	var selected ManagedPathPlacement
	count := 0
	for _, placement := range placements {
		if !placement.Default() {
			continue
		}
		selected = placement
		count++
	}
	switch count {
	case 1:
		return selected, nil
	case 0:
		return ManagedPathPlacement{}, fmt.Errorf(
			"%s target %q scope %q has no default placement",
			resourceKind,
			profile.selectedTarget,
			scope,
		)
	default:
		return ManagedPathPlacement{}, fmt.Errorf(
			"%s target %q scope %q has multiple default placements",
			resourceKind,
			profile.selectedTarget,
			scope,
		)
	}
}

// PlacementAt returns the exact write-authoritative placement at path.
func (profile TargetProfile) PlacementAt(
	resourceKind entity.Kind,
	scope target.Scope,
	path string,
) (ManagedPathPlacement, bool) {
	var selected ManagedPathPlacement
	count := 0
	for _, placement := range profile.Placements(resourceKind, scope) {
		if placement.Root().String() == path {
			selected = placement
			count++
		}
	}
	return selected, count == 1
}

// DiscoveryLocations returns host-visible import candidates in stable priority order.
func (profile TargetProfile) DiscoveryLocations(resourceKind entity.Kind, scope target.Scope) []DiscoveryLocation {
	result := make([]DiscoveryLocation, 0)
	for _, location := range profile.discoveries {
		if location.ResourceKind() == resourceKind && location.Scope() == scope {
			result = append(result, location)
		}
	}
	sort.SliceStable(result, func(left int, right int) bool {
		if result[left].Priority() != result[right].Priority() {
			return result[left].Priority() < result[right].Priority()
		}
		return result[left].Path() < result[right].Path()
	})
	return result
}

// RuntimeLocations returns read-only host runtime lookup locations.
func (profile TargetProfile) RuntimeLocations(resourceKind entity.Kind, scope target.Scope) []RuntimeLocation {
	result := make([]RuntimeLocation, 0)
	for _, location := range profile.runtime {
		if location.ResourceKind() == resourceKind && location.Scope() == scope {
			result = append(result, location)
		}
	}
	sort.Slice(result, func(left int, right int) bool { return result[left].Path() < result[right].Path() })
	return result
}

// OperationRoute returns one exact route correlated to a placement or relation.
func (profile TargetProfile) OperationRoute(
	resourceKind entity.Kind,
	correlationID string,
	operation Operation,
) (OperationRoute, bool) {
	var selected OperationRoute
	count := 0
	for _, route := range profile.operationRoutes {
		if route.ResourceKind() == resourceKind && route.CorrelationID() == correlationID && route.Operation() == operation {
			selected = route
			count++
		}
	}
	return selected, count == 1
}

// MCPPlacement returns one standalone MCP aggregate placement selected by this profile.
func (profile TargetProfile) MCPPlacement(id aggregate.MCPPlacementID) (aggregate.MCPPlacement, bool) {
	for _, placement := range profile.mcpPlacements {
		if placement.ID() == id {
			return placement, true
		}
	}
	return aggregate.MCPPlacement{}, false
}

// DelegatedRoute returns the route profile for one desired carrier.
func (profile TargetProfile) DelegatedRoute(carrier desiredextension.Carrier) (DelegatedRouteProfile, bool) {
	for _, route := range profile.delegatedRoutes {
		if route.carrier == carrier {
			return cloneDelegatedRouteProfile(route), true
		}
	}
	return DelegatedRouteProfile{}, false
}

func profileSupports(selectedTarget target.Target) map[entity.Kind]Support {
	facts := supportCatalog[selectedTarget]
	result := make(map[entity.Kind]Support, len(facts))
	maps.Copy(result, facts)
	return result
}

func profilePlacements(selectedTarget target.Target) []ManagedPathPlacement {
	all := append(append([]ManagedPathPlacement(nil), instructionPlacements...), skillPlacements...)
	result := make([]ManagedPathPlacement, 0)
	for _, placement := range all {
		if targetSetContains(placement.consumerTargets, selectedTarget) {
			selected := cloneManagedPathPlacement(placement)
			selected.consumerTargets = []target.Target{selectedTarget}
			result = append(result, selected)
		}
	}
	return result
}

func profileDiscoveries(selectedTarget target.Target) []DiscoveryLocation {
	all := append(append([]DiscoveryLocation(nil), instructionDiscoveries...), skillDiscoveries...)
	result := make([]DiscoveryLocation, 0)
	for _, location := range all {
		if location.Target() == selectedTarget {
			result = append(result, location)
		}
	}
	return result
}

func profileRuntimeLocations(selectedTarget target.Target) []RuntimeLocation {
	all := append(append([]RuntimeLocation(nil), instructionRuntimeLocations...), skillRuntimeLocations...)
	result := make([]RuntimeLocation, 0)
	for _, location := range all {
		if location.Target() == selectedTarget {
			result = append(result, location)
		}
	}
	return result
}

func profileRoutes(selectedTarget target.Target, delegated []DelegatedRouteProfile) []OperationRoute {
	result := aggregateOperationRoutesForTarget(selectedTarget)
	for _, route := range profileOperationRoutes() {
		switch route.ResourceKind() {
		case entity.KindInstructions, entity.KindSkill:
			if profileHasPlacement(selectedTarget, route.ResourceKind(), route.CorrelationID()) {
				result = append(result, route)
			}
		case entity.KindHookAsset:
			if supportCatalog[selectedTarget][entity.KindHook].Supported() {
				result = append(result, route)
			}
		case entity.KindExtension:
			for _, delegatedRoute := range delegated {
				for _, operationRoute := range delegatedRoute.OperationRoutes() {
					if operationRoute == route {
						result = append(result, route)
					}
				}
			}
		}
	}
	return result
}

func profileHasPlacement(selectedTarget target.Target, resourceKind entity.Kind, placementID string) bool {
	for _, placement := range profilePlacements(selectedTarget) {
		if placement.ResourceKind() == resourceKind && placement.ID() == placementID {
			return true
		}
	}
	return false
}

func profileMCPPlacements(selectedTarget target.Target) []aggregate.MCPPlacement {
	placements := make([]aggregate.MCPPlacement, 0)
	for _, placement := range aggregate.ImplementedMCPPlacements() {
		if placement.Target() == selectedTarget {
			placements = append(placements, placement)
		}
	}
	return placements
}

func targetSetContains(values []target.Target, selectedTarget target.Target) bool {
	return slices.Contains(values, selectedTarget)
}

func cloneManagedPathPlacement(placement ManagedPathPlacement) ManagedPathPlacement {
	placement.consumerTargets = append([]target.Target(nil), placement.consumerTargets...)
	return placement
}
