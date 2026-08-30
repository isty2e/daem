package catalog

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/hostsurface"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
)

type hookSeed struct {
	placements []aggregate.HookPlacement
	routes     map[aggregate.HookPlacementID]RoutePair
	supports   []profile.Support
}

type hookAssetSeed struct {
	placements []profile.HookAssetPlacement
	supports   []profile.Support
}

// HookSurfaceView is one shadow-compiled managed-aggregate Hook surface.
type HookSurfaceView struct {
	id            hostsurface.SurfaceID
	key           hostsurface.SurfaceKey
	support       profile.Support
	placement     aggregate.HookPlacement
	writeRouteID  string
	removeRouteID string
}

func (view HookSurfaceView) ID() hostsurface.SurfaceID          { return view.id }
func (view HookSurfaceView) Key() hostsurface.SurfaceKey        { return view.key }
func (view HookSurfaceView) Support() profile.Support           { return view.support }
func (view HookSurfaceView) Placement() aggregate.HookPlacement { return view.placement }
func (view HookSurfaceView) RealizationKind() realization.RealizationKind {
	return realization.RealizationManagedAggregateContribution
}
func (view HookSurfaceView) WriteRouteID() string  { return view.writeRouteID }
func (view HookSurfaceView) RemoveRouteID() string { return view.removeRouteID }

// HookAssetSurfaceView is one target-relative view of a shared physical
// HookAsset managed-path placement.
type HookAssetSurfaceView struct {
	id          hostsurface.SurfaceID
	key         hostsurface.SurfaceKey
	hookSupport profile.Support
	placement   profile.HookAssetPlacement
	writeRoute  profile.OperationRoute
	removeRoute profile.OperationRoute
}

func (view HookAssetSurfaceView) ID() hostsurface.SurfaceID             { return view.id }
func (view HookAssetSurfaceView) Key() hostsurface.SurfaceKey           { return view.key }
func (view HookAssetSurfaceView) HookSupport() profile.Support          { return view.hookSupport }
func (view HookAssetSurfaceView) Placement() profile.HookAssetPlacement { return view.placement }
func (view HookAssetSurfaceView) RealizationKind() realization.RealizationKind {
	return realization.RealizationManagedPathProjection
}
func (view HookAssetSurfaceView) WriteRoute() profile.OperationRoute  { return view.writeRoute }
func (view HookAssetSurfaceView) RemoveRoute() profile.OperationRoute { return view.removeRoute }

func productHookSeed() hookSeed {
	placements := aggregate.ImplementedHookPlacements()
	routes := make(map[aggregate.HookPlacementID]RoutePair, len(placements))
	for _, placement := range placements {
		write, remove, ok := profile.HookAggregateRouteIDs(placement.ID())
		if !ok {
			panic("hostsurface catalog: missing Hook aggregate routes for " + string(placement.ID()))
		}
		routes[placement.ID()] = RoutePair{Write: write, Remove: remove}
	}
	return hookSeed{
		placements: placements,
		routes:     routes,
		supports:   profile.ResourceSupportFacts(),
	}
}

func productHookAssetSeed() hookAssetSeed {
	return hookAssetSeed{
		placements: profile.ImplementedHookAssetPlacements(),
		supports:   profile.ResourceSupportFacts(),
	}
}

func compileHookSurfaces(seed hookSeed) ([]HookSurfaceView, []hostsurface.SurfaceKey, error) {
	supports := make(map[managedPathSupportKey]profile.Support, len(seed.supports))
	for _, support := range seed.supports {
		if err := support.Validate(); err != nil {
			return nil, nil, fmt.Errorf("host-surface Hook support: %w", err)
		}
		key := managedPathSupportKey{target: support.Target(), kind: support.ResourceKind()}
		if _, duplicate := supports[key]; duplicate {
			return nil, nil, fmt.Errorf(
				"host-surface Hook duplicate support for %s/%s",
				support.Target(),
				support.ResourceKind(),
			)
		}
		supports[key] = support
	}
	views := make([]HookSurfaceView, 0, len(seed.placements))
	ownerOrder := make([]hostsurface.SurfaceKey, 0, len(seed.placements))
	seenPlacements := make(map[aggregate.HookPlacementID]struct{}, len(seed.placements))
	seenKeys := make(map[hostsurface.SurfaceKey]struct{}, len(seed.placements))
	for _, placement := range seed.placements {
		if err := placement.Validate(); err != nil {
			return nil, nil, fmt.Errorf("host-surface Hook placement: %w", err)
		}
		if _, duplicate := seenPlacements[placement.ID()]; duplicate {
			return nil, nil, fmt.Errorf("host-surface Hook duplicate placement %q", placement.ID())
		}
		seenPlacements[placement.ID()] = struct{}{}
		support, ok := supports[managedPathSupportKey{target: placement.Target(), kind: entity.KindHook}]
		if !ok || !support.Supported() {
			return nil, nil, fmt.Errorf("host-surface Hook placement %q lacks supported owner fact", placement.ID())
		}
		routes, ok := seed.routes[placement.ID()]
		if !ok || routes.Write == "" || routes.Remove == "" {
			return nil, nil, fmt.Errorf("host-surface Hook placement %q lacks write and remove routes", placement.ID())
		}
		variant, err := hostsurface.ParseVariantID(string(placement.ID()))
		if err != nil {
			return nil, nil, err
		}
		key, err := hostsurface.NewSurfaceKey(
			placement.Target(),
			placement.Scope(),
			entity.KindHook,
			variant,
		)
		if err != nil {
			return nil, nil, err
		}
		if _, duplicate := seenKeys[key]; duplicate {
			return nil, nil, fmt.Errorf("host-surface Hook duplicate surface %q", hostsurface.MustSurfaceID(key))
		}
		seenKeys[key] = struct{}{}
		id, err := hostsurface.NewSurfaceID(key)
		if err != nil {
			return nil, nil, err
		}
		views = append(views, HookSurfaceView{
			id:            id,
			key:           key,
			support:       support,
			placement:     placement,
			writeRouteID:  routes.Write,
			removeRouteID: routes.Remove,
		})
		ownerOrder = append(ownerOrder, key)
	}
	if len(seed.routes) != len(seenPlacements) {
		return nil, nil, fmt.Errorf(
			"host-surface Hook route rows = %d for %d placements",
			len(seed.routes),
			len(seenPlacements),
		)
	}
	slices.SortFunc(views, func(left HookSurfaceView, right HookSurfaceView) int {
		return hostsurface.CompareIDs(left.id, right.id)
	})
	return views, ownerOrder, nil
}

func compileHookAssetSurfaces(
	seed hookAssetSeed,
) ([]HookAssetSurfaceView, []hostsurface.SurfaceKey, error) {
	supports := make(map[managedPathSupportKey]profile.Support, len(seed.supports))
	for _, support := range seed.supports {
		if err := support.Validate(); err != nil {
			return nil, nil, fmt.Errorf("host-surface HookAsset support: %w", err)
		}
		key := managedPathSupportKey{target: support.Target(), kind: support.ResourceKind()}
		if _, duplicate := supports[key]; duplicate {
			return nil, nil, fmt.Errorf(
				"host-surface HookAsset duplicate support for %s/%s",
				support.Target(),
				support.ResourceKind(),
			)
		}
		supports[key] = support
	}
	views := make([]HookAssetSurfaceView, 0, 2*len(seed.placements))
	ownerOrder := make([]hostsurface.SurfaceKey, 0, 2*len(seed.placements))
	seenPlacements := make(map[string]struct{}, len(seed.placements))
	seenKeys := make(map[hostsurface.SurfaceKey]struct{})
	for _, placement := range seed.placements {
		if err := placement.Validate(); err != nil {
			return nil, nil, fmt.Errorf("host-surface HookAsset placement: %w", err)
		}
		if _, duplicate := seenPlacements[placement.ID()]; duplicate {
			return nil, nil, fmt.Errorf("host-surface HookAsset duplicate placement %q", placement.ID())
		}
		seenPlacements[placement.ID()] = struct{}{}
		writeRoute, err := profile.HookAssetOperationRoute(placement, profile.OperationWrite)
		if err != nil {
			return nil, nil, err
		}
		removeRoute, err := profile.HookAssetOperationRoute(placement, profile.OperationRemove)
		if err != nil {
			return nil, nil, err
		}
		variant, err := hostsurface.ParseVariantID(placement.ID())
		if err != nil {
			return nil, nil, err
		}
		for _, consumer := range placement.ConsumerTargets() {
			support, ok := supports[managedPathSupportKey{target: consumer, kind: entity.KindHook}]
			if !ok || !support.Supported() {
				return nil, nil, fmt.Errorf(
					"host-surface HookAsset placement %q has unsupported consumer %q",
					placement.ID(),
					consumer,
				)
			}
			key, err := hostsurface.NewSurfaceKey(
				consumer,
				placement.Scope(),
				entity.KindHookAsset,
				variant,
			)
			if err != nil {
				return nil, nil, err
			}
			if _, duplicate := seenKeys[key]; duplicate {
				return nil, nil, fmt.Errorf("host-surface HookAsset duplicate surface %q", hostsurface.MustSurfaceID(key))
			}
			seenKeys[key] = struct{}{}
			id, err := hostsurface.NewSurfaceID(key)
			if err != nil {
				return nil, nil, err
			}
			views = append(views, HookAssetSurfaceView{
				id:          id,
				key:         key,
				hookSupport: support,
				placement:   placement,
				writeRoute:  writeRoute,
				removeRoute: removeRoute,
			})
			ownerOrder = append(ownerOrder, key)
		}
	}
	slices.SortFunc(views, func(left HookAssetSurfaceView, right HookAssetSurfaceView) int {
		return hostsurface.CompareIDs(left.id, right.id)
	})
	return views, ownerOrder, nil
}

func (catalog Catalog) withHookSurfaces(hookSeed hookSeed, assetSeed hookAssetSeed) (Catalog, error) {
	hooks, hookOwnerKeys, err := compileHookSurfaces(hookSeed)
	if err != nil {
		return Catalog{}, err
	}
	assets, assetOwnerKeys, err := compileHookAssetSurfaces(assetSeed)
	if err != nil {
		return Catalog{}, err
	}
	catalog.hookViews = hooks
	catalog.hookOwnerOrder = make([]int, 0, len(hookOwnerKeys))
	catalog.hookByID = make(map[hostsurface.SurfaceID]int, len(hooks))
	catalog.hookByKey = make(map[hostsurface.SurfaceKey]int, len(hooks))
	for index, view := range hooks {
		if err := catalog.rejectCompiledCollision(view.id, view.key); err != nil {
			return Catalog{}, err
		}
		catalog.hookByID[view.id] = index
		catalog.hookByKey[view.key] = index
	}
	for _, key := range hookOwnerKeys {
		index, ok := catalog.hookByKey[key]
		if !ok {
			return Catalog{}, fmt.Errorf("host-surface Hook owner key is not compiled")
		}
		catalog.hookOwnerOrder = append(catalog.hookOwnerOrder, index)
	}

	catalog.hookAssetViews = assets
	catalog.hookAssetOwnerOrder = make([]int, 0, len(assetOwnerKeys))
	catalog.hookAssetByID = make(map[hostsurface.SurfaceID]int, len(assets))
	catalog.hookAssetByKey = make(map[hostsurface.SurfaceKey]int, len(assets))
	for index, view := range assets {
		if err := catalog.rejectCompiledCollision(view.id, view.key); err != nil {
			return Catalog{}, err
		}
		catalog.hookAssetByID[view.id] = index
		catalog.hookAssetByKey[view.key] = index
	}
	for _, key := range assetOwnerKeys {
		index, ok := catalog.hookAssetByKey[key]
		if !ok {
			return Catalog{}, fmt.Errorf("host-surface HookAsset owner key is not compiled")
		}
		catalog.hookAssetOwnerOrder = append(catalog.hookAssetOwnerOrder, index)
	}
	return catalog, nil
}

func (catalog Catalog) rejectCompiledCollision(id hostsurface.SurfaceID, key hostsurface.SurfaceKey) error {
	if _, collision := catalog.byID[id]; collision {
		return fmt.Errorf("host-surface ID collides with MCP surface %q", id)
	}
	if _, collision := catalog.managedPathByID[id]; collision {
		return fmt.Errorf("host-surface ID collides with managed-path surface %q", id)
	}
	if _, collision := catalog.byKey[key]; collision {
		return fmt.Errorf("host-surface key collides with MCP surface %q", id)
	}
	if _, collision := catalog.managedPathByKey[key]; collision {
		return fmt.Errorf("host-surface key collides with managed-path surface %q", id)
	}
	if _, collision := catalog.hookByID[id]; collision {
		return fmt.Errorf("host-surface ID collides with Hook surface %q", id)
	}
	if _, collision := catalog.hookByKey[key]; collision {
		return fmt.Errorf("host-surface key collides with Hook surface %q", id)
	}
	if _, collision := catalog.hookAssetByID[id]; collision {
		return fmt.Errorf("host-surface ID collides with HookAsset surface %q", id)
	}
	if _, collision := catalog.hookAssetByKey[key]; collision {
		return fmt.Errorf("host-surface key collides with HookAsset surface %q", id)
	}
	if _, collision := catalog.extensionByID[id]; collision {
		return fmt.Errorf("host-surface ID collides with Extension surface %q", id)
	}
	if _, collision := catalog.extensionByKey[key]; collision {
		return fmt.Errorf("host-surface key collides with Extension surface %q", id)
	}
	return nil
}

func (catalog Catalog) HookSurfaces() []HookSurfaceView {
	return append([]HookSurfaceView(nil), catalog.hookViews...)
}

func (catalog Catalog) HooksInOwnerOrder() []HookSurfaceView {
	result := make([]HookSurfaceView, 0, len(catalog.hookOwnerOrder))
	for _, index := range catalog.hookOwnerOrder {
		result = append(result, catalog.hookViews[index])
	}
	return result
}

func (catalog Catalog) HookSurface(id hostsurface.SurfaceID) (HookSurfaceView, bool) {
	index, ok := catalog.hookByID[id]
	if !ok {
		return HookSurfaceView{}, false
	}
	return catalog.hookViews[index], true
}

func (catalog Catalog) LookupHook(key hostsurface.SurfaceKey) (HookSurfaceView, bool) {
	index, ok := catalog.hookByKey[key]
	if !ok {
		return HookSurfaceView{}, false
	}
	return catalog.hookViews[index], true
}

func (catalog Catalog) HookAssetSurfaces() []HookAssetSurfaceView {
	return append([]HookAssetSurfaceView(nil), catalog.hookAssetViews...)
}

func (catalog Catalog) HookAssetsInOwnerOrder() []HookAssetSurfaceView {
	result := make([]HookAssetSurfaceView, 0, len(catalog.hookAssetOwnerOrder))
	for _, index := range catalog.hookAssetOwnerOrder {
		result = append(result, catalog.hookAssetViews[index])
	}
	return result
}

func (catalog Catalog) HookAssetSurface(id hostsurface.SurfaceID) (HookAssetSurfaceView, bool) {
	index, ok := catalog.hookAssetByID[id]
	if !ok {
		return HookAssetSurfaceView{}, false
	}
	return catalog.hookAssetViews[index], true
}

func (catalog Catalog) LookupHookAsset(key hostsurface.SurfaceKey) (HookAssetSurfaceView, bool) {
	index, ok := catalog.hookAssetByKey[key]
	if !ok {
		return HookAssetSurfaceView{}, false
	}
	return catalog.hookAssetViews[index], true
}
