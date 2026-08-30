package catalog

import (
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/hostsurface"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// LookupMCP returns the compiled MCP surface for target and scope. Invalid
// coordinates and unsupported cells are missing, matching owner-catalog
// not-found semantics.
func (catalog Catalog) LookupMCP(selectedTarget target.Target, scope target.Scope) (SurfaceView, bool) {
	key, err := hostsurface.MCPSurfaceKey(selectedTarget, scope)
	if err != nil {
		return SurfaceView{}, false
	}
	return catalog.Lookup(key)
}

// HasMCPTarget reports whether any compiled MCP surface exists for target.
func (catalog Catalog) HasMCPTarget(selectedTarget target.Target) bool {
	for _, view := range catalog.views {
		if view.key.Kind() == entity.KindMCPServer && view.key.Target() == selectedTarget {
			return true
		}
	}
	return false
}

// HasMCPProviderAuthoring reports whether any compiled MCP surface for target
// admits provider authoring. Invalid targets and hosts without a provider
// profile are missing, matching owner-catalog not-found semantics.
func (catalog Catalog) HasMCPProviderAuthoring(selectedTarget target.Target) bool {
	for _, view := range catalog.views {
		if view.key.Kind() == entity.KindMCPServer &&
			view.key.Target() == selectedTarget &&
			view.providerAuthoring {
			return true
		}
	}
	return false
}

// LookupMCPBySubject returns the compiled MCP surface named by a topology
// projection subject. Invalid, non-projection, and foreign-namespace subjects
// are missing, matching owner MCPPlacementForSubject not-found semantics.
func (catalog Catalog) LookupMCPBySubject(subject topology.SubjectID) (SurfaceView, bool) {
	if subject.Validate() != nil || subject.Kind() != topology.SubjectProjection {
		return SurfaceView{}, false
	}
	for _, view := range catalog.views {
		if view.namespace == subject.Namespace() {
			return view, true
		}
	}
	return SurfaceView{}, false
}

// MCPInOwnerOrder returns compiled MCP views in owner placement-catalog order.
// Surfaces() remains SurfaceID order; public help and authoring option lists
// use this projection so CLI order stays exact.
func (catalog Catalog) MCPInOwnerOrder() []SurfaceView {
	out := make([]SurfaceView, len(catalog.ownerOrder))
	for index, viewIndex := range catalog.ownerOrder {
		out[index] = catalog.views[viewIndex]
	}
	return out
}
