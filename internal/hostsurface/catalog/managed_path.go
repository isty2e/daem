package catalog

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/hostsurface"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

type managedPathSeed struct {
	placements  []profile.ManagedPathPlacement
	admissions  []profile.PlacementAdmission
	discoveries []profile.DiscoveryLocation
	runtime     []profile.RuntimeLocation
	routes      []profile.OperationRoute
	supports    []profile.Support
}

type managedPathGroup struct {
	target target.Target
	scope  target.Scope
	kind   entity.Kind
}

type managedPathSupportKey struct {
	target target.Target
	kind   entity.Kind
}

type managedPathRouteKey struct {
	kind        entity.Kind
	placementID string
	operation   profile.Operation
}

// ManagedPathSurfaceView is one shadow-compiled Instruction or Skill surface.
// Owner-local profile facts remain authoritative until a later consumer cutover.
type ManagedPathSurfaceView struct {
	id          hostsurface.SurfaceID
	key         hostsurface.SurfaceKey
	support     profile.Support
	placement   profile.ManagedPathPlacement
	admission   profile.PlacementAdmission
	discoveries []profile.DiscoveryLocation
	runtime     []profile.RuntimeLocation
	writeRoute  profile.OperationRoute
	removeRoute profile.OperationRoute
}

func (view ManagedPathSurfaceView) ID() hostsurface.SurfaceID               { return view.id }
func (view ManagedPathSurfaceView) Key() hostsurface.SurfaceKey             { return view.key }
func (view ManagedPathSurfaceView) Support() profile.Support                { return view.support }
func (view ManagedPathSurfaceView) Placement() profile.ManagedPathPlacement { return view.placement }

func (view ManagedPathSurfaceView) Admission() profile.PlacementAdmission { return view.admission }

func (view ManagedPathSurfaceView) RealizationKind() realization.RealizationKind {
	return realization.RealizationManagedPathProjection
}

func (view ManagedPathSurfaceView) DiscoveryLocations() []profile.DiscoveryLocation {
	return append([]profile.DiscoveryLocation(nil), view.discoveries...)
}

func (view ManagedPathSurfaceView) RuntimeLocations() []profile.RuntimeLocation {
	return append([]profile.RuntimeLocation(nil), view.runtime...)
}
func (view ManagedPathSurfaceView) WriteRoute() profile.OperationRoute  { return view.writeRoute }
func (view ManagedPathSurfaceView) RemoveRoute() profile.OperationRoute { return view.removeRoute }
func (view ManagedPathSurfaceView) IsDefaultPlacement() bool            { return view.admission.Default() }

func productManagedPathSeed() managedPathSeed {
	facets := profile.StaticManagedPathFacets()
	return managedPathSeed{
		placements:  facets.Placements(),
		admissions:  facets.Admissions(),
		discoveries: facets.Discoveries(),
		runtime:     facets.RuntimeLocations(),
		routes:      facets.OperationRoutes(),
		supports:    profile.ResourceSupportFacts(),
	}
}

func compileManagedPathSurfaces(
	seed managedPathSeed,
) ([]ManagedPathSurfaceView, []hostsurface.SurfaceKey, error) {
	placements := make(map[string]profile.ManagedPathPlacement, len(seed.placements))
	for _, placement := range seed.placements {
		if err := placement.Validate(); err != nil {
			return nil, nil, fmt.Errorf("host-surface managed-path placement: %w", err)
		}
		if placement.ResourceKind() != entity.KindInstructions && placement.ResourceKind() != entity.KindSkill {
			return nil, nil, fmt.Errorf(
				"host-surface managed-path placement %q has unsupported kind %q",
				placement.ID(),
				placement.ResourceKind(),
			)
		}
		if _, duplicate := placements[placement.ID()]; duplicate {
			return nil, nil, fmt.Errorf("host-surface managed-path duplicate placement %q", placement.ID())
		}
		placements[placement.ID()] = placement
	}

	supports := make(map[managedPathSupportKey]profile.Support, len(seed.supports))
	for _, support := range seed.supports {
		if err := support.Validate(); err != nil {
			return nil, nil, fmt.Errorf("host-surface managed-path support: %w", err)
		}
		key := managedPathSupportKey{target: support.Target(), kind: support.ResourceKind()}
		if _, duplicate := supports[key]; duplicate {
			return nil, nil, fmt.Errorf(
				"host-surface managed-path duplicate support for %s/%s",
				support.Target(),
				support.ResourceKind(),
			)
		}
		supports[key] = support
	}

	routes := make(map[managedPathRouteKey]profile.OperationRoute, len(seed.routes))
	for _, route := range seed.routes {
		if err := route.Validate(); err != nil {
			return nil, nil, fmt.Errorf("host-surface managed-path route: %w", err)
		}
		key := managedPathRouteKey{
			kind:        route.ResourceKind(),
			placementID: route.CorrelationID(),
			operation:   route.Operation(),
		}
		if _, duplicate := routes[key]; duplicate {
			return nil, nil, fmt.Errorf(
				"host-surface managed-path duplicate %s route for %q",
				route.Operation(),
				route.CorrelationID(),
			)
		}
		routes[key] = route
	}

	discoveries := make(map[managedPathGroup][]profile.DiscoveryLocation)
	for _, location := range seed.discoveries {
		if err := location.Validate(); err != nil {
			return nil, nil, fmt.Errorf("host-surface managed-path discovery: %w", err)
		}
		group := managedPathGroup{target: location.Target(), scope: location.Scope(), kind: location.ResourceKind()}
		discoveries[group] = append(discoveries[group], location)
	}
	for group := range discoveries {
		slices.SortStableFunc(discoveries[group], func(left profile.DiscoveryLocation, right profile.DiscoveryLocation) int {
			if order := cmp.Compare(left.Priority(), right.Priority()); order != 0 {
				return order
			}
			return cmp.Compare(left.Path(), right.Path())
		})
	}

	runtime := make(map[managedPathGroup][]profile.RuntimeLocation)
	for _, location := range seed.runtime {
		if err := location.Validate(); err != nil {
			return nil, nil, fmt.Errorf("host-surface managed-path runtime: %w", err)
		}
		group := managedPathGroup{target: location.Target(), scope: location.Scope(), kind: location.ResourceKind()}
		runtime[group] = append(runtime[group], location)
	}
	for group := range runtime {
		slices.SortFunc(runtime[group], func(left profile.RuntimeLocation, right profile.RuntimeLocation) int {
			return cmp.Compare(left.Path(), right.Path())
		})
	}

	views := make([]ManagedPathSurfaceView, 0, len(seed.admissions))
	ownerOrder := make([]hostsurface.SurfaceKey, 0, len(seed.admissions))
	seenAdmissions := make(map[[2]string]struct{}, len(seed.admissions))
	referencedPlacements := make(map[string]struct{}, len(seed.placements))
	referencedRoutes := make(map[managedPathRouteKey]struct{}, len(seed.routes))
	groups := make(map[managedPathGroup]int)
	defaultCounts := make(map[managedPathGroup]int)
	seenKeys := make(map[hostsurface.SurfaceKey]struct{}, len(seed.admissions))

	for _, admission := range seed.admissions {
		if err := admission.Validate(); err != nil {
			return nil, nil, fmt.Errorf("host-surface managed-path admission: %w", err)
		}
		admissionKey := [2]string{string(admission.Target()), admission.PlacementID()}
		if _, duplicate := seenAdmissions[admissionKey]; duplicate {
			return nil, nil, fmt.Errorf(
				"host-surface managed-path duplicate admission for %s/%s",
				admission.Target(),
				admission.PlacementID(),
			)
		}
		seenAdmissions[admissionKey] = struct{}{}
		placement, ok := placements[admission.PlacementID()]
		if !ok {
			return nil, nil, fmt.Errorf(
				"host-surface managed-path admission for %s references missing placement %q",
				admission.Target(),
				admission.PlacementID(),
			)
		}
		support, ok := supports[managedPathSupportKey{target: admission.Target(), kind: placement.ResourceKind()}]
		if !ok || !support.Supported() {
			return nil, nil, fmt.Errorf(
				"host-surface managed-path admission for %s/%s lacks supported owner fact",
				admission.Target(),
				placement.ResourceKind(),
			)
		}
		variant, err := hostsurface.ParseVariantID(placement.ID())
		if err != nil {
			return nil, nil, fmt.Errorf("host-surface managed-path variant: %w", err)
		}
		key, err := hostsurface.NewSurfaceKey(
			admission.Target(),
			placement.Scope(),
			placement.ResourceKind(),
			variant,
		)
		if err != nil {
			return nil, nil, err
		}
		if _, duplicate := seenKeys[key]; duplicate {
			return nil, nil, fmt.Errorf(
				"host-surface managed-path duplicate surface %s",
				hostsurface.MustSurfaceID(key),
			)
		}
		seenKeys[key] = struct{}{}
		group := managedPathGroup{target: admission.Target(), scope: placement.Scope(), kind: placement.ResourceKind()}
		groups[group]++
		if admission.Default() {
			defaultCounts[group]++
		}

		writeKey := managedPathRouteKey{kind: placement.ResourceKind(), placementID: placement.ID(), operation: profile.OperationWrite}
		writeRoute, ok := routes[writeKey]
		if !ok || !writeRoute.Correlates(placement.ResourceKind(), placement.ID(), profile.OperationWrite) {
			return nil, nil, fmt.Errorf("host-surface managed-path placement %q lacks write route", placement.ID())
		}
		removeKey := managedPathRouteKey{kind: placement.ResourceKind(), placementID: placement.ID(), operation: profile.OperationRemove}
		removeRoute, ok := routes[removeKey]
		if !ok || !removeRoute.Correlates(placement.ResourceKind(), placement.ID(), profile.OperationRemove) {
			return nil, nil, fmt.Errorf("host-surface managed-path placement %q lacks remove route", placement.ID())
		}
		id, err := hostsurface.NewSurfaceID(key)
		if err != nil {
			return nil, nil, err
		}
		views = append(views, ManagedPathSurfaceView{
			id:          id,
			key:         key,
			support:     support,
			placement:   placement,
			admission:   admission,
			discoveries: append([]profile.DiscoveryLocation(nil), discoveries[group]...),
			runtime:     append([]profile.RuntimeLocation(nil), runtime[group]...),
			writeRoute:  writeRoute,
			removeRoute: removeRoute,
		})
		ownerOrder = append(ownerOrder, key)
		referencedPlacements[placement.ID()] = struct{}{}
		referencedRoutes[writeKey] = struct{}{}
		referencedRoutes[removeKey] = struct{}{}
	}

	for group, count := range groups {
		if defaultCounts[group] != 1 {
			return nil, nil, fmt.Errorf(
				"host-surface managed-path %s/%s/%s has %d defaults across %d surfaces",
				group.target,
				group.scope,
				group.kind,
				defaultCounts[group],
				count,
			)
		}
	}
	for group := range discoveries {
		if groups[group] == 0 {
			return nil, nil, fmt.Errorf(
				"host-surface managed-path discovery has no surface for %s/%s/%s",
				group.target,
				group.scope,
				group.kind,
			)
		}
	}
	for group := range runtime {
		if groups[group] == 0 {
			return nil, nil, fmt.Errorf(
				"host-surface managed-path runtime has no surface for %s/%s/%s",
				group.target,
				group.scope,
				group.kind,
			)
		}
	}
	if len(referencedPlacements) != len(placements) {
		return nil, nil, fmt.Errorf(
			"host-surface managed-path referenced %d of %d placements",
			len(referencedPlacements),
			len(placements),
		)
	}
	if len(referencedRoutes) != len(routes) {
		return nil, nil, fmt.Errorf(
			"host-surface managed-path referenced %d of %d routes",
			len(referencedRoutes),
			len(routes),
		)
	}

	slices.SortFunc(views, func(left ManagedPathSurfaceView, right ManagedPathSurfaceView) int {
		return hostsurface.CompareIDs(left.id, right.id)
	})
	return views, ownerOrder, nil
}

func (catalog Catalog) withManagedPathSurfaces(seed managedPathSeed) (Catalog, error) {
	views, ownerKeys, err := compileManagedPathSurfaces(seed)
	if err != nil {
		return Catalog{}, err
	}
	catalog.managedPathViews = views
	catalog.managedPathOwnerOrder = make([]int, 0, len(ownerKeys))
	catalog.managedPathByID = make(map[hostsurface.SurfaceID]int, len(views))
	catalog.managedPathByKey = make(map[hostsurface.SurfaceKey]int, len(views))
	for index, view := range views {
		if _, collision := catalog.byID[view.id]; collision {
			return Catalog{}, fmt.Errorf("host-surface managed-path ID collides with MCP surface %q", view.id)
		}
		if _, collision := catalog.byKey[view.key]; collision {
			return Catalog{}, fmt.Errorf("host-surface managed-path key collides with MCP surface %q", view.id)
		}
		if _, duplicate := catalog.managedPathByID[view.id]; duplicate {
			return Catalog{}, fmt.Errorf("host-surface managed-path duplicate ID %q", view.id)
		}
		catalog.managedPathByID[view.id] = index
		catalog.managedPathByKey[view.key] = index
	}
	for _, key := range ownerKeys {
		index, ok := catalog.managedPathByKey[key]
		if !ok {
			return Catalog{}, fmt.Errorf("host-surface managed-path owner key is not compiled")
		}
		catalog.managedPathOwnerOrder = append(catalog.managedPathOwnerOrder, index)
	}
	return catalog, nil
}

// ManagedPathSurfaces returns shadow-compiled managed-path views in SurfaceID order.
func (catalog Catalog) ManagedPathSurfaces() []ManagedPathSurfaceView {
	return append([]ManagedPathSurfaceView(nil), catalog.managedPathViews...)
}

// ManagedPathsInOwnerOrder returns views in placement-admission catalog order.
func (catalog Catalog) ManagedPathsInOwnerOrder() []ManagedPathSurfaceView {
	result := make([]ManagedPathSurfaceView, 0, len(catalog.managedPathOwnerOrder))
	for _, index := range catalog.managedPathOwnerOrder {
		result = append(result, catalog.managedPathViews[index])
	}
	return result
}

// ManagedPathSurface returns the shadow view for one managed-path SurfaceID.
func (catalog Catalog) ManagedPathSurface(id hostsurface.SurfaceID) (ManagedPathSurfaceView, bool) {
	index, ok := catalog.managedPathByID[id]
	if !ok {
		return ManagedPathSurfaceView{}, false
	}
	return catalog.managedPathViews[index], true
}

// LookupManagedPath returns the shadow view for one managed-path SurfaceKey.
func (catalog Catalog) LookupManagedPath(key hostsurface.SurfaceKey) (ManagedPathSurfaceView, bool) {
	index, ok := catalog.managedPathByKey[key]
	if !ok {
		return ManagedPathSurfaceView{}, false
	}
	return catalog.managedPathViews[index], true
}
