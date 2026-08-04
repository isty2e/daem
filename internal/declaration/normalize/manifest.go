package normalize

import (
	"fmt"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/desired"
)

// Manifest normalizes one decoded declaration into canonical desired state.
func Manifest(raw declaration.Manifest) (desired.Environment, error) {
	if raw.Version != declaration.CurrentManifestVersion {
		return desired.Environment{}, fmt.Errorf("unsupported manifest version %d", raw.Version)
	}

	targets, err := normalizeTargets(raw.Targets, "targets")
	if err != nil {
		return desired.Environment{}, err
	}

	defaults, err := normalizeDefaults(raw.Defaults)
	if err != nil {
		return desired.Environment{}, err
	}

	skills, skillSets, err := normalizeSkills(raw.Skills, raw.SkillGroups, targets, defaults)
	if err != nil {
		return desired.Environment{}, err
	}

	hooks, err := normalizeHooks(raw.Hooks, targets, defaults)
	if err != nil {
		return desired.Environment{}, err
	}

	hookAssets, err := normalizeHookAssets(raw.HookAssets, defaults)
	if err != nil {
		return desired.Environment{}, err
	}

	instructions, err := normalizeInstructions(raw.Instructions, targets, defaults)
	if err != nil {
		return desired.Environment{}, err
	}

	mcpServers, err := normalizeMCPServers(raw.MCPServers, targets, defaults)
	if err != nil {
		return desired.Environment{}, err
	}

	extensions, err := normalizeExtensions(raw.Extensions, targets, defaults)
	if err != nil {
		return desired.Environment{}, err
	}

	environment, err := desired.New(desired.Spec{
		Targets:      targets,
		Defaults:     defaults,
		Skills:       skills,
		SkillSets:    skillSets,
		Hooks:        hooks,
		HookAssets:   hookAssets,
		Instructions: instructions,
		MCPServers:   mcpServers,
		Extensions:   extensions,
	})
	if err != nil {
		return desired.Environment{}, fmt.Errorf("desired environment: %w", err)
	}
	return environment, nil
}
