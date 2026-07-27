package merge

import (
	"path/filepath"
	"strings"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	targetpkg "github.com/isty2e/daem/internal/target"
)

func manifestInstructionFromImportSource(source adoptmodel.Source, targets []targetpkg.Target) declarationcodec.Instruction {
	instruction := declarationcodec.Instruction{
		Source: declarationcodec.InstructionSource{
			Path: filepath.ToSlash(source.SourcePath),
			Mode: string(sourcepkg.LocalSourceModeVendor),
		},
		Targets: adoptmodel.TargetStrings(targets),
		Scope:   string(source.Scope),
	}
	if source.RenderTo != "" {
		instruction.Target = map[string]declaration.InstructionTarget{
			string(source.Target): {RenderTo: source.RenderTo},
		}
	}
	return instruction
}

func manifestSkillFromImportSkill(skill adoptmodel.Skill, targets []targetpkg.Target) declarationcodec.Skill {
	return declarationcodec.Skill{
		ID:   adoptmodel.ManifestSkillID(skill),
		Name: skill.InstallName,
		Source: declarationcodec.SkillSource{
			Path: filepath.ToSlash(skill.SourcePath),
			Mode: string(sourcepkg.LocalSourceModeVendor),
		},
		Targets: adoptmodel.TargetStrings(targets),
		Scope:   string(skill.Scope),
	}
}

func manifestHookFromImportHook(hook adoptmodel.Hook, targets []targetpkg.Target) declaration.Hook {
	result := declaration.Hook{
		Name:           hook.ResourceName,
		Event:          hook.Event,
		Matcher:        hook.Matcher,
		Type:           declarationHookTypeCommand,
		Command:        hook.Command,
		TimeoutSeconds: hook.Timeout,
		StatusMessage:  hook.StatusMessage,
		Targets:        adoptmodel.TargetStrings(targets),
		Scope:          string(hook.Scope),
	}
	if hook.Condition != "" && containsTarget(targets, hook.Target) {
		result.TargetOverrides = []declaration.HookTargetOverride{
			{Target: string(hook.Target), Condition: hook.Condition},
		}
	}
	return result
}

func manifestMCPServerFromImportMCPServer(server adoptmodel.MCPServer) declarationcodec.MCPServer {
	return declarationcodec.MCPServer{
		Name:      server.ResourceName,
		Targets:   []string{string(server.Target)},
		Scope:     string(server.Scope),
		Transport: "stdio",
		Command:   server.Command,
		Args:      append([]string(nil), server.Args...),
		Env:       mcpServerEnvReferences(server.Env),
	}
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

func importSkillResourceID(skill declarationcodec.Skill) string {
	if strings.TrimSpace(skill.ID) != "" {
		return strings.TrimSpace(skill.ID)
	}
	return strings.TrimSpace(skill.Name)
}
