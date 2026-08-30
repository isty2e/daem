package catalog

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/hostsurface"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
	topologyextension "github.com/isty2e/daem/internal/topology/extension"
)

type extensionSeed struct {
	carriers   []desiredextension.Carrier
	routes     []profile.DelegatedRouteProfile
	orders     []profile.ExtensionOrderAdmission
	namespaces map[desiredextension.Carrier]string
}

type extensionOrderKey struct {
	target  target.Target
	carrier desiredextension.Carrier
	scope   target.Scope
}

// ExtensionSurfaceView is one shadow-compiled delegated extension surface.
type ExtensionSurfaceView struct {
	id           hostsurface.SurfaceID
	key          hostsurface.SurfaceKey
	carrier      desiredextension.Carrier
	sourceKind   desiredextension.SourceKind
	namespace    string
	routeProfile profile.DelegatedRouteProfile
	order        profile.ExtensionOrderCapability
	hasOrder     bool
}

func (view ExtensionSurfaceView) ID() hostsurface.SurfaceID         { return view.id }
func (view ExtensionSurfaceView) Key() hostsurface.SurfaceKey       { return view.key }
func (view ExtensionSurfaceView) Carrier() desiredextension.Carrier { return view.carrier }
func (view ExtensionSurfaceView) RequiredSourceKind() desiredextension.SourceKind {
	return view.sourceKind
}
func (view ExtensionSurfaceView) Namespace() string { return view.namespace }
func (view ExtensionSurfaceView) RouteProfile() profile.DelegatedRouteProfile {
	return view.routeProfile
}

func (view ExtensionSurfaceView) OrderCapability() (profile.ExtensionOrderCapability, bool) {
	return view.order, view.hasOrder
}

func (view ExtensionSurfaceView) RealizationKind() realization.RealizationKind {
	return realization.RealizationDelegatedRelation
}

func productExtensionSeed() extensionSeed {
	carriers := desiredextension.SupportedCarriers()
	namespaces := make(map[desiredextension.Carrier]string, len(carriers))
	for _, carrier := range carriers {
		namespace, ok := topologyextension.CarrierNamespace(carrier)
		if !ok {
			panic("hostsurface catalog: missing Extension topology namespace for " + string(carrier))
		}
		namespaces[carrier] = namespace
	}
	return extensionSeed{
		carriers:   carriers,
		routes:     profile.DelegatedRouteProfiles(),
		orders:     profile.ExtensionOrderAdmissions(),
		namespaces: namespaces,
	}
}

func compileExtensionSurfaces(
	seed extensionSeed,
) ([]ExtensionSurfaceView, []hostsurface.SurfaceKey, error) {
	routes := make(map[desiredextension.Carrier]profile.DelegatedRouteProfile, len(seed.routes))
	for _, routeProfile := range seed.routes {
		if err := routeProfile.Validate(); err != nil {
			return nil, nil, fmt.Errorf("host-surface Extension route profile: %w", err)
		}
		if _, duplicate := routes[routeProfile.Carrier()]; duplicate {
			return nil, nil, fmt.Errorf(
				"host-surface Extension duplicate route profile for %q",
				routeProfile.Carrier(),
			)
		}
		routes[routeProfile.Carrier()] = routeProfile
	}

	orders := make(map[extensionOrderKey]profile.ExtensionOrderCapability, len(seed.orders))
	for _, admission := range seed.orders {
		capability := admission.Capability()
		if err := capability.Validate(); err != nil {
			return nil, nil, fmt.Errorf("host-surface Extension order capability: %w", err)
		}
		key := extensionOrderKey{
			target:  admission.Target(),
			carrier: capability.Carrier(),
			scope:   capability.Scope(),
		}
		if _, duplicate := orders[key]; duplicate {
			return nil, nil, fmt.Errorf(
				"host-surface Extension duplicate order capability for %s/%s/%s",
				key.target,
				key.carrier,
				key.scope,
			)
		}
		if !capability.Carrier().AdmitsTargetScope(admission.Target(), capability.Scope()) {
			return nil, nil, fmt.Errorf(
				"host-surface Extension order capability is outside carrier admission for %s/%s/%s",
				admission.Target(),
				capability.Carrier(),
				capability.Scope(),
			)
		}
		orders[key] = capability
	}

	views := make([]ExtensionSurfaceView, 0)
	ownerOrder := make([]hostsurface.SurfaceKey, 0)
	seenCarriers := make(map[desiredextension.Carrier]struct{}, len(seed.carriers))
	seenNamespaces := make(map[string]desiredextension.Carrier, len(seed.carriers))
	seenKeys := make(map[hostsurface.SurfaceKey]struct{})
	referencedOrders := make(map[extensionOrderKey]struct{}, len(orders))
	for _, carrier := range seed.carriers {
		parsed, err := desiredextension.ParseCarrier(string(carrier))
		if err != nil {
			return nil, nil, fmt.Errorf("host-surface Extension carrier: %w", err)
		}
		if _, duplicate := seenCarriers[parsed]; duplicate {
			return nil, nil, fmt.Errorf("host-surface Extension duplicate carrier %q", parsed)
		}
		seenCarriers[parsed] = struct{}{}
		selectedTarget, ok := parsed.AdmittedTarget()
		if !ok {
			return nil, nil, fmt.Errorf("host-surface Extension carrier %q has no target", parsed)
		}
		sourceKind, ok := parsed.RequiredSourceKind()
		if !ok {
			return nil, nil, fmt.Errorf("host-surface Extension carrier %q has no source kind", parsed)
		}
		routeProfile, ok := routes[parsed]
		if !ok || routeProfile.Target() != selectedTarget ||
			!slices.Equal(routeProfile.AdmittedScopes(), parsed.AdmittedScopes()) {
			return nil, nil, fmt.Errorf(
				"host-surface Extension carrier %q has no exact delegated route profile",
				parsed,
			)
		}
		for _, operation := range []profile.Operation{
			profile.OperationInstall,
			profile.OperationRemove,
			profile.OperationRefresh,
		} {
			if _, ok := routeProfile.OperationRoute(operation); !ok {
				return nil, nil, fmt.Errorf(
					"host-surface Extension carrier %q lacks %s route",
					parsed,
					operation,
				)
			}
		}
		namespace, ok := seed.namespaces[parsed]
		if !ok || namespace == "" {
			return nil, nil, fmt.Errorf(
				"host-surface Extension carrier %q has no topology namespace",
				parsed,
			)
		}
		if previous, duplicate := seenNamespaces[namespace]; duplicate {
			return nil, nil, fmt.Errorf(
				"host-surface Extension topology namespace %q is shared by %q and %q",
				namespace,
				previous,
				parsed,
			)
		}
		seenNamespaces[namespace] = parsed
		variant, err := hostsurface.ParseVariantID(string(parsed))
		if err != nil {
			return nil, nil, err
		}
		for _, scope := range parsed.AdmittedScopes() {
			key, err := hostsurface.NewSurfaceKey(
				selectedTarget,
				scope,
				entity.KindExtension,
				variant,
			)
			if err != nil {
				return nil, nil, err
			}
			if _, duplicate := seenKeys[key]; duplicate {
				return nil, nil, fmt.Errorf(
					"host-surface Extension duplicate surface %q",
					hostsurface.MustSurfaceID(key),
				)
			}
			seenKeys[key] = struct{}{}
			orderKey := extensionOrderKey{target: selectedTarget, carrier: parsed, scope: scope}
			order, hasOrder := orders[orderKey]
			if hasOrder {
				referencedOrders[orderKey] = struct{}{}
			}
			id, err := hostsurface.NewSurfaceID(key)
			if err != nil {
				return nil, nil, err
			}
			views = append(views, ExtensionSurfaceView{
				id:           id,
				key:          key,
				carrier:      parsed,
				sourceKind:   sourceKind,
				namespace:    namespace,
				routeProfile: routeProfile,
				order:        order,
				hasOrder:     hasOrder,
			})
			ownerOrder = append(ownerOrder, key)
		}
	}
	if len(routes) != len(seenCarriers) {
		return nil, nil, fmt.Errorf(
			"host-surface Extension route profiles = %d for %d carriers",
			len(routes),
			len(seenCarriers),
		)
	}
	if len(seed.namespaces) != len(seenCarriers) {
		return nil, nil, fmt.Errorf(
			"host-surface Extension namespaces = %d for %d carriers",
			len(seed.namespaces),
			len(seenCarriers),
		)
	}
	if len(referencedOrders) != len(orders) {
		return nil, nil, fmt.Errorf(
			"host-surface Extension referenced %d of %d order capabilities",
			len(referencedOrders),
			len(orders),
		)
	}
	slices.SortFunc(views, func(left ExtensionSurfaceView, right ExtensionSurfaceView) int {
		return hostsurface.CompareIDs(left.id, right.id)
	})
	return views, ownerOrder, nil
}

func (catalog Catalog) withExtensionSurfaces(seed extensionSeed) (Catalog, error) {
	views, ownerKeys, err := compileExtensionSurfaces(seed)
	if err != nil {
		return Catalog{}, err
	}
	catalog.extensionViews = views
	catalog.extensionOwnerOrder = make([]int, 0, len(ownerKeys))
	catalog.extensionByID = make(map[hostsurface.SurfaceID]int, len(views))
	catalog.extensionByKey = make(map[hostsurface.SurfaceKey]int, len(views))
	for index, view := range views {
		if err := catalog.rejectCompiledCollision(view.id, view.key); err != nil {
			return Catalog{}, err
		}
		catalog.extensionByID[view.id] = index
		catalog.extensionByKey[view.key] = index
	}
	for _, key := range ownerKeys {
		index, ok := catalog.extensionByKey[key]
		if !ok {
			return Catalog{}, fmt.Errorf("host-surface Extension owner key is not compiled")
		}
		catalog.extensionOwnerOrder = append(catalog.extensionOwnerOrder, index)
	}
	return catalog, nil
}

func (catalog Catalog) ExtensionSurfaces() []ExtensionSurfaceView {
	return append([]ExtensionSurfaceView(nil), catalog.extensionViews...)
}

func (catalog Catalog) ExtensionsInOwnerOrder() []ExtensionSurfaceView {
	result := make([]ExtensionSurfaceView, 0, len(catalog.extensionOwnerOrder))
	for _, index := range catalog.extensionOwnerOrder {
		result = append(result, catalog.extensionViews[index])
	}
	return result
}

func (catalog Catalog) ExtensionSurface(id hostsurface.SurfaceID) (ExtensionSurfaceView, bool) {
	index, ok := catalog.extensionByID[id]
	if !ok {
		return ExtensionSurfaceView{}, false
	}
	return catalog.extensionViews[index], true
}

// ExtensionViewsForTarget returns admitted carrier/scope cells in owner order.
func (catalog Catalog) ExtensionViewsForTarget(
	selectedTarget target.Target,
) []ExtensionSurfaceView {
	result := make([]ExtensionSurfaceView, 0)
	for _, view := range catalog.ExtensionsInOwnerOrder() {
		if view.Key().Target() == selectedTarget {
			result = append(result, view)
		}
	}
	return result
}

// LookupExtensionCell returns one exact target/scope/carrier surface.
func (catalog Catalog) LookupExtensionCell(
	selectedTarget target.Target,
	scope target.Scope,
	carrier desiredextension.Carrier,
) (ExtensionSurfaceView, bool) {
	variant, err := hostsurface.ParseVariantID(string(carrier))
	if err != nil {
		return ExtensionSurfaceView{}, false
	}
	key, err := hostsurface.NewSurfaceKey(
		selectedTarget,
		scope,
		entity.KindExtension,
		variant,
	)
	if err != nil {
		return ExtensionSurfaceView{}, false
	}
	return catalog.LookupExtension(key)
}

func (catalog Catalog) LookupExtension(key hostsurface.SurfaceKey) (ExtensionSurfaceView, bool) {
	index, ok := catalog.extensionByKey[key]
	if !ok {
		return ExtensionSurfaceView{}, false
	}
	return catalog.extensionViews[index], true
}
