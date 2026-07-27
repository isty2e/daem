package profile

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
)

var resourceKinds = []entity.Kind{
	entity.KindInstructions,
	entity.KindSkill,
	entity.KindHook,
}

var supportCatalog = map[target.Target]map[entity.Kind]Support{
	target.TargetCodex: {
		entity.KindInstructions: mustSupported(target.TargetCodex, entity.KindInstructions),
		entity.KindSkill:        mustSupported(target.TargetCodex, entity.KindSkill),
		entity.KindHook:         mustSupported(target.TargetCodex, entity.KindHook),
	},
	target.TargetClaudeCode: {
		entity.KindInstructions: mustSupported(target.TargetClaudeCode, entity.KindInstructions),
		entity.KindSkill:        mustSupported(target.TargetClaudeCode, entity.KindSkill),
		entity.KindHook:         mustSupported(target.TargetClaudeCode, entity.KindHook),
	},
	target.TargetOpenCode: {
		entity.KindInstructions: mustSupported(target.TargetOpenCode, entity.KindInstructions),
		entity.KindSkill:        mustSupported(target.TargetOpenCode, entity.KindSkill),
		entity.KindHook:         mustUnsupported(target.TargetOpenCode, entity.KindHook, UnsupportedReasonBridgeRequired),
	},
	target.TargetPi: {
		entity.KindInstructions: mustSupported(target.TargetPi, entity.KindInstructions),
		entity.KindSkill:        mustSupported(target.TargetPi, entity.KindSkill),
		entity.KindHook:         mustUnsupported(target.TargetPi, entity.KindHook, UnsupportedReasonBridgeRequired),
	},
	target.TargetAntigravityCLI: {
		entity.KindInstructions: mustSupported(target.TargetAntigravityCLI, entity.KindInstructions),
		entity.KindSkill:        mustSupported(target.TargetAntigravityCLI, entity.KindSkill),
		entity.KindHook:         mustUnsupported(target.TargetAntigravityCLI, entity.KindHook, UnsupportedReasonDirectCLIRouteNotAdmitted),
	},
}

func mustSupported(selectedTarget target.Target, resourceKind entity.Kind) Support {
	support, err := NewSupported(selectedTarget, resourceKind)
	if err != nil {
		panic(err)
	}
	return support
}

func mustUnsupported(
	selectedTarget target.Target,
	resourceKind entity.Kind,
	reason UnsupportedReason,
) Support {
	support, err := NewUnsupported(selectedTarget, resourceKind, reason)
	if err != nil {
		panic(err)
	}
	return support
}

func mustManagedPathPlacement(input ManagedPathPlacementInput) ManagedPathPlacement {
	placement, err := NewManagedPathPlacement(input)
	if err != nil {
		panic(err)
	}
	return placement
}

func mustDiscoveryLocation(
	selectedTarget target.Target,
	resourceKind entity.Kind,
	scope target.Scope,
	locationPath string,
	priority int,
) DiscoveryLocation {
	location, err := NewDiscoveryLocation(
		selectedTarget,
		resourceKind,
		scope,
		locationPath,
		priority,
		ImportPolicyInclude,
	)
	if err != nil {
		panic(err)
	}
	return location
}

func mustRuntimeLocation(
	selectedTarget target.Target,
	resourceKind entity.Kind,
	scope target.Scope,
	locationPath string,
) RuntimeLocation {
	location, err := NewRuntimeLocation(selectedTarget, resourceKind, scope, locationPath)
	if err != nil {
		panic(err)
	}
	return location
}

func managedPathOperationRoutes(
	placements []ManagedPathPlacement,
	writeRouteID string,
	removeRouteID string,
	adapterContractVersion string,
) []OperationRoute {
	routes := make([]OperationRoute, 0, len(placements)*2)
	for _, placement := range placements {
		routes = append(
			routes,
			mustOperationRoute(
				placement.ResourceKind(),
				OperationWrite,
				placement.ID(),
				writeRouteID,
				adapterContractVersion,
			),
			mustOperationRoute(
				placement.ResourceKind(),
				OperationRemove,
				placement.ID(),
				removeRouteID,
				adapterContractVersion,
			),
		)
	}
	return routes
}

func mustOperationRoute(
	resourceKind entity.Kind,
	operation Operation,
	correlationID string,
	routeID string,
	adapterContractVersion string,
) OperationRoute {
	route, err := NewOperationRoute(resourceKind, operation, correlationID, routeID, adapterContractVersion)
	if err != nil {
		panic(err)
	}
	return route
}

func profileOperationRoutes() []OperationRoute {
	routes := append(instructionOperationRoutes(), skillOperationRoutes()...)
	routes = append(routes, hookAssetOperationRoutes()...)
	for _, selectedTarget := range target.SupportedTargets() {
		routes = append(routes, aggregateOperationRoutesForTarget(selectedTarget)...)
	}
	for _, delegated := range delegatedRouteProfiles {
		routes = append(routes, delegated.OperationRoutes()...)
	}
	return routes
}

func validateStaticCatalog() error {
	if err := validateAggregateOperationRouteCatalog(); err != nil {
		return err
	}
	if err := validateMCPRuntimeProbeCapabilityCatalog(mcpRuntimeProbeCapabilityCatalog); err != nil {
		return err
	}
	for _, selectedTarget := range target.SupportedTargets() {
		facts, ok := supportCatalog[selectedTarget]
		if !ok {
			return fmt.Errorf("target %q has no support catalog", selectedTarget)
		}
		for _, resourceKind := range resourceKinds {
			fact, ok := facts[resourceKind]
			if !ok {
				return fmt.Errorf("target %q resource %q has no support fact", selectedTarget, resourceKind)
			}
			if err := fact.Validate(); err != nil {
				return err
			}
		}
	}
	return validateProfileFacetCatalogs(
		append(append([]ManagedPathPlacement(nil), instructionPlacements...), skillPlacements...),
		append(append([]DiscoveryLocation(nil), instructionDiscoveries...), skillDiscoveries...),
		append(append([]RuntimeLocation(nil), instructionRuntimeLocations...), skillRuntimeLocations...),
		profileOperationRoutes(),
	)
}

func init() {
	if err := validateStaticCatalog(); err != nil {
		panic(err)
	}
}

func profileRealizations(
	supports map[entity.Kind]Support,
	mcpCount int,
	delegatedCount int,
) map[entity.Kind]realization.RealizationKind {
	result := make(map[entity.Kind]realization.RealizationKind)
	if supports[entity.KindInstructions].Supported() {
		result[entity.KindInstructions] = realization.RealizationManagedPathProjection
	}
	if supports[entity.KindSkill].Supported() {
		result[entity.KindSkill] = realization.RealizationManagedPathProjection
	}
	if supports[entity.KindHook].Supported() {
		result[entity.KindHook] = realization.RealizationManagedAggregateContribution
		result[entity.KindHookAsset] = realization.RealizationManagedPathProjection
	}
	if mcpCount > 0 {
		result[entity.KindMCPServer] = realization.RealizationManagedAggregateContribution
	}
	if delegatedCount > 0 {
		result[entity.KindExtension] = realization.RealizationDelegatedRelation
	}
	return result
}
