package profile

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

// Operation identifies one exact profile-routed host operation.
type Operation string

const (
	OperationWrite   Operation = "write"
	OperationRemove  Operation = "remove"
	OperationInstall Operation = "install"
	OperationRefresh Operation = "refresh"
)

// OperationRoute is one operation/subject-correlation to boundary adapter fact.
// It carries no placement path or execution outcome.
type OperationRoute struct {
	resourceKind           entity.Kind
	operation              Operation
	correlationID          string
	routeID                string
	adapterContractVersion string
}

func NewOperationRoute(
	resourceKind entity.Kind,
	operation Operation,
	correlationID string,
	routeID string,
	adapterContractVersion string,
) (OperationRoute, error) {
	route := OperationRoute{
		resourceKind:           resourceKind,
		operation:              operation,
		correlationID:          strings.TrimSpace(correlationID),
		routeID:                strings.TrimSpace(routeID),
		adapterContractVersion: strings.TrimSpace(adapterContractVersion),
	}
	if err := route.Validate(); err != nil {
		return OperationRoute{}, err
	}
	return route, nil
}

func (route OperationRoute) Validate() error {
	if _, err := entity.ParseKind(string(route.resourceKind)); err != nil {
		return err
	}
	switch route.operation {
	case OperationWrite, OperationRemove, OperationInstall, OperationRefresh:
	default:
		return fmt.Errorf("operation route operation %q is unsupported", route.operation)
	}
	for _, field := range []struct {
		label string
		value string
	}{
		{label: "correlation id", value: route.correlationID},
		{label: "route id", value: route.routeID},
		{label: "adapter contract version", value: route.adapterContractVersion},
	} {
		if err := validateProfileToken(field.label, field.value); err != nil {
			return err
		}
	}
	return nil
}

func (route OperationRoute) ResourceKind() entity.Kind { return route.resourceKind }
func (route OperationRoute) Operation() Operation      { return route.operation }
func (route OperationRoute) CorrelationID() string     { return route.correlationID }
func (route OperationRoute) RouteID() string           { return route.routeID }
func (route OperationRoute) AdapterContractVersion() string {
	return route.adapterContractVersion
}

func (route OperationRoute) Correlates(
	resourceKind entity.Kind,
	correlationID string,
	operation Operation,
) bool {
	return route.resourceKind == resourceKind &&
		route.correlationID == correlationID &&
		route.operation == operation
}

type aggregateRouteIDs struct {
	write  string
	remove string
}

var hookAggregateRouteIDs = map[aggregate.HookPlacementID]aggregateRouteIDs{
	aggregate.HookPlacementCodexProject:  {write: "codex-project-hooks.write_projection", remove: "codex-project-hooks.remove_projection"},
	aggregate.HookPlacementCodexGlobal:   {write: "codex-global-hooks.write_projection", remove: "codex-global-hooks.remove_projection"},
	aggregate.HookPlacementClaudeProject: {write: "claude-project-hooks.write_projection", remove: "claude-project-hooks.remove_projection"},
	aggregate.HookPlacementClaudeGlobal:  {write: "claude-global-hooks.write_projection", remove: "claude-global-hooks.remove_projection"},
}

var mcpAggregateRouteIDs = map[aggregate.MCPPlacementID]aggregateRouteIDs{
	aggregate.MCPPlacementClaudeProject:     {write: "claude-project-mcp-stdio.write_projection", remove: "claude-project-mcp-stdio.remove_binding"},
	aggregate.MCPPlacementClaudeGlobal:      {write: "claude-code-user-mcp-stdio.write_projection", remove: "claude-code-user-mcp-stdio.remove_binding"},
	aggregate.MCPPlacementAntigravityGlobal: {write: "antigravity-cli-global-mcp-command.write_projection", remove: "antigravity-cli-global-mcp-command.remove_binding"},
	aggregate.MCPPlacementOpenCodeProject:   {write: "opencode-project-mcp-local-command.write_projection", remove: "opencode-project-mcp-local-command.remove_binding"},
	aggregate.MCPPlacementOpenCodeGlobal:    {write: "opencode-global-mcp-local-command.write_projection", remove: "opencode-global-mcp-local-command.remove_binding"},
	aggregate.MCPPlacementCodexProject:      {write: "codex-project-mcp-stdio-command.write_projection", remove: "codex-project-mcp-stdio-command.remove_binding"},
	aggregate.MCPPlacementCodexGlobal:       {write: "codex-global-mcp-stdio-env-vars.write_projection", remove: "codex-global-mcp-stdio-env-vars.remove_binding"},
}

func aggregateOperationRoutesForTarget(selectedTarget target.Target) []OperationRoute {
	routes := make([]OperationRoute, 0)
	for _, placement := range aggregate.ImplementedHookPlacements() {
		if placement.Target() != selectedTarget {
			continue
		}
		ids, ok := hookAggregateRouteIDs[placement.ID()]
		if !ok {
			panic(fmt.Sprintf("Hook placement %q has no operation routes", placement.ID()))
		}
		routes = appendAggregateOperationRoutes(routes, entity.KindHook, string(placement.ID()), string(placement.CodecContractID()), ids)
	}
	for _, placement := range aggregate.ImplementedMCPPlacements() {
		if placement.Target() != selectedTarget {
			continue
		}
		ids, ok := mcpAggregateRouteIDs[placement.ID()]
		if !ok {
			panic(fmt.Sprintf("MCP placement %q has no operation routes", placement.ID()))
		}
		routes = appendAggregateOperationRoutes(routes, entity.KindMCPServer, string(placement.ID()), string(placement.CodecContractID()), ids)
	}
	return routes
}

func appendAggregateOperationRoutes(
	routes []OperationRoute,
	resourceKind entity.Kind,
	correlationID string,
	adapterContractVersion string,
	ids aggregateRouteIDs,
) []OperationRoute {
	return append(
		routes,
		mustOperationRoute(resourceKind, OperationWrite, correlationID, ids.write, adapterContractVersion),
		mustOperationRoute(resourceKind, OperationRemove, correlationID, ids.remove, adapterContractVersion),
	)
}

func validateAggregateOperationRouteCatalog() error {
	hookPlacements := aggregate.ImplementedHookPlacements()
	if len(hookAggregateRouteIDs) != len(hookPlacements) {
		return fmt.Errorf("Hook operation-route catalog has %d rows for %d placements", len(hookAggregateRouteIDs), len(hookPlacements))
	}
	for _, placement := range hookPlacements {
		if _, ok := hookAggregateRouteIDs[placement.ID()]; !ok {
			return fmt.Errorf("Hook placement %q has no operation-route row", placement.ID())
		}
	}
	mcpPlacements := aggregate.ImplementedMCPPlacements()
	if len(mcpAggregateRouteIDs) != len(mcpPlacements) {
		return fmt.Errorf("MCP operation-route catalog has %d rows for %d placements", len(mcpAggregateRouteIDs), len(mcpPlacements))
	}
	for _, placement := range mcpPlacements {
		if _, ok := mcpAggregateRouteIDs[placement.ID()]; !ok {
			return fmt.Errorf("MCP placement %q has no operation-route row", placement.ID())
		}
	}
	return nil
}

// ManagedPathOperationRoute resolves one route independently from placement
// data and verifies that every consumer profile selects the same contract.
func ManagedPathOperationRoute(
	placement SelectedManagedPathPlacement,
	operation Operation,
) (OperationRoute, error) {
	if err := placement.validate(); err != nil {
		return OperationRoute{}, err
	}
	var selected OperationRoute
	for index, consumer := range placement.ConsumerTargets() {
		route, ok := Profile(consumer).OperationRoute(placement.ResourceKind(), placement.ID(), operation)
		if !ok {
			return OperationRoute{}, fmt.Errorf(
				"target %q has no unique %s route for %s placement %q",
				consumer,
				operation,
				placement.ResourceKind(),
				placement.ID(),
			)
		}
		if index > 0 && route != selected {
			return OperationRoute{}, fmt.Errorf(
				"%s placement %q consumers select conflicting %s routes",
				placement.ResourceKind(),
				placement.ID(),
				operation,
			)
		}
		selected = route
	}
	return selected, nil
}

// HookAssetOperationRoute resolves one route independently from HookAsset placement data.
func HookAssetOperationRoute(placement HookAssetPlacement, operation Operation) (OperationRoute, error) {
	if err := placement.Validate(); err != nil {
		return OperationRoute{}, err
	}
	var selected OperationRoute
	for index, consumer := range placement.ConsumerTargets() {
		route, ok := Profile(consumer).OperationRoute(entity.KindHookAsset, placement.ID(), operation)
		if !ok {
			return OperationRoute{}, fmt.Errorf(
				"target %q has no unique %s route for HookAsset placement %q",
				consumer,
				operation,
				placement.ID(),
			)
		}
		if index > 0 && route != selected {
			return OperationRoute{}, fmt.Errorf(
				"HookAsset placement %q consumers select conflicting %s routes",
				placement.ID(),
				operation,
			)
		}
		selected = route
	}
	return selected, nil
}

func validateProfileToken(label string, value string) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be a stable token", label)
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'A' && character <= 'Z') ||
			(character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == '-' {
			continue
		}
		return fmt.Errorf("%s must be a stable token", label)
	}
	return nil
}
