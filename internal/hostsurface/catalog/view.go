package catalog

import (
	"github.com/isty2e/daem/internal/hostsurface"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
)

// ObservationPurpose is one static observation question a surface may answer.
type ObservationPurpose string

const (
	// ObservationRuntimeProbe is the catalogued MCP runtime-probe purpose.
	ObservationRuntimeProbe ObservationPurpose = "runtime_probe"
)

// SurfaceView is one compiled MCP host-surface cell. Facet values remain
// owned by their source catalogs; the view stores references only.
type SurfaceView struct {
	id                hostsurface.SurfaceID
	key               hostsurface.SurfaceKey
	placement         aggregate.MCPPlacement
	namespace         string
	writeRouteID      string
	removeRouteID     string
	runtimeProbe      profile.MCPRuntimeProbeCapability
	hasRuntimeProbe   bool
	providerAuthoring bool
}

// ID returns the opaque internal surface identity.
func (view SurfaceView) ID() hostsurface.SurfaceID { return view.id }

// Key returns the semantic surface key.
func (view SurfaceView) Key() hostsurface.SurfaceKey { return view.key }

// Placement returns the owner-local MCP placement row.
func (view SurfaceView) Placement() aggregate.MCPPlacement { return view.placement }

// Namespace returns the topology projection namespace token.
func (view SurfaceView) Namespace() string { return view.namespace }

// WriteRouteID returns the write actuation route token.
func (view SurfaceView) WriteRouteID() string { return view.writeRouteID }

// RemoveRouteID returns the remove actuation route token.
func (view SurfaceView) RemoveRouteID() string { return view.removeRouteID }

// RuntimeProbe returns the optional runtime-probe observation facet.
func (view SurfaceView) RuntimeProbe() (profile.MCPRuntimeProbeCapability, bool) {
	return view.runtimeProbe, view.hasRuntimeProbe
}

// ProviderAuthoringAdmitted reports whether the surface's target admits MCP
// provider authoring.
func (view SurfaceView) ProviderAuthoringAdmitted() bool {
	return view.providerAuthoring
}

// ObservationPurposes returns static observation purposes for this surface.
func (view SurfaceView) ObservationPurposes() []ObservationPurpose {
	if !view.hasRuntimeProbe {
		return nil
	}
	return []ObservationPurpose{ObservationRuntimeProbe}
}

// IsDefaultVariant reports whether this cell is the family's default variant.
func (view SurfaceView) IsDefaultVariant() bool {
	return view.key.Variant() == hostsurface.VariantDefault
}

// Catalog is an immutable compiled host-surface snapshot.
type Catalog struct {
	views      []SurfaceView
	ownerOrder []int
	byID       map[hostsurface.SurfaceID]int
	byKey      map[hostsurface.SurfaceKey]int

	managedPathViews      []ManagedPathSurfaceView
	managedPathOwnerOrder []int
	managedPathByID       map[hostsurface.SurfaceID]int
	managedPathByKey      map[hostsurface.SurfaceKey]int

	hookViews      []HookSurfaceView
	hookOwnerOrder []int
	hookByID       map[hostsurface.SurfaceID]int
	hookByKey      map[hostsurface.SurfaceKey]int

	hookAssetViews      []HookAssetSurfaceView
	hookAssetOwnerOrder []int
	hookAssetByID       map[hostsurface.SurfaceID]int
	hookAssetByKey      map[hostsurface.SurfaceKey]int
}

// Surfaces returns compiled MCP views in stable key order.
func (catalog Catalog) Surfaces() []SurfaceView {
	out := make([]SurfaceView, len(catalog.views))
	copy(out, catalog.views)
	return out
}

// Surface returns the compiled MCP view for an opaque ID.
func (catalog Catalog) Surface(id hostsurface.SurfaceID) (SurfaceView, bool) {
	index, ok := catalog.byID[id]
	if !ok {
		return SurfaceView{}, false
	}
	return catalog.views[index], true
}

// Lookup returns the compiled MCP view for a semantic key. Missing keys are
// unsupported, not compiled as support=false rows.
func (catalog Catalog) Lookup(key hostsurface.SurfaceKey) (SurfaceView, bool) {
	index, ok := catalog.byKey[key]
	if !ok {
		return SurfaceView{}, false
	}
	return catalog.views[index], true
}
