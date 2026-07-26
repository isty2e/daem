package desired

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/desired/hook"
	"github.com/isty2e/daem/internal/desired/hookasset"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

// Defaults records normalized manifest defaults after boundary defaulting.
type Defaults struct {
	scope       target.Scope
	installMode skill.InstallMode
}

// NewDefaults constructs canonical manifest defaults.
func NewDefaults(scope target.Scope, installMode skill.InstallMode) (Defaults, error) {
	parsedScope, err := target.ParseScope(string(scope))
	if err != nil {
		return Defaults{}, err
	}
	parsedMode, err := skill.ParseInstallMode(string(installMode))
	if err != nil {
		return Defaults{}, err
	}
	return Defaults{scope: parsedScope, installMode: parsedMode}, nil
}

// Scope returns the normalized default scope.
func (defaults Defaults) Scope() target.Scope { return defaults.scope }

// InstallMode returns the normalized default skill install mode.
func (defaults Defaults) InstallMode() skill.InstallMode { return defaults.installMode }

func (defaults Defaults) validate() error {
	_, err := NewDefaults(defaults.scope, defaults.installMode)
	return err
}

// Spec is constructor input for one canonical Environment.
type Spec struct {
	Targets      []target.Target
	Defaults     Defaults
	Skills       []skill.Skill
	SkillSets    []skill.SkillSet
	Hooks        []hook.Hook
	HookAssets   []hookasset.HookAsset
	Instructions []instructions.Instructions
	MCPServers   []mcp.Server
	Extensions   []extension.Extension
}

// Environment is the immutable canonical desired aggregate root.
type Environment struct {
	targets      target.Set
	defaults     Defaults
	skills       []skill.Skill
	skillSets    []skill.SkillSet
	hooks        []hook.Hook
	hookAssets   []hookasset.HookAsset
	instructions []instructions.Instructions
	mcpServers   []mcp.Server
	extensions   []extension.Extension
}

// New constructs and validates one canonical desired environment.
func New(spec Spec) (Environment, error) {
	targets, err := target.NewSet(spec.Targets)
	if err != nil {
		return Environment{}, fmt.Errorf("environment targets: %w", err)
	}
	if err := spec.Defaults.validate(); err != nil {
		return Environment{}, fmt.Errorf("environment defaults: %w", err)
	}

	environment := Environment{
		targets:      targets,
		defaults:     spec.Defaults,
		skills:       append([]skill.Skill(nil), spec.Skills...),
		skillSets:    append([]skill.SkillSet(nil), spec.SkillSets...),
		hooks:        append([]hook.Hook(nil), spec.Hooks...),
		hookAssets:   append([]hookasset.HookAsset(nil), spec.HookAssets...),
		instructions: append([]instructions.Instructions(nil), spec.Instructions...),
		mcpServers:   append([]mcp.Server(nil), spec.MCPServers...),
		extensions:   append([]extension.Extension(nil), spec.Extensions...),
	}
	if err := environment.validateFamilies(); err != nil {
		return Environment{}, err
	}
	if err := environment.validateHookAssetReferences(); err != nil {
		return Environment{}, err
	}
	return environment, nil
}

func (environment Environment) validateFamilies() error {
	seenIDs := make(map[entity.ID]struct{})
	destinations := make(map[skillDestination]string)
	extensionCarriers := make(map[extension.CarrierKey]string)
	instructionNames := make(map[string]string)

	for index, value := range environment.skills {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("skills[%d]: %w", index, err)
		}
		if err := addEntityID(seenIDs, value.ID()); err != nil {
			return err
		}
		for _, selected := range value.Targets() {
			key := skillDestination{target: selected, scope: value.Scope(), installName: value.InstallName()}
			if existing, ok := destinations[key]; ok {
				return fmt.Errorf("duplicate skill destination name %q for target %s scope %s already used by skill id %q", value.InstallName(), selected, value.Scope(), existing)
			}
			destinations[key] = value.ID().Name()
		}
	}
	for index, value := range environment.skillSets {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("skill_sets[%d]: %w", index, err)
		}
	}
	for index, value := range environment.hooks {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("hooks[%d]: %w", index, err)
		}
		if err := addEntityID(seenIDs, value.ID()); err != nil {
			return err
		}
	}
	for index, value := range environment.hookAssets {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("hook_assets[%d]: %w", index, err)
		}
		if err := addEntityID(seenIDs, value.ID()); err != nil {
			return err
		}
	}
	for index, value := range environment.instructions {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("instructions[%d]: %w", index, err)
		}
		if err := addEntityID(seenIDs, value.ID()); err != nil {
			return err
		}
		canonicalName := strings.TrimSpace(value.ID().Name())
		if existing, ok := instructionNames[canonicalName]; ok {
			return fmt.Errorf("duplicate instructions id %q from names %q and %q", canonicalName, existing, value.ID().Name())
		}
		instructionNames[canonicalName] = value.ID().Name()
	}
	for index, value := range environment.mcpServers {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("mcp_servers[%d]: %w", index, err)
		}
		if err := addEntityID(seenIDs, value.ID()); err != nil {
			return err
		}
	}
	for index, value := range environment.extensions {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("extensions[%d]: %w", index, err)
		}
		if err := addEntityID(seenIDs, value.ID()); err != nil {
			return err
		}
		key := value.CarrierKey()
		if existing, ok := extensionCarriers[key]; ok {
			return fmt.Errorf("duplicate extension relation for extension ids %q and %q", existing, value.ID().Name())
		}
		extensionCarriers[key] = value.ID().Name()
	}
	return nil
}

func addEntityID(seen map[entity.ID]struct{}, id entity.ID) error {
	if _, exists := seen[id]; exists {
		return fmt.Errorf("duplicate %s id %q", id.Kind(), id.Name())
	}
	seen[id] = struct{}{}
	return nil
}

type skillDestination struct {
	target      target.Target
	scope       target.Scope
	installName string
}

func (environment Environment) validateHookAssetReferences() error {
	assets := make(map[string]hookasset.HookAsset, len(environment.hookAssets))
	for _, asset := range environment.hookAssets {
		assets[asset.ID().Name()] = asset
	}
	for _, selectedHook := range environment.hooks {
		for _, reference := range selectedHook.AssetReferences() {
			asset, ok := assets[reference.ID()]
			if !ok {
				return fmt.Errorf("hook %q: hook asset %q is not declared", selectedHook.ID().Name(), reference.ID())
			}
			if asset.Scope() != selectedHook.Scope() {
				return fmt.Errorf("hook %q: hook asset %q scope %q does not match hook scope %q", selectedHook.ID().Name(), asset.ID().Name(), asset.Scope(), selectedHook.Scope())
			}
		}
	}
	return nil
}

// Validate rejects a zero or invalid Environment value.
func (environment Environment) Validate() error {
	_, err := New(environment.spec())
	return err
}

func (environment Environment) spec() Spec {
	return Spec{
		Targets: environment.targets.Values(), Defaults: environment.defaults,
		Skills: environment.skills, SkillSets: environment.skillSets,
		Hooks: environment.hooks, HookAssets: environment.hookAssets,
		Instructions: environment.instructions, MCPServers: environment.mcpServers,
		Extensions: environment.extensions,
	}
}

// WithGeneratedSkills returns a revalidated environment with generated Skills
// added to the same identity and destination namespace as direct Skills.
func (environment Environment) WithGeneratedSkills(values []skill.Skill) (Environment, error) {
	spec := environment.spec()
	spec.Skills = append(append([]skill.Skill(nil), spec.Skills...), values...)
	return New(spec)
}

// WithExtensions returns a revalidated environment with a replacement
// extension relation set.
func (environment Environment) WithExtensions(values []extension.Extension) (Environment, error) {
	spec := environment.spec()
	spec.Extensions = append([]extension.Extension(nil), values...)
	return New(spec)
}

// Targets returns normalized top-level targets in authored order.
func (environment Environment) Targets() []target.Target { return environment.targets.Values() }

// Skills returns a defensive copy of canonical direct and expanded Skills.
func (environment Environment) Skills() []skill.Skill {
	return append([]skill.Skill(nil), environment.skills...)
}

// SkillSets returns a defensive copy of unresolved SkillSet generators.
func (environment Environment) SkillSets() []skill.SkillSet {
	return append([]skill.SkillSet(nil), environment.skillSets...)
}

// Hooks returns a defensive copy of canonical Hooks.
func (environment Environment) Hooks() []hook.Hook {
	return append([]hook.Hook(nil), environment.hooks...)
}

// HookAssets returns a defensive copy of canonical HookAssets.
func (environment Environment) HookAssets() []hookasset.HookAsset {
	return append([]hookasset.HookAsset(nil), environment.hookAssets...)
}

// Instructions returns a defensive copy of canonical Instructions values.
func (environment Environment) Instructions() []instructions.Instructions {
	return append([]instructions.Instructions(nil), environment.instructions...)
}

// EntityArtifactSources returns provenance inputs owned by concrete
// source-backed entities. Selector roots and non-artifact relations are not
// entity artifact inputs.
func (environment Environment) EntityArtifactSources() []source.Source {
	result := make([]source.Source, 0, len(environment.skills)+len(environment.hookAssets)+len(environment.instructions))
	for _, value := range environment.skills {
		result = append(result, value.Source())
	}
	for _, value := range environment.hookAssets {
		result = append(result, value.Source())
	}
	for _, value := range environment.instructions {
		result = append(result, value.Source())
	}
	return result
}

// MCPServers returns a defensive copy of canonical MCP servers.
func (environment Environment) MCPServers() []mcp.Server {
	return append([]mcp.Server(nil), environment.mcpServers...)
}

// Extensions returns a defensive copy of canonical extension relations.
func (environment Environment) Extensions() []extension.Extension {
	return append([]extension.Extension(nil), environment.extensions...)
}
