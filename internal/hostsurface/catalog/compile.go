package catalog

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/hostsurface"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

type targetScope struct {
	target target.Target
	scope  target.Scope
}

// Compile joins owner-local MCP catalogs into an immutable surface snapshot.
func Compile(seed Seed) (Catalog, error) {
	placements, err := uniquePlacements(seed.Placements)
	if err != nil {
		return Catalog{}, err
	}
	namespaces, err := uniqueNamespaces(seed.Namespaces)
	if err != nil {
		return Catalog{}, err
	}
	bindings, err := deriveBindings(seed.Bindings, seed.Placements)
	if err != nil {
		return Catalog{}, err
	}

	referencedPlacements := make(map[aggregate.MCPPlacementID]struct{}, len(bindings))
	referencedNamespaces := make(map[targetScope]struct{}, len(bindings))
	views := make([]SurfaceView, 0, len(bindings))
	seenKeys := make(map[hostsurface.SurfaceKey]struct{}, len(bindings))

	for _, binding := range bindings {
		if err := binding.Key.Validate(); err != nil {
			return Catalog{}, fmt.Errorf("host-surface catalog: %w", err)
		}
		if _, duplicate := seenKeys[binding.Key]; duplicate {
			return Catalog{}, fmt.Errorf(
				"host-surface catalog: duplicate surface key %s/%s/%s/%s",
				binding.Key.Target(),
				binding.Key.Scope(),
				binding.Key.Kind(),
				binding.Key.Variant(),
			)
		}
		seenKeys[binding.Key] = struct{}{}

		placement, ok := placements[binding.PlacementID]
		if !ok {
			return Catalog{}, fmt.Errorf(
				"host-surface catalog: surface %s/%s missing placement %q",
				binding.Key.Target(),
				binding.Key.Scope(),
				binding.PlacementID,
			)
		}
		namespaceKey := targetScope{target: binding.Key.Target(), scope: binding.Key.Scope()}
		namespace, ok := namespaces[namespaceKey]
		if !ok {
			return Catalog{}, fmt.Errorf(
				"host-surface catalog: surface %s/%s missing topology namespace",
				binding.Key.Target(),
				binding.Key.Scope(),
			)
		}
		routes, ok := seed.Routes[binding.PlacementID]
		if !ok || routes.Write == "" || routes.Remove == "" {
			return Catalog{}, fmt.Errorf(
				"host-surface catalog: placement %q missing write and remove routes",
				binding.PlacementID,
			)
		}
		id, err := hostsurface.NewSurfaceID(binding.Key)
		if err != nil {
			return Catalog{}, fmt.Errorf("host-surface catalog: %w", err)
		}
		if id.String() == string(binding.PlacementID) {
			return Catalog{}, fmt.Errorf(
				"host-surface catalog: surface ID collides with placement ID %q",
				binding.PlacementID,
			)
		}

		view := SurfaceView{
			id:                id,
			key:               binding.Key,
			placement:         placement,
			namespace:         namespace,
			writeRouteID:      routes.Write,
			removeRouteID:     routes.Remove,
			providerAuthoring: seed.ProviderFor != nil && mapContains(seed.ProviderFor, binding.Key.Target()),
		}
		views = append(views, view)
		referencedPlacements[binding.PlacementID] = struct{}{}
		referencedNamespaces[namespaceKey] = struct{}{}
	}

	if err := rejectExtraPlacements(seed.Placements, referencedPlacements); err != nil {
		return Catalog{}, err
	}
	if err := rejectExtraNamespaces(seed.Namespaces, referencedNamespaces); err != nil {
		return Catalog{}, err
	}
	if err := rejectExtraRoutes(seed.Routes, referencedPlacements); err != nil {
		return Catalog{}, err
	}
	probes, err := uniqueProbes(seed.Probes, referencedPlacements)
	if err != nil {
		return Catalog{}, err
	}
	for index := range views {
		if probe, ok := probes[views[index].placement.ID()]; ok {
			views[index].runtimeProbe = probe
			views[index].hasRuntimeProbe = true
		}
	}

	slices.SortFunc(views, func(left SurfaceView, right SurfaceView) int {
		return hostsurface.CompareIDs(left.id, right.id)
	})
	catalog := Catalog{
		views: views,
		byID:  make(map[hostsurface.SurfaceID]int, len(views)),
		byKey: make(map[hostsurface.SurfaceKey]int, len(views)),
	}
	for index, view := range views {
		if _, exists := catalog.byID[view.id]; exists {
			return Catalog{}, fmt.Errorf("host-surface catalog: duplicate surface ID %q", view.id)
		}
		catalog.byID[view.id] = index
		catalog.byKey[view.key] = index
	}
	return catalog, nil
}

func deriveBindings(
	bindings []SurfaceBinding,
	placements []aggregate.MCPPlacement,
) ([]SurfaceBinding, error) {
	if len(bindings) > 0 {
		out := make([]SurfaceBinding, len(bindings))
		copy(out, bindings)
		return out, nil
	}
	out := make([]SurfaceBinding, 0, len(placements))
	for _, placement := range placements {
		key, err := hostsurface.MCPSurfaceKey(placement.Target(), placement.Scope())
		if err != nil {
			return nil, fmt.Errorf("host-surface catalog: %w", err)
		}
		out = append(out, SurfaceBinding{Key: key, PlacementID: placement.ID()})
	}
	return out, nil
}

func uniquePlacements(
	placements []aggregate.MCPPlacement,
) (map[aggregate.MCPPlacementID]aggregate.MCPPlacement, error) {
	out := make(map[aggregate.MCPPlacementID]aggregate.MCPPlacement, len(placements))
	for _, placement := range placements {
		if err := placement.Validate(); err != nil {
			return nil, fmt.Errorf("host-surface catalog: %w", err)
		}
		if _, duplicate := out[placement.ID()]; duplicate {
			return nil, fmt.Errorf("host-surface catalog: duplicate placement %q", placement.ID())
		}
		out[placement.ID()] = placement
	}
	return out, nil
}

func uniqueNamespaces(
	namespaces []topologymcp.ProjectionNamespace,
) (map[targetScope]string, error) {
	out := make(map[targetScope]string, len(namespaces))
	seenTokens := make(map[string]targetScope, len(namespaces))
	for _, row := range namespaces {
		key := targetScope{target: row.Target(), scope: row.Scope()}
		if row.Namespace() == "" {
			return nil, fmt.Errorf(
				"host-surface catalog: empty topology namespace for %s/%s",
				row.Target(),
				row.Scope(),
			)
		}
		if _, duplicate := out[key]; duplicate {
			return nil, fmt.Errorf(
				"host-surface catalog: duplicate topology namespace for %s/%s",
				row.Target(),
				row.Scope(),
			)
		}
		if previous, exists := seenTokens[row.Namespace()]; exists {
			return nil, fmt.Errorf(
				"host-surface catalog: topology namespace %q shared by %s/%s and %s/%s",
				row.Namespace(),
				previous.target,
				previous.scope,
				row.Target(),
				row.Scope(),
			)
		}
		out[key] = row.Namespace()
		seenTokens[row.Namespace()] = key
	}
	return out, nil
}

func uniqueProbes(
	probes []profile.MCPRuntimeProbeCapability,
	referenced map[aggregate.MCPPlacementID]struct{},
) (map[aggregate.MCPPlacementID]profile.MCPRuntimeProbeCapability, error) {
	out := make(map[aggregate.MCPPlacementID]profile.MCPRuntimeProbeCapability, len(probes))
	for _, probe := range probes {
		if err := probe.Validate(); err != nil {
			return nil, fmt.Errorf("host-surface catalog: %w", err)
		}
		id := probe.Placement().ID()
		if _, known := referenced[id]; !known {
			return nil, fmt.Errorf(
				"host-surface catalog: runtime probe for unreferenced placement %q",
				id,
			)
		}
		if _, duplicate := out[id]; duplicate {
			return nil, fmt.Errorf("host-surface catalog: duplicate runtime probe for %q", id)
		}
		out[id] = probe
	}
	return out, nil
}

func rejectExtraPlacements(
	placements []aggregate.MCPPlacement,
	referenced map[aggregate.MCPPlacementID]struct{},
) error {
	for _, placement := range placements {
		if _, ok := referenced[placement.ID()]; !ok {
			return fmt.Errorf("host-surface catalog: unused placement %q", placement.ID())
		}
	}
	return nil
}

func rejectExtraNamespaces(
	namespaces []topologymcp.ProjectionNamespace,
	referenced map[targetScope]struct{},
) error {
	for _, row := range namespaces {
		key := targetScope{target: row.Target(), scope: row.Scope()}
		if _, ok := referenced[key]; !ok {
			return fmt.Errorf(
				"host-surface catalog: unused topology namespace %s/%s",
				row.Target(),
				row.Scope(),
			)
		}
	}
	return nil
}

func rejectExtraRoutes(
	routes map[aggregate.MCPPlacementID]RoutePair,
	referenced map[aggregate.MCPPlacementID]struct{},
) error {
	for id := range routes {
		if _, ok := referenced[id]; !ok {
			return fmt.Errorf("host-surface catalog: unused routes for placement %q", id)
		}
	}
	return nil
}

func mapContains(values map[target.Target]struct{}, selected target.Target) bool {
	_, ok := values[selected]
	return ok
}
