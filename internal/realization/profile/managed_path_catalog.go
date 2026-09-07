package profile

// ManagedPathFacetCatalog is the immutable owner-local catalog for managed-path
// placement, target admission, discovery, runtime, and operation-route facts.
// It is not a target projection and carries no desired or observed state.
type ManagedPathFacetCatalog struct {
	placements  []ManagedPathPlacement
	admissions  []PlacementAdmission
	discoveries []DiscoveryLocation
	runtime     []RuntimeLocation
	routes      []OperationRoute
}

// StaticManagedPathFacets returns the complete Instruction and Skill
// managed-path fact catalog. Returned slices are independently owned.
func StaticManagedPathFacets() ManagedPathFacetCatalog {
	return ManagedPathFacetCatalog{
		placements: append(
			append([]ManagedPathPlacement(nil), instructionPlacements...),
			skillPlacements...,
		),
		admissions: append(
			append([]PlacementAdmission(nil), instructionPlacementAdmissions...),
			skillPlacementAdmissions...,
		),
		discoveries: append(
			append([]DiscoveryLocation(nil), instructionDiscoveries...),
			skillDiscoveries...,
		),
		runtime: append(
			append([]RuntimeLocation(nil), instructionRuntimeLocations...),
			skillRuntimeLocations...,
		),
		routes: append(instructionOperationRoutes(), skillOperationRoutes()...),
	}
}

// Placements returns physical managed-path facts in owner catalog order.
func (catalog ManagedPathFacetCatalog) Placements() []ManagedPathPlacement {
	return append([]ManagedPathPlacement(nil), catalog.placements...)
}

// Admissions returns target-relative placement admissions in owner catalog order.
func (catalog ManagedPathFacetCatalog) Admissions() []PlacementAdmission {
	return append([]PlacementAdmission(nil), catalog.admissions...)
}

// Discoveries returns host-visible discovery facts in owner catalog order.
func (catalog ManagedPathFacetCatalog) Discoveries() []DiscoveryLocation {
	return append([]DiscoveryLocation(nil), catalog.discoveries...)
}

// RuntimeLocations returns runtime-only lookup facts in owner catalog order.
func (catalog ManagedPathFacetCatalog) RuntimeLocations() []RuntimeLocation {
	return append([]RuntimeLocation(nil), catalog.runtime...)
}

// OperationRoutes returns managed-path route facts in owner catalog order.
func (catalog ManagedPathFacetCatalog) OperationRoutes() []OperationRoute {
	return append([]OperationRoute(nil), catalog.routes...)
}
