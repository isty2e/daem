package catalog

import (
	"github.com/isty2e/daem/internal/hostsurface"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

// SurfaceBinding joins one SurfaceKey to an owner-local MCP placement ID.
type SurfaceBinding struct {
	Key         hostsurface.SurfaceKey
	PlacementID aggregate.MCPPlacementID
}

// RoutePair is the write and remove actuation tokens for one placement.
type RoutePair struct {
	Write  string
	Remove string
}

// Seed is an explicit join of owner-local MCP catalogs. Empty Bindings derive
// one default MCP surface per placement.
type Seed struct {
	Bindings    []SurfaceBinding
	Placements  []aggregate.MCPPlacement
	Namespaces  []topologymcp.ProjectionNamespace
	Routes      map[aggregate.MCPPlacementID]RoutePair
	Probes      []profile.MCPRuntimeProbeCapability
	ProviderFor map[target.Target]struct{}
}

func productSeed() Seed {
	placements := aggregate.ImplementedMCPPlacements()
	routes := make(map[aggregate.MCPPlacementID]RoutePair, len(placements))
	for _, placement := range placements {
		write, remove, ok := profile.MCPAggregateRouteIDs(placement.ID())
		if !ok {
			panic("hostsurface catalog: missing MCP aggregate routes for " + string(placement.ID()))
		}
		routes[placement.ID()] = RoutePair{Write: write, Remove: remove}
	}
	providerFor := make(map[target.Target]struct{})
	for _, placement := range placements {
		if _, ok := profile.MCPProviderAuthoringProfileForTarget(placement.Target()); ok {
			providerFor[placement.Target()] = struct{}{}
		}
	}
	return Seed{
		Placements:  placements,
		Namespaces:  topologymcp.ImplementedProjectionNamespaces(),
		Routes:      routes,
		Probes:      profile.MCPRuntimeProbeCapabilities(),
		ProviderFor: providerFor,
	}
}
