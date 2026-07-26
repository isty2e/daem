package manifest

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
)

// ValidatePlacement rejects manifest resources whose scope cannot be projected from the selected path context.
func ValidatePlacement(paths daempaths.Paths, environment desired.Environment) error {
	if paths.ProjectPlacementAllowed() {
		return nil
	}

	for _, instruction := range environment.Instructions() {
		if instruction.Scope() == target.ScopeProject {
			return projectScopeFromUserDefaultError(paths, fmt.Sprintf("instruction %q", instruction.ID().Name()))
		}
	}
	for _, skill := range environment.Skills() {
		if skill.Scope() == target.ScopeProject {
			return projectScopeFromUserDefaultError(paths, fmt.Sprintf("skill %q", skill.ID().Name()))
		}
	}
	for index, skillGroup := range environment.SkillSets() {
		if skillGroup.Scope() == target.ScopeProject {
			return projectScopeFromUserDefaultError(paths, fmt.Sprintf("skill_group[%d]", index))
		}
	}
	for _, hook := range environment.Hooks() {
		if hook.Scope() == target.ScopeProject {
			return projectScopeFromUserDefaultError(paths, fmt.Sprintf("hook %q", hook.ID().Name()))
		}
	}
	for _, asset := range environment.HookAssets() {
		if asset.Scope() == target.ScopeProject {
			return projectScopeFromUserDefaultError(paths, fmt.Sprintf("hook_asset %q", asset.ID().Name()))
		}
	}
	for _, server := range environment.MCPServers() {
		for _, binding := range server.Bindings() {
			if binding.Scope() == target.ScopeProject {
				return projectScopeFromUserDefaultError(paths, fmt.Sprintf("mcp_server %q", server.ID().Name()))
			}
		}
	}
	for _, extension := range environment.Extensions() {
		if extension.Scope() == target.ScopeProject {
			return projectScopeFromUserDefaultError(paths, fmt.Sprintf("extension %q", extension.ID().Name()))
		}
	}

	return nil
}

func projectScopeFromUserDefaultError(paths daempaths.Paths, resource string) error {
	return fmt.Errorf(
		"project-scoped %s requires a project manifest; selected manifest %s is the OS user config manifest; use --manifest ./daem.toml or set scope = %q",
		resource,
		paths.ManifestPath,
		target.ScopeGlobal,
	)
}
