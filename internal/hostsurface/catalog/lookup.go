package catalog

import (
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/hostsurface"
	"github.com/isty2e/daem/internal/target"
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
