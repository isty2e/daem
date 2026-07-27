package profile

import (
	"fmt"
	"maps"
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
	selectedTarget   target.Target
	supports         map[entity.Kind]Support
	realizations     map[entity.Kind]realization.RealizationKind
	placements       []ManagedPathPlacement
	admissions       []PlacementAdmission
	discoveries      []DiscoveryLocation
	runtime          []RuntimeLocation
	operationRoutes  []OperationRoute
	mcpPlacements    []aggregate.MCPPlacement
	mcpRuntimeProbes []MCPRuntimeProbeCapability
	delegatedRoutes  []DelegatedRouteProfile
}

// Profile returns the finite static profile for one target. Unknown targets
// produce an empty profile whose queries conservatively return no admission.
func Profile(selectedTarget target.Target) TargetProfile {
	supports := profileSupports(selectedTarget)
	mcpPlacements := profileMCPPlacements(selectedTarget)
	delegatedRoutes := profileDelegatedRoutes(selectedTarget)
	profile := TargetProfile{
		selectedTarget:   selectedTarget,
		supports:         supports,
		placements:       profilePlacements(selectedTarget),
		admissions:       profilePlacementAdmissions(selectedTarget),
		discoveries:      profileDiscoveries(selectedTarget),
		runtime:          profileRuntimeLocations(selectedTarget),
		operationRoutes:  profileRoutes(selectedTarget, delegatedRoutes),
		mcpPlacements:    mcpPlacements,
		mcpRuntimeProbes: profileMCPRuntimeProbeCapabilities(selectedTarget),
		delegatedRoutes:  delegatedRoutes,
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
			result = append(result, placement)
		}
	}
	return result
}

// DefaultPlacement returns the single default write placement for one resource and scope.
func (profile TargetProfile) DefaultPlacement(
	resourceKind entity.Kind,
	scope target.Scope,
) (SelectedManagedPathPlacement, error) {
	var selected ManagedPathPlacement
	count := 0
	for _, admission := range profile.admissions {
		placement, ok := profile.placement(admission.PlacementID())
		if !ok || placement.ResourceKind() != resourceKind || placement.Scope() != scope || !admission.Default() {
			continue
		}
		selected = placement
		count++
	}
	switch count {
	case 1:
		result, err := newSelectedManagedPathPlacement(selected, []target.Target{profile.selectedTarget})
		if err != nil {
			return SelectedManagedPathPlacement{}, err
		}
		return result, nil
	case 0:
		return SelectedManagedPathPlacement{}, fmt.Errorf(
			"%s target %q scope %q has no default placement",
			resourceKind,
			profile.selectedTarget,
			scope,
		)
	default:
		return SelectedManagedPathPlacement{}, fmt.Errorf(
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
) (SelectedManagedPathPlacement, bool) {
	var selected ManagedPathPlacement
	count := 0
	for _, placement := range profile.Placements(resourceKind, scope) {
		if placement.Root().String() == path {
			selected = placement
			count++
		}
	}
	if count != 1 {
		return SelectedManagedPathPlacement{}, false
	}
	result, err := newSelectedManagedPathPlacement(selected, []target.Target{profile.selectedTarget})
	return result, err == nil
}

// PlacementAdmissions returns the target-relative placement admissions for one
// resource and scope in stable catalog order.
func (profile TargetProfile) PlacementAdmissions(
	resourceKind entity.Kind,
	scope target.Scope,
) []PlacementAdmission {
	result := make([]PlacementAdmission, 0)
	for _, admission := range profile.admissions {
		placement, ok := profile.placement(admission.PlacementID())
		if ok && placement.ResourceKind() == resourceKind && placement.Scope() == scope {
			result = append(result, admission)
		}
	}
	return result
}

// PlacementAdmissionAt returns the target-relative admission for the exact
// write placement at path.
func (profile TargetProfile) PlacementAdmissionAt(
	resourceKind entity.Kind,
	scope target.Scope,
	path string,
) (PlacementAdmission, bool) {
	for _, admission := range profile.PlacementAdmissions(resourceKind, scope) {
		placement, ok := profile.placement(admission.PlacementID())
		if ok && placement.Root().String() == path {
			return admission, true
		}
	}
	return PlacementAdmission{}, false
}

func (profile TargetProfile) placement(placementID string) (ManagedPathPlacement, bool) {
	for _, placement := range profile.placements {
		if placement.ID() == placementID {
			return placement, true
		}
	}
	return ManagedPathPlacement{}, false
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

// HasImportableDiscovery reports whether at least one discovery location may
// contribute a standalone import candidate.
func (profile TargetProfile) HasImportableDiscovery() bool {
	for _, location := range profile.discoveries {
		if location.ImportPolicy() == ImportPolicyInclude {
			return true
		}
	}
	return false
}

// ImportableTargets returns target profiles with importable discovery evidence
// in stable product target order.
func ImportableTargets() []target.Target {
	result := make([]target.Target, 0)
	for _, selectedTarget := range target.SupportedTargets() {
		if Profile(selectedTarget).HasImportableDiscovery() {
			result = append(result, selectedTarget)
		}
	}
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

// MCPRuntimeProbeCapability returns the exact static probe row selected by this profile.
func (profile TargetProfile) MCPRuntimeProbeCapability(
	id aggregate.MCPPlacementID,
) (MCPRuntimeProbeCapability, bool) {
	for _, capability := range profile.mcpRuntimeProbes {
		if capability.Placement().ID() == id {
			return capability, true
		}
	}
	return MCPRuntimeProbeCapability{}, false
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
	admitted := make(map[string]struct{})
	for _, admission := range profilePlacementAdmissions(selectedTarget) {
		admitted[admission.PlacementID()] = struct{}{}
	}
	result := make([]ManagedPathPlacement, 0)
	for _, placement := range all {
		if _, ok := admitted[placement.ID()]; ok {
			result = append(result, placement)
		}
	}
	return result
}

func profilePlacementAdmissions(selectedTarget target.Target) []PlacementAdmission {
	all := append(
		append([]PlacementAdmission(nil), instructionPlacementAdmissions...),
		skillPlacementAdmissions...,
	)
	result := make([]PlacementAdmission, 0)
	for _, admission := range all {
		if admission.Target() == selectedTarget {
			result = append(result, admission)
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
	for _, admission := range profilePlacementAdmissions(selectedTarget) {
		placement, ok := placementByID(placementID)
		if ok && admission.PlacementID() == placementID && placement.ResourceKind() == resourceKind {
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

func placementByID(placementID string) (ManagedPathPlacement, bool) {
	for _, placement := range append(append([]ManagedPathPlacement(nil), instructionPlacements...), skillPlacements...) {
		if placement.ID() == placementID {
			return placement, true
		}
	}
	return ManagedPathPlacement{}, false
}
