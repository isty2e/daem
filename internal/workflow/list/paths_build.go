package listworkflow

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

var locationInventoryScopes = []target.Scope{target.ScopeProject, target.ScopeGlobal}

// BuildLocationInventory combines immutable compiled surface views with manifest
// request markers. Manifest facts can select admitted rows but cannot create
// new path or route authority.
func BuildLocationInventory(
	environment desired.Environment,
	selection targetselection.Selection,
) (LocationInventory, error) {
	if err := validateLocationInventoryTargets(availableTargets(environment), selection.Targets()); err != nil {
		return LocationInventory{}, err
	}
	manifestSelections, err := collectManifestLocationSelections(environment)
	if err != nil {
		return LocationInventory{}, err
	}

	rows := make([]LocationEntry, 0)
	for _, selectedTarget := range selection.Targets() {
		for _, scope := range locationInventoryScopes {
			if err := appendManagedResourceLocations(
				&rows,
				selectedTarget,
				scope,
				entity.KindInstructions,
				manifestSelections,
			); err != nil {
				return LocationInventory{}, err
			}
			if err := appendManagedResourceLocations(
				&rows,
				selectedTarget,
				scope,
				entity.KindSkill,
				manifestSelections,
			); err != nil {
				return LocationInventory{}, err
			}
			if err := appendHookLocations(
				&rows,
				selectedTarget,
				scope,
				manifestSelections,
			); err != nil {
				return LocationInventory{}, err
			}
			if err := appendMCPLocations(
				&rows,
				selectedTarget,
				scope,
				manifestSelections,
			); err != nil {
				return LocationInventory{}, err
			}
			if err := appendExtensionLocations(
				&rows,
				selectedTarget,
				scope,
				manifestSelections,
			); err != nil {
				return LocationInventory{}, err
			}
		}
	}
	for requested := range manifestSelections.unadmittedPlacements {
		if !selection.Includes(requested.target) {
			continue
		}
		if err := appendLocationEntry(&rows, locationEntryInput{
			kind: LocationUnsupported, selectedTarget: requested.target, scope: requested.scope,
			resourceKind: requested.resource, realization: LocationUnavailable,
			role: LocationRoleUnsupported, requested: true,
			selectionSource: LocationSelectionNotApplicable, source: LocationSourceManifest,
			reason: "requested-placement-not-admitted", detail: fmt.Sprintf("requested placement %q", requested.path),
		}); err != nil {
			return LocationInventory{}, err
		}
	}
	return newLocationInventory(rows)
}

func validateLocationInventoryTargets(
	available []target.Target,
	selected []target.Target,
) error {
	for _, selectedTarget := range selected {
		if !slices.Contains(available, selectedTarget) {
			return fmt.Errorf("location inventory target %q is not available in the manifest", selectedTarget)
		}
	}
	return nil
}

func appendManagedResourceLocations(
	rows *[]LocationEntry,
	selectedTarget target.Target,
	scope target.Scope,
	resourceKind entity.Kind,
	selections manifestLocationSelections,
) error {
	requestKey := resourceRequestKey{target: selectedTarget, scope: scope, resource: resourceKind}
	requested := selections.resources[requestKey]
	compiled := catalog.Product()
	views := compiled.ManagedPathViews(selectedTarget, scope, resourceKind)
	if len(views) == 0 {
		return appendLocationEntry(rows, locationEntryInput{
			kind: LocationUnsupported, selectedTarget: selectedTarget, scope: scope,
			resourceKind: resourceKind, realization: LocationUnavailable,
			role: LocationRoleUnsupported, requested: requested,
			selectionSource: LocationSelectionNotApplicable, source: LocationSourceProfile,
			reason: "not-implemented",
		})
	}
	for _, view := range views {
		placement := view.Placement()
		admission := view.Admission()
		selectionSource, selected := selections.selectedPaths[selectedPathKey{
			resourceRequestKey: requestKey,
			path:               placement.Root().String(),
		}]
		if !selected {
			if admission.Default() {
				selectionSource = LocationSelectionProfileDefault
			} else {
				selectionSource = LocationSelectionAlternative
			}
		}
		if err := appendLocationEntry(rows, locationEntryInput{
			kind: LocationPath, selectedTarget: selectedTarget, scope: scope,
			resourceKind: resourceKind, realization: LocationManagedPath,
			role: LocationRoleWrite, path: placement.Root().String(),
			selected: selected, requested: requested, defaultChoice: admission.Default(),
			selectionSource: selectionSource, source: LocationSourceProfile,
		}); err != nil {
			return err
		}
	}
	for _, location := range compiled.ManagedPathDiscoveryLocations(selectedTarget, scope, resourceKind) {
		if err := appendLocationEntry(rows, locationEntryInput{
			kind: LocationPath, selectedTarget: selectedTarget, scope: scope,
			resourceKind: resourceKind, realization: LocationHostDiscovery,
			role: LocationRoleDiscovery, path: location.Path(), requested: requested,
			selectionSource: LocationSelectionNotApplicable, source: LocationSourceProfile,
		}); err != nil {
			return err
		}
	}
	for _, location := range compiled.ManagedPathRuntimeLocations(selectedTarget, scope, resourceKind) {
		if err := appendLocationEntry(rows, locationEntryInput{
			kind: LocationPath, selectedTarget: selectedTarget, scope: scope,
			resourceKind: resourceKind, realization: LocationHostRuntime,
			role: LocationRoleRuntime, path: location.Path(), requested: requested,
			selectionSource: LocationSelectionNotApplicable, source: LocationSourceProfile,
		}); err != nil {
			return err
		}
	}
	return nil
}

func appendHookLocations(
	rows *[]LocationEntry,
	selectedTarget target.Target,
	scope target.Scope,
	selections manifestLocationSelections,
) error {
	requestKey := resourceRequestKey{target: selectedTarget, scope: scope, resource: entity.KindHook}
	requested := selections.resources[requestKey]
	compiled := catalog.Product()
	support, supportPresent := compiled.ResourceSupport(selectedTarget, entity.KindHook)
	if view, implemented := compiled.LookupHookCell(selectedTarget, scope); implemented {
		if err := appendLocationEntry(rows, locationEntryInput{
			kind: LocationPath, selectedTarget: selectedTarget, scope: scope,
			resourceKind: entity.KindHook, realization: LocationConfigContribution,
			role: LocationRoleConfig, path: view.Placement().AggregateRoot().String(),
			selected: requested, requested: requested,
			selectionSource: LocationSelectionNotApplicable, source: LocationSourceAggregate,
		}); err != nil {
			return err
		}
	} else {
		reason := "not-implemented"
		if supportPresent && !support.Supported() {
			reason = string(support.Reason())
		}
		if err := appendLocationEntry(rows, locationEntryInput{
			kind: LocationUnsupported, selectedTarget: selectedTarget, scope: scope,
			resourceKind: entity.KindHook, realization: LocationUnavailable,
			role: LocationRoleUnsupported, requested: requested,
			selectionSource: LocationSelectionNotApplicable, source: LocationSourceProfile,
			reason: reason,
		}); err != nil {
			return err
		}
	}

	assetKey := resourceRequestKey{target: selectedTarget, scope: scope, resource: entity.KindHookAsset}
	assetRequested := selections.hookAssetResources[assetKey]
	if view, implemented := compiled.LookupHookAssetCell(selectedTarget, scope); implemented {
		return appendLocationEntry(rows, locationEntryInput{
			kind: LocationPath, selectedTarget: selectedTarget, scope: scope,
			resourceKind: entity.KindHookAsset, realization: LocationInternalStore,
			role: LocationRoleInternal, path: view.Placement().Root().String(),
			selected: assetRequested, requested: assetRequested,
			selectionSource: LocationSelectionNotApplicable, source: LocationSourceProfile,
		})
	}
	reason := "not-implemented"
	if supportPresent {
		reason = string(support.Reason())
	}
	return appendLocationEntry(rows, locationEntryInput{
		kind: LocationUnsupported, selectedTarget: selectedTarget, scope: scope,
		resourceKind: entity.KindHookAsset, realization: LocationUnavailable,
		role: LocationRoleUnsupported, requested: assetRequested,
		selectionSource: LocationSelectionNotApplicable, source: LocationSourceProfile,
		reason: reason,
	})
}

func appendMCPLocations(
	rows *[]LocationEntry,
	selectedTarget target.Target,
	scope target.Scope,
	selections manifestLocationSelections,
) error {
	requestKey := resourceRequestKey{target: selectedTarget, scope: scope, resource: entity.KindMCPServer}
	requested := selections.resources[requestKey]
	view, implemented := catalog.Product().LookupMCP(selectedTarget, scope)
	if !implemented {
		return appendLocationEntry(rows, locationEntryInput{
			kind: LocationUnsupported, selectedTarget: selectedTarget, scope: scope,
			resourceKind: entity.KindMCPServer, realization: LocationUnavailable,
			role: LocationRoleUnsupported, requested: requested,
			selectionSource: LocationSelectionNotApplicable, source: LocationSourceAggregate,
			reason: "not-implemented",
		})
	}
	return appendLocationEntry(rows, locationEntryInput{
		kind: LocationPath, selectedTarget: selectedTarget, scope: scope,
		resourceKind: entity.KindMCPServer, realization: LocationConfigContribution,
		role: LocationRoleConfig, path: view.Placement().ConfigPath().String(),
		selected: requested, requested: requested,
		selectionSource: LocationSelectionNotApplicable, source: LocationSourceAggregate,
	})
}

func appendExtensionLocations(
	rows *[]LocationEntry,
	selectedTarget target.Target,
	scope target.Scope,
	selections manifestLocationSelections,
) error {
	compiled := catalog.Product()
	found := false
	seen := make(map[desiredextension.Carrier]struct{})
	for _, targetView := range compiled.ExtensionViewsForTarget(selectedTarget) {
		carrier := targetView.Carrier()
		if _, duplicate := seen[carrier]; duplicate {
			continue
		}
		seen[carrier] = struct{}{}
		found = true
		requested := selections.extensionRelations[extensionRequestKey{
			target: selectedTarget, scope: scope, carrier: carrier,
		}]
		view, admitted := compiled.LookupExtensionCell(selectedTarget, scope, carrier)
		if !admitted {
			if err := appendLocationEntry(rows, locationEntryInput{
				kind: LocationUnsupported, selectedTarget: selectedTarget, scope: scope,
				resourceKind: entity.KindExtension, variant: string(carrier), realization: LocationUnavailable,
				role: LocationRoleUnsupported, requested: requested,
				selectionSource: LocationSelectionNotApplicable, source: LocationSourceDelegatedProfile,
				reason: "scope-not-admitted",
			}); err != nil {
				return err
			}
			continue
		}
		for _, route := range view.RouteProfile().OperationRoutes() {
			if err := appendLocationEntry(rows, locationEntryInput{
				kind: LocationRoute, selectedTarget: selectedTarget, scope: scope,
				resourceKind: entity.KindExtension, variant: string(carrier), realization: LocationDelegatedRoute,
				role: LocationRoleDelegated, route: route.RouteID(), operation: route.Operation(),
				selected: requested, requested: requested,
				selectionSource: LocationSelectionNotApplicable, source: LocationSourceDelegatedProfile,
			}); err != nil {
				return err
			}
		}
	}
	if found {
		return nil
	}
	return appendLocationEntry(rows, locationEntryInput{
		kind: LocationUnsupported, selectedTarget: selectedTarget, scope: scope,
		resourceKind: entity.KindExtension, realization: LocationUnavailable,
		role:            LocationRoleUnsupported,
		selectionSource: LocationSelectionNotApplicable, source: LocationSourceDelegatedProfile,
		reason: "not-implemented",
	})
}

func appendLocationEntry(rows *[]LocationEntry, input locationEntryInput) error {
	entry, err := newLocationEntry(input)
	if err != nil {
		return err
	}
	*rows = append(*rows, entry)
	return nil
}
