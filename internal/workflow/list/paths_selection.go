package listworkflow

import (
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

type resourceRequestKey struct {
	target   target.Target
	scope    target.Scope
	resource entity.Kind
}

type selectedPathKey struct {
	resourceRequestKey
	path string
}

type extensionRequestKey struct {
	target  target.Target
	scope   target.Scope
	carrier desiredextension.Carrier
}

type requestedPlacement struct {
	target   target.Target
	scope    target.Scope
	resource entity.Kind
	path     string
}

type manifestLocationSelections struct {
	resources            map[resourceRequestKey]bool
	selectedPaths        map[selectedPathKey]LocationSelectionSource
	extensionRelations   map[extensionRequestKey]bool
	hookAssetResources   map[resourceRequestKey]bool
	unadmittedPlacements map[requestedPlacement]struct{}
}

func collectManifestLocationSelections(environment desired.Environment) (manifestLocationSelections, error) {
	result := manifestLocationSelections{
		resources:            make(map[resourceRequestKey]bool),
		selectedPaths:        make(map[selectedPathKey]LocationSelectionSource),
		extensionRelations:   make(map[extensionRequestKey]bool),
		hookAssetResources:   make(map[resourceRequestKey]bool),
		unadmittedPlacements: make(map[requestedPlacement]struct{}),
	}
	for _, value := range environment.Instructions() {
		for _, selectedTarget := range value.Targets() {
			key := resourceRequestKey{target: selectedTarget, scope: value.Scope(), resource: entity.KindInstructions}
			result.resources[key] = true
			if err := result.selectManagedDefault(key); err != nil {
				return manifestLocationSelections{}, err
			}
		}
	}
	for _, value := range environment.Skills() {
		if err := result.selectSkill(
			value.Targets(),
			value.Scope(),
			value.TargetPlacements(),
		); err != nil {
			return manifestLocationSelections{}, err
		}
	}
	for _, value := range environment.Hooks() {
		for _, selectedTarget := range value.Targets() {
			key := resourceRequestKey{target: selectedTarget, scope: value.Scope(), resource: entity.KindHook}
			result.resources[key] = true
			if len(value.AssetReferences()) != 0 {
				result.hookAssetResources[resourceRequestKey{
					target: selectedTarget, scope: value.Scope(), resource: entity.KindHookAsset,
				}] = true
			}
		}
	}
	for _, server := range environment.MCPServers() {
		for _, binding := range server.Bindings() {
			result.resources[resourceRequestKey{
				target: binding.Target(), scope: binding.Scope(), resource: entity.KindMCPServer,
			}] = true
		}
	}
	for _, value := range environment.Extensions() {
		result.extensionRelations[extensionRequestKey{
			target: value.Target(), scope: value.Scope(), carrier: value.Carrier(),
		}] = true
		result.resources[resourceRequestKey{
			target: value.Target(), scope: value.Scope(), resource: entity.KindExtension,
		}] = true
	}
	return result, nil
}

func (selections *manifestLocationSelections) selectManagedDefault(key resourceRequestKey) error {
	placement, err := profile.Profile(key.target).DefaultPlacement(key.resource, key.scope)
	if err != nil {
		return err
	}
	selections.recordSelectedPath(
		selectedPathKey{resourceRequestKey: key, path: placement.Root().String()},
		LocationSelectionManifestDefault,
	)
	return nil
}

func (selections *manifestLocationSelections) selectSkill(
	targets []target.Target,
	scope target.Scope,
	overrides map[target.Target]desiredskill.TargetPlacement,
) error {
	for _, selectedTarget := range targets {
		key := resourceRequestKey{target: selectedTarget, scope: scope, resource: entity.KindSkill}
		selections.resources[key] = true
		override, explicit := overrides[selectedTarget]
		if !explicit {
			if err := selections.selectManagedDefault(key); err != nil {
				return err
			}
			continue
		}
		requestedRoot := override.InstallTo()
		if _, admitted := profile.Profile(selectedTarget).PlacementAt(entity.KindSkill, scope, requestedRoot); !admitted {
			selections.unadmittedPlacements[requestedPlacement{
				target: selectedTarget, scope: scope, resource: entity.KindSkill, path: requestedRoot,
			}] = struct{}{}
			continue
		}
		selections.recordSelectedPath(
			selectedPathKey{resourceRequestKey: key, path: requestedRoot},
			LocationSelectionManifestExplicit,
		)
	}
	return nil
}

func (selections *manifestLocationSelections) recordSelectedPath(
	key selectedPathKey,
	source LocationSelectionSource,
) {
	existing := selections.selectedPaths[key]
	if existing == LocationSelectionManifestExplicit {
		return
	}
	selections.selectedPaths[key] = source
}
