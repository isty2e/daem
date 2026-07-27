package profile

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

type placementKey struct {
	selectedTarget target.Target
	resourceKind   entity.Kind
	scope          target.Scope
	path           string
}

type defaultPlacementKey struct {
	selectedTarget target.Target
	resourceKind   entity.Kind
	scope          target.Scope
}

type locationKey struct {
	selectedTarget target.Target
	resourceKind   entity.Kind
	scope          target.Scope
	path           string
}

type operationRouteKey struct {
	resourceKind  entity.Kind
	operation     Operation
	correlationID string
}

func validateProfileFacetCatalogs(
	placements []ManagedPathPlacement,
	discoveries []DiscoveryLocation,
	runtime []RuntimeLocation,
	routes []OperationRoute,
) error {
	placementIDs := make(map[string]ManagedPathPlacement, len(placements))
	placementsByTarget := make(map[placementKey]string)
	defaultPlacements := make(map[defaultPlacementKey]string)
	for _, placement := range placements {
		if err := placement.validate(); err != nil {
			return err
		}
		if previous, exists := placementIDs[placement.ID()]; exists {
			return fmt.Errorf(
				"duplicate placement id %q for roots %q and %q",
				placement.ID(),
				previous.Root(),
				placement.Root(),
			)
		}
		placementIDs[placement.ID()] = placement
		for _, selectedTarget := range placement.ConsumerTargets() {
			key := placementKey{selectedTarget, placement.ResourceKind(), placement.Scope(), placement.Root().String()}
			if previousID, exists := placementsByTarget[key]; exists {
				return fmt.Errorf(
					"target %q %s %s path %q is claimed by placements %q and %q",
					selectedTarget,
					placement.ResourceKind(),
					placement.Scope(),
					placement.Root(),
					previousID,
					placement.ID(),
				)
			}
			placementsByTarget[key] = placement.ID()
			if !supportCatalog[selectedTarget][placement.ResourceKind()].Supported() {
				return fmt.Errorf("placement %q targets unsupported %q/%q", placement.ID(), selectedTarget, placement.ResourceKind())
			}
			if placement.Default() {
				defaultKey := defaultPlacementKey{selectedTarget, placement.ResourceKind(), placement.Scope()}
				if previousID, exists := defaultPlacements[defaultKey]; exists {
					return fmt.Errorf(
						"target %q %s %s has default placements %q and %q",
						selectedTarget,
						placement.ResourceKind(),
						placement.Scope(),
						previousID,
						placement.ID(),
					)
				}
				defaultPlacements[defaultKey] = placement.ID()
			}
		}
	}

	discoveryKeys := make(map[locationKey]struct{}, len(discoveries))
	for _, location := range discoveries {
		if err := location.Validate(); err != nil {
			return err
		}
		key := locationKey{location.Target(), location.ResourceKind(), location.Scope(), location.Path()}
		if _, exists := discoveryKeys[key]; exists {
			return fmt.Errorf("duplicate discovery location for %q/%q/%q/%q", key.selectedTarget, key.resourceKind, key.scope, key.path)
		}
		discoveryKeys[key] = struct{}{}
		if !supportCatalog[location.Target()][location.ResourceKind()].Supported() {
			return fmt.Errorf("discovery location targets unsupported %q/%q", location.Target(), location.ResourceKind())
		}
	}

	runtimeKeys := make(map[locationKey]struct{}, len(runtime))
	for _, location := range runtime {
		if err := location.Validate(); err != nil {
			return err
		}
		key := locationKey{location.Target(), location.ResourceKind(), location.Scope(), location.Path()}
		if _, exists := runtimeKeys[key]; exists {
			return fmt.Errorf("duplicate runtime location for %q/%q/%q/%q", key.selectedTarget, key.resourceKind, key.scope, key.path)
		}
		runtimeKeys[key] = struct{}{}
		if !supportCatalog[location.Target()][location.ResourceKind()].Supported() {
			return fmt.Errorf("runtime location targets unsupported %q/%q", location.Target(), location.ResourceKind())
		}
	}

	routeKeys := make(map[operationRouteKey]struct{}, len(routes))
	for _, route := range routes {
		if err := route.Validate(); err != nil {
			return err
		}
		key := operationRouteKey{route.ResourceKind(), route.Operation(), route.CorrelationID()}
		if _, exists := routeKeys[key]; exists {
			return fmt.Errorf(
				"duplicate %s route for %s correlation %q",
				route.Operation(),
				route.ResourceKind(),
				route.CorrelationID(),
			)
		}
		routeKeys[key] = struct{}{}
	}

	for _, placement := range placements {
		for _, operation := range []Operation{OperationWrite, OperationRemove} {
			key := operationRouteKey{placement.ResourceKind(), operation, placement.ID()}
			if _, exists := routeKeys[key]; !exists {
				return fmt.Errorf("placement %q has no %s route", placement.ID(), operation)
			}
		}
		for _, selectedTarget := range placement.ConsumerTargets() {
			key := locationKey{selectedTarget, placement.ResourceKind(), placement.Scope(), placement.Root().String()}
			if _, exists := discoveryKeys[key]; !exists {
				return fmt.Errorf("placement %q has no corresponding discovery location for target %q", placement.ID(), selectedTarget)
			}
		}
	}
	return nil
}

func (profile TargetProfile) validate() error {
	if _, err := target.ParseTarget(string(profile.selectedTarget)); err != nil {
		return err
	}
	for _, resourceKind := range resourceKinds {
		support, ok := profile.supports[resourceKind]
		if !ok {
			return fmt.Errorf("resource %q has no support fact", resourceKind)
		}
		if support.Target() != profile.selectedTarget || support.ResourceKind() != resourceKind {
			return fmt.Errorf("resource %q support fact has mismatched identity", resourceKind)
		}
		if err := support.Validate(); err != nil {
			return err
		}
	}
	for _, placement := range profile.placements {
		if !targetSetContains(placement.consumerTargets, profile.selectedTarget) {
			return fmt.Errorf("placement %q does not include profile target %q", placement.ID(), profile.selectedTarget)
		}
		if !profile.Supports(placement.ResourceKind()) {
			return fmt.Errorf("placement %q belongs to unsupported resource %q", placement.ID(), placement.ResourceKind())
		}
		for _, operation := range []Operation{OperationWrite, OperationRemove} {
			if _, ok := profile.OperationRoute(placement.ResourceKind(), placement.ID(), operation); !ok {
				return fmt.Errorf("placement %q has no unique %s route in target profile", placement.ID(), operation)
			}
		}
	}
	for _, location := range profile.discoveries {
		if location.Target() != profile.selectedTarget || !profile.Supports(location.ResourceKind()) {
			return fmt.Errorf("discovery location %q is not admitted by profile %q", location.Path(), profile.selectedTarget)
		}
	}
	for _, location := range profile.runtime {
		if location.Target() != profile.selectedTarget || !profile.Supports(location.ResourceKind()) {
			return fmt.Errorf("runtime location %q is not admitted by profile %q", location.Path(), profile.selectedTarget)
		}
	}
	for _, capability := range profile.mcpRuntimeProbes {
		if err := capability.Validate(); err != nil {
			return err
		}
		placement := capability.Placement()
		if placement.Target() != profile.selectedTarget {
			return fmt.Errorf(
				"MCP runtime-probe capability %q belongs to target %q, not profile %q",
				placement.ID(),
				placement.Target(),
				profile.selectedTarget,
			)
		}
		if _, ok := profile.MCPPlacement(placement.ID()); !ok {
			return fmt.Errorf(
				"MCP runtime-probe capability %q has no placement in profile %q",
				placement.ID(),
				profile.selectedTarget,
			)
		}
	}
	return nil
}
