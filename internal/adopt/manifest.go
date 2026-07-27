package adopt

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	targetpkg "github.com/isty2e/daem/internal/target"
)

const (
	declarationInstallModeCopy = "copy"
	declarationHookTypeCommand = "command"
)

func RenderManifestContent(sources []Source, skills []Skill, hooks []Hook, mcpServers []MCPServer) ([]byte, error) {
	targets := make([]string, 0, len(sources)+len(skills)+len(hooks)+len(mcpServers))
	targetSeen := make(map[targetpkg.Target]struct{}, len(sources)+len(skills)+len(hooks)+len(mcpServers))
	body, targets, err := importManifestTables(sources, skills, hooks, mcpServers, targets, targetSeen)
	if err != nil {
		return nil, err
	}

	return declarationcodec.RenderImportManifest(targets, body)
}

func RenderManifestBodyContent(sources []Source, skills []Skill, hooks []Hook, mcpServers []MCPServer) ([]byte, error) {
	if len(sources)+len(skills)+len(hooks)+len(mcpServers) == 0 {
		return nil, nil
	}
	body, _, err := importManifestTables(sources, skills, hooks, mcpServers, nil, make(map[targetpkg.Target]struct{}))
	if err != nil {
		return nil, err
	}

	return declarationcodec.RenderImportManifestBody(body)
}

func importManifestTables(
	sources []Source,
	skills []Skill,
	hooks []Hook,
	mcpServers []MCPServer,
	targets []string,
	targetSeen map[targetpkg.Target]struct{},
) (declarationcodec.ImportManifestBody, []string, error) {
	instructions := make(map[string]declarationcodec.ImportManifestInstruction, len(sources))
	for _, source := range sources {
		if _, ok := targetSeen[source.Target]; !ok {
			targetSeen[source.Target] = struct{}{}
			targets = append(targets, string(source.Target))
		}
		if _, ok := instructions[source.ResourceName]; ok {
			return declarationcodec.ImportManifestBody{}, nil, fmt.Errorf("duplicate imported resource %q", source.ResourceName)
		}
		instructions[source.ResourceName] = declarationcodec.ImportManifestInstruction{
			Source:  filepath.ToSlash(source.SourcePath),
			Targets: []string{string(source.Target)},
			Scope:   string(source.Scope),
			Target:  importInstructionRenderings(source),
		}
	}
	manifestSkills := make([]declarationcodec.ImportManifestSkill, 0, len(skills))
	manifestSkillGroups := make([]declarationcodec.ImportManifestSkillGroup, 0)
	manifestSkillIndexes := make(map[string]int, len(skills))
	manifestSkillGroupIndexes := make(map[string]int, len(skills))
	for _, skill := range skills {
		skillTargets := skill.Targets
		if len(skillTargets) == 0 {
			skillTargets = []targetpkg.Target{skill.Target}
		}
		for _, target := range skillTargets {
			if _, ok := targetSeen[target]; !ok {
				targetSeen[target] = struct{}{}
				targets = append(targets, string(target))
			}
		}
		if skill.GroupRoot != "" {
			groupKey := filepath.ToSlash(skill.GroupRoot) + "\x00" +
				string(skill.Scope) + "\x00" +
				importTargetSetKey(skillTargets) + "\x00" +
				importPlacementSetKey(skill.Placements)
			if index, ok := manifestSkillGroupIndexes[groupKey]; ok {
				existing := manifestSkillGroups[index]
				existing.Names = append(existing.Names, skill.InstallName)
				sort.Strings(existing.Names)
				manifestSkillGroups[index] = existing
				continue
			}
			manifestSkillGroupIndexes[groupKey] = len(manifestSkillGroups)
			manifestSkillGroups = append(manifestSkillGroups, declarationcodec.ImportManifestSkillGroup{
				Names: []string{skill.InstallName},
				Source: declarationcodec.ImportManifestSource{
					Path: filepath.ToSlash(skill.GroupRoot),
					Mode: string(sourcepkg.LocalSourceModeVendor),
				},
				Targets:     importTargetStrings(orderedImportTargets(skillTargets)),
				Scope:       string(skill.Scope),
				InstallMode: declarationInstallModeCopy,
				Target:      importSkillTargetPlacements(skill.Placements),
			})
			continue
		}
		manifestPath := filepath.ToSlash(skill.SourcePath)
		if index, ok := manifestSkillIndexes[skill.ResourceName]; ok {
			existing := manifestSkills[index]
			if existing.Source.Path != manifestPath || existing.Scope != string(skill.Scope) || existing.Name != skill.InstallName {
				return declarationcodec.ImportManifestBody{}, nil, fmt.Errorf("duplicate imported skill resource %q has incompatible source or skill name", skill.ResourceName)
			}
			existing.Targets = mergeImportTargetStrings(existing.Targets, skillTargets)
			existing.Target = mergeImportSkillTargetPlacements(existing.Target, skill.Placements)
			manifestSkills[index] = existing
			continue
		}
		manifestSkillIndexes[skill.ResourceName] = len(manifestSkills)
		manifestSkills = append(manifestSkills, declarationcodec.ImportManifestSkill{
			ID:   ManifestSkillID(skill),
			Name: skill.InstallName,
			Source: declarationcodec.ImportManifestSource{
				Path: manifestPath,
				Mode: string(sourcepkg.LocalSourceModeVendor),
			},
			Targets:     importTargetStrings(skillTargets),
			Scope:       string(skill.Scope),
			InstallMode: declarationInstallModeCopy,
			Target:      importSkillTargetPlacements(skill.Placements),
		})
	}
	manifestHooks := make([]declarationcodec.ImportManifestHook, 0, len(hooks))
	for _, hook := range hooks {
		if _, ok := targetSeen[hook.Target]; !ok {
			targetSeen[hook.Target] = struct{}{}
			targets = append(targets, string(hook.Target))
		}
		manifestHook := declarationcodec.ImportManifestHook{
			Name:          hook.ResourceName,
			Event:         hook.Event,
			Matcher:       hook.Matcher,
			Type:          declarationHookTypeCommand,
			Command:       hook.Command,
			Timeout:       hook.Timeout,
			StatusMessage: hook.StatusMessage,
			Targets:       []string{string(hook.Target)},
			Scope:         string(hook.Scope),
		}
		if hook.Condition != "" {
			manifestHook.TargetOverrides = []declarationcodec.ImportManifestHookTargetOverride{
				{Target: string(hook.Target), Condition: hook.Condition},
			}
		}
		manifestHooks = append(manifestHooks, manifestHook)
	}
	manifestMCPServers := make([]declarationcodec.MCPServer, 0, len(mcpServers))
	manifestMCPServerNames := make(map[string]struct{}, len(mcpServers))
	for _, server := range mcpServers {
		if _, ok := targetSeen[server.Target]; !ok {
			targetSeen[server.Target] = struct{}{}
			targets = append(targets, string(server.Target))
		}
		if _, ok := manifestMCPServerNames[server.ResourceName]; ok {
			return declarationcodec.ImportManifestBody{}, nil, fmt.Errorf("duplicate imported mcp_server resource %q", server.ResourceName)
		}
		manifestMCPServerNames[server.ResourceName] = struct{}{}
		manifestMCPServers = append(manifestMCPServers, declarationcodec.MCPServer{
			Name:      server.ResourceName,
			Targets:   []string{string(server.Target)},
			Scope:     string(server.Scope),
			Transport: "stdio",
			Command:   declaration.MCPCommandFromExecutable(server.Command),
			Args:      append([]string(nil), server.Args...),
			Env:       mcpServerEnvReferences(server.Env),
		})
	}

	return declarationcodec.ImportManifestBody{
		Instructions: instructions,
		SkillGroups:  manifestSkillGroups,
		Skills:       manifestSkills,
		Hooks:        manifestHooks,
		MCPServers:   manifestMCPServers,
	}, targets, nil
}

func importSkillTargetPlacements(values map[targetpkg.Target]string) map[string]declaration.SkillTarget {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]declaration.SkillTarget, len(values))
	for selectedTarget, installTo := range values {
		result[string(selectedTarget)] = declaration.SkillTarget{InstallTo: installTo}
	}
	return result
}

func mergeImportSkillTargetPlacements(
	existing map[string]declaration.SkillTarget,
	additions map[targetpkg.Target]string,
) map[string]declaration.SkillTarget {
	result := make(map[string]declaration.SkillTarget, len(existing)+len(additions))
	for selectedTarget, placement := range existing {
		result[selectedTarget] = placement
	}
	for selectedTarget, installTo := range additions {
		result[string(selectedTarget)] = declaration.SkillTarget{InstallTo: installTo}
	}
	return result
}

func importPlacementSetKey(values map[targetpkg.Target]string) string {
	targets := make([]targetpkg.Target, 0, len(values))
	for selectedTarget := range values {
		targets = append(targets, selectedTarget)
	}
	targets = orderedImportTargets(targets)
	parts := make([]string, 0, len(targets))
	for _, selectedTarget := range targets {
		parts = append(parts, string(selectedTarget)+"="+values[selectedTarget])
	}
	return strings.Join(parts, "\x00")
}

func mcpServerEnvReferences(env map[string]string) map[string]declarationcodec.MCPEnvReference {
	if len(env) == 0 {
		return nil
	}
	result := make(map[string]declarationcodec.MCPEnvReference, len(env))
	for key, fromEnv := range env {
		result[key] = declarationcodec.MCPEnvReference{FromEnv: fromEnv}
	}
	return result
}

func importInstructionRenderings(source Source) map[string]declarationcodec.ImportManifestInstructionRendering {
	if source.RenderTo == "" {
		return nil
	}
	return map[string]declarationcodec.ImportManifestInstructionRendering{
		string(source.Target): {RenderTo: source.RenderTo},
	}
}

func ManifestSkillID(skill Skill) string {
	if skill.ResourceName == "" || skill.ResourceName == skill.InstallName {
		return ""
	}
	return skill.ResourceName
}

func mergeImportTargetStrings(existing []string, additions []targetpkg.Target) []string {
	seen := make(map[string]struct{}, len(existing)+len(additions))
	merged := make([]string, 0, len(existing)+len(additions))
	for _, selectedTarget := range existing {
		if _, duplicate := seen[selectedTarget]; duplicate {
			continue
		}
		seen[selectedTarget] = struct{}{}
		merged = append(merged, selectedTarget)
	}
	for _, selectedTarget := range additions {
		value := string(selectedTarget)
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		merged = append(merged, value)
	}
	return merged
}

func importTargetSetKey(targets []targetpkg.Target) string {
	return strings.Join(importTargetStrings(orderedImportTargets(targets)), "\x00")
}

func orderedImportTargets(targets []targetpkg.Target) []targetpkg.Target {
	selected := make(map[targetpkg.Target]struct{}, len(targets))
	for _, selectedTarget := range targets {
		selected[selectedTarget] = struct{}{}
	}
	ordered := make([]targetpkg.Target, 0, len(selected))
	for _, selectedTarget := range targetpkg.SupportedTargets() {
		if _, ok := selected[selectedTarget]; ok {
			ordered = append(ordered, selectedTarget)
		}
	}
	return ordered
}

func importTargetStrings(targets []targetpkg.Target) []string {
	values := make([]string, 0, len(targets))
	for _, selectedTarget := range targets {
		values = append(values, string(selectedTarget))
	}
	return values
}
