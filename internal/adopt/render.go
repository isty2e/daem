package adopt

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	targetpkg "github.com/isty2e/daem/internal/target"
)

const (
	declarationInstallModeCopy = "copy"
	declarationHookTypeCommand = "command"
)

type importManifestSource struct {
	Path string `toml:"path"`
	Mode string `toml:"mode"`
}

type importManifestSkill struct {
	ID          string               `toml:"id,omitempty"`
	Name        string               `toml:"name"`
	Source      importManifestSource `toml:"source"`
	Targets     []string             `toml:"targets"`
	Scope       string               `toml:"scope"`
	InstallMode string               `toml:"install_mode"`
}

type importManifestSkillGroup struct {
	Names       []string             `toml:"names"`
	Source      importManifestSource `toml:"source"`
	Targets     []string             `toml:"targets"`
	Scope       string               `toml:"scope"`
	InstallMode string               `toml:"install_mode"`
}

type importManifestFile struct {
	Version      int                                  `toml:"version"`
	Targets      []string                             `toml:"targets"`
	Instructions map[string]importManifestInstruction `toml:"instructions"`
	SkillGroups  []importManifestSkillGroup           `toml:"skill_group"`
	Skills       []importManifestSkill                `toml:"skill"`
	Hooks        []importManifestHook                 `toml:"hook"`
	MCPServers   []declarationcodec.MCPServer         `toml:"mcp_server"`
}

type importManifestBody struct {
	Instructions map[string]importManifestInstruction `toml:"instructions"`
	SkillGroups  []importManifestSkillGroup           `toml:"skill_group"`
	Skills       []importManifestSkill                `toml:"skill"`
	Hooks        []importManifestHook                 `toml:"hook"`
	MCPServers   []declarationcodec.MCPServer         `toml:"mcp_server"`
}

type importManifestInstruction struct {
	Source  string                                        `toml:"source"`
	Targets []string                                      `toml:"targets"`
	Scope   string                                        `toml:"scope"`
	Target  map[string]importManifestInstructionRendering `toml:"target,omitempty"`
}

type importManifestInstructionRendering struct {
	RenderTo string `toml:"render_to,omitempty"`
	Mode     string `toml:"mode,omitempty"`
}

type importManifestHook struct {
	Name            string                             `toml:"name"`
	Event           string                             `toml:"event"`
	Matcher         string                             `toml:"matcher"`
	Type            string                             `toml:"type"`
	Command         string                             `toml:"command"`
	Timeout         int                                `toml:"timeout"`
	StatusMessage   string                             `toml:"status_message"`
	Targets         []string                           `toml:"targets"`
	Scope           string                             `toml:"scope"`
	TargetOverrides []importManifestHookTargetOverride `toml:"target_override"`
}

type importManifestHookTargetOverride struct {
	Target    string `toml:"target"`
	Condition string `toml:"if"`
}

func RenderManifestContent(sources []Source, skills []Skill, hooks []Hook, mcpServers []MCPServer) ([]byte, error) {
	targets := make([]string, 0, len(sources)+len(skills)+len(hooks)+len(mcpServers))
	targetSeen := make(map[targetpkg.Target]struct{}, len(sources)+len(skills)+len(hooks)+len(mcpServers))
	body, targets, err := importManifestTables(sources, skills, hooks, mcpServers, targets, targetSeen)
	if err != nil {
		return nil, err
	}

	var output bytes.Buffer
	if err := toml.NewEncoder(&output).Encode(importManifestFile{
		Version:      1,
		Targets:      targets,
		Instructions: body.Instructions,
		SkillGroups:  body.SkillGroups,
		Skills:       body.Skills,
		Hooks:        body.Hooks,
		MCPServers:   body.MCPServers,
	}); err != nil {
		return nil, fmt.Errorf("render import manifest: %w", err)
	}

	return output.Bytes(), nil
}

func RenderManifestBodyContent(sources []Source, skills []Skill, hooks []Hook, mcpServers []MCPServer) ([]byte, error) {
	if len(sources)+len(skills)+len(hooks)+len(mcpServers) == 0 {
		return nil, nil
	}
	body, _, err := importManifestTables(sources, skills, hooks, mcpServers, nil, make(map[targetpkg.Target]struct{}))
	if err != nil {
		return nil, err
	}

	var output bytes.Buffer
	if err := toml.NewEncoder(&output).Encode(body); err != nil {
		return nil, fmt.Errorf("render import manifest body: %w", err)
	}

	return compactImportManifestBody(output.Bytes()), nil
}

func compactImportManifestBody(content []byte) []byte {
	lines := bytes.SplitAfter(content, []byte("\n"))
	var output bytes.Buffer
	for _, line := range lines {
		switch strings.TrimSpace(string(line)) {
		case "", "instructions = {}", "skill_group = []", "skill = []", "hook = []", "mcp_server = []":
			continue
		default:
			output.Write(line)
		}
	}
	return output.Bytes()
}

func AppendManifestBody(content []byte, body []byte) []byte {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return append([]byte{}, content...)
	}
	output := append([]byte{}, content...)
	if len(output) != 0 && !bytes.HasSuffix(output, []byte("\n")) {
		output = append(output, '\n')
	}
	if len(output) != 0 {
		output = append(output, '\n')
	}
	output = append(output, body...)
	output = append(output, '\n')
	return output
}

func importManifestTables(
	sources []Source,
	skills []Skill,
	hooks []Hook,
	mcpServers []MCPServer,
	targets []string,
	targetSeen map[targetpkg.Target]struct{},
) (importManifestBody, []string, error) {
	instructions := make(map[string]importManifestInstruction, len(sources))
	for _, source := range sources {
		if _, ok := targetSeen[source.Target]; !ok {
			targetSeen[source.Target] = struct{}{}
			targets = append(targets, string(source.Target))
		}
		if _, ok := instructions[source.ResourceName]; ok {
			return importManifestBody{}, nil, fmt.Errorf("duplicate imported resource %q", source.ResourceName)
		}
		instructions[source.ResourceName] = importManifestInstruction{
			Source:  filepath.ToSlash(source.SourcePath),
			Targets: []string{string(source.Target)},
			Scope:   string(source.Scope),
			Target:  importInstructionRenderings(source),
		}
	}
	manifestSkills := make([]importManifestSkill, 0, len(skills))
	manifestSkillGroups := make([]importManifestSkillGroup, 0)
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
			groupKey := filepath.ToSlash(skill.GroupRoot) + "\x00" + string(skill.Scope) + "\x00" + TargetsKey(skillTargets)
			if index, ok := manifestSkillGroupIndexes[groupKey]; ok {
				existing := manifestSkillGroups[index]
				existing.Names = append(existing.Names, skill.InstallName)
				sort.Strings(existing.Names)
				manifestSkillGroups[index] = existing
				continue
			}
			manifestSkillGroupIndexes[groupKey] = len(manifestSkillGroups)
			manifestSkillGroups = append(manifestSkillGroups, importManifestSkillGroup{
				Names: []string{skill.InstallName},
				Source: importManifestSource{
					Path: filepath.ToSlash(skill.GroupRoot),
					Mode: string(sourcepkg.LocalSourceModeVendor),
				},
				Targets:     TargetStrings(OrderedTargets(skillTargets)),
				Scope:       string(skill.Scope),
				InstallMode: declarationInstallModeCopy,
			})
			continue
		}
		manifestPath := filepath.ToSlash(skill.SourcePath)
		if index, ok := manifestSkillIndexes[skill.ResourceName]; ok {
			existing := manifestSkills[index]
			if existing.Source.Path != manifestPath || existing.Scope != string(skill.Scope) || existing.Name != skill.InstallName {
				return importManifestBody{}, nil, fmt.Errorf("duplicate imported skill resource %q has incompatible source or skill name", skill.ResourceName)
			}
			existing.Targets = mergeImportTargetStrings(existing.Targets, skillTargets)
			manifestSkills[index] = existing
			continue
		}
		manifestSkillIndexes[skill.ResourceName] = len(manifestSkills)
		manifestSkills = append(manifestSkills, importManifestSkill{
			ID:   ManifestSkillID(skill),
			Name: skill.InstallName,
			Source: importManifestSource{
				Path: manifestPath,
				Mode: string(sourcepkg.LocalSourceModeVendor),
			},
			Targets:     TargetStrings(skillTargets),
			Scope:       string(skill.Scope),
			InstallMode: declarationInstallModeCopy,
		})
	}
	manifestHooks := make([]importManifestHook, 0, len(hooks))
	for _, hook := range hooks {
		if _, ok := targetSeen[hook.Target]; !ok {
			targetSeen[hook.Target] = struct{}{}
			targets = append(targets, string(hook.Target))
		}
		manifestHook := importManifestHook{
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
			manifestHook.TargetOverrides = []importManifestHookTargetOverride{
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
			return importManifestBody{}, nil, fmt.Errorf("duplicate imported mcp_server resource %q", server.ResourceName)
		}
		manifestMCPServerNames[server.ResourceName] = struct{}{}
		manifestMCPServers = append(manifestMCPServers, declarationcodec.MCPServer{
			Name:      server.ResourceName,
			Targets:   []string{string(server.Target)},
			Scope:     string(server.Scope),
			Transport: "stdio",
			Command:   server.Command,
			Args:      append([]string(nil), server.Args...),
			Env:       mcpServerEnvReferences(server.Env),
		})
	}

	return importManifestBody{
		Instructions: instructions,
		SkillGroups:  manifestSkillGroups,
		Skills:       manifestSkills,
		Hooks:        manifestHooks,
		MCPServers:   manifestMCPServers,
	}, targets, nil
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

func importInstructionRenderings(source Source) map[string]importManifestInstructionRendering {
	if source.RenderTo == "" {
		return nil
	}
	return map[string]importManifestInstructionRendering{
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
	return MergeTargetStrings(existing, additions)
}
