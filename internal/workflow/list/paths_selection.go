package listworkflow

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredinstructions "github.com/isty2e/daem/internal/desired/instructions"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/realization"
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
		if err := result.selectInstructions(
			value.Targets(),
			value.Scope(),
			value.Renderings(),
		); err != nil {
			return manifestLocationSelections{}, err
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
	for _, value := range environment.SkillSets() {
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
	view, ok := hostsurfacecatalog.Product().ManagedPathDefault(key.target, key.scope, key.resource)
	if !ok {
		return fmt.Errorf(
			"%s target %q scope %q has no default placement",
			key.resource,
			key.target,
			key.scope,
		)
	}
	selections.recordSelectedPath(
		selectedPathKey{resourceRequestKey: key, path: view.Placement().Root().String()},
		LocationSelectionManifestDefault,
	)
	return nil
}

func (selections *manifestLocationSelections) selectInstructions(
	targets []target.Target,
	scope target.Scope,
	renderings map[target.Target]desiredinstructions.Rendering,
) error {
	for _, selectedTarget := range targets {
		key := resourceRequestKey{target: selectedTarget, scope: scope, resource: entity.KindInstructions}
		selections.resources[key] = true

		renderTo := ""
		if rendering, present := renderings[selectedTarget]; present {
			renderTo = rendering.RenderTo()
		}
		if renderTo == "" {
			if err := selections.selectManagedDefault(key); err != nil {
				return err
			}
			continue
		}
		compiled := hostsurfacecatalog.Product()
		defaultView, ok := compiled.ManagedPathDefault(selectedTarget, scope, entity.KindInstructions)
		if !ok {
			return fmt.Errorf(
				"%s target %q scope %q has no default placement",
				entity.KindInstructions,
				selectedTarget,
				scope,
			)
		}
		destination, err := profile.ResolveManagedFileRelativePath(
			scope,
			defaultView.Placement().Root(),
			renderTo,
		)
		if err != nil {
			selections.unadmittedPlacements[requestedPlacement{
				target: selectedTarget, scope: scope, resource: entity.KindInstructions, path: renderTo,
			}] = struct{}{}
			continue
		}
		view, admitted := compiled.ManagedPathAt(
			selectedTarget,
			scope,
			entity.KindInstructions,
			destination.String(),
		)
		if !admitted || view.Placement().ContentKind() != realization.PathProjectionFile {
			selections.unadmittedPlacements[requestedPlacement{
				target: selectedTarget, scope: scope, resource: entity.KindInstructions, path: renderTo,
			}] = struct{}{}
			continue
		}
		selections.recordSelectedPath(
			selectedPathKey{resourceRequestKey: key, path: view.Placement().Root().String()},
			LocationSelectionManifestExplicit,
		)
	}
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
		if _, admitted := hostsurfacecatalog.Product().ManagedPathAt(
			selectedTarget,
			scope,
			entity.KindSkill,
			requestedRoot,
		); !admitted {
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
