package merge

import (
	"fmt"
	"path/filepath"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/profile"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	targetpkg "github.com/isty2e/daem/internal/target"
)

func classifyImportInstructionMerge(existing existingDeclarations, source adoptmodel.Source) (adoptmodel.MergeResult, []targetpkg.Target) {
	resource := "instructions/" + source.ResourceName
	importedSource := declarationcodec.InstructionSource{
		Path: filepath.ToSlash(source.SourcePath),
		Mode: string(sourcepkg.LocalSourceModeVendor),
	}
	for _, block := range existing.Instructions {
		if block.Name != source.ResourceName {
			continue
		}
		if !sameInstructionSource(block.Instruction.Source, importedSource) ||
			block.Instruction.Scope != string(source.Scope) ||
			!sameImportedInstructionRendering(block.Instruction, source) {
			return adoptmodel.MergeResult{
				Resource: resource,
				Status:   adoptmodel.MergeStatusConflict,
				Detail:   "existing instruction has the same name with a different source, scope, or rendering",
			}, nil
		}
		return classifyImportTargets(resource, block.Instruction.Targets, []targetpkg.Target{source.Target})
	}
	return adoptmodel.MergeResult{Resource: resource, Status: adoptmodel.MergeStatusAdd, Detail: "append imported instruction"}, nil
}

func sameImportedInstructionRendering(existing declarationcodec.Instruction, imported adoptmodel.Source) bool {
	rendering, ok := existing.Target[string(imported.Target)]
	if imported.RenderTo == "" {
		return !ok || (rendering.RenderTo == "" && rendering.Mode == "")
	}
	return ok && rendering.RenderTo == imported.RenderTo && rendering.Mode == ""
}

func classifyImportSkillMerge(existing existingDeclarations, skill adoptmodel.Skill) (adoptmodel.MergeResult, []targetpkg.Target) {
	resource := "skill/" + skill.ResourceName
	importedSource := declarationcodec.SkillSource{
		Path: filepath.ToSlash(skill.SourcePath),
		Mode: string(sourcepkg.LocalSourceModeVendor),
	}
	importedTargets := skill.Targets
	if len(importedTargets) == 0 {
		importedTargets = []targetpkg.Target{skill.Target}
	}
	for _, block := range existing.Skills {
		if importSkillResourceID(block.Skill) != skill.ResourceName {
			continue
		}
		if block.Skill.Name != skill.InstallName ||
			block.Skill.Source != importedSource ||
			block.Skill.Scope != string(skill.Scope) ||
			effectiveInstallMode(block.Skill.InstallMode) != declarationInstallModeCopy {
			return adoptmodel.MergeResult{
				Resource: resource,
				Status:   adoptmodel.MergeStatusConflict,
				Detail:   "existing skill has the same id with a different name, source, scope, or install mode",
			}, nil
		}
		if !sameImportedSkillPlacements(block.Skill, skill, importedTargets) {
			return adoptmodel.MergeResult{
				Resource: resource,
				Status:   adoptmodel.MergeStatusConflict,
				Detail:   "existing skill target placement differs from the imported skill location",
			}, nil
		}
		return classifyImportTargets(resource, block.Skill.Targets, importedTargets)
	}
	if conflict := conflictingSkillDestination(existing, skill); conflict != "" {
		return adoptmodel.MergeResult{
			Resource: resource,
			Status:   adoptmodel.MergeStatusConflict,
			Detail:   conflict,
		}, nil
	}
	return adoptmodel.MergeResult{Resource: resource, Status: adoptmodel.MergeStatusAdd, Detail: "append imported skill"}, nil
}

func sameImportedSkillPlacements(
	existing declarationcodec.Skill,
	imported adoptmodel.Skill,
	importedTargets []targetpkg.Target,
) bool {
	for _, selectedTarget := range importedTargets {
		if !containsStringTarget(existing.Targets, selectedTarget) {
			continue
		}
		defaultPlacement, err := profile.Profile(selectedTarget).DefaultPlacement(entity.KindSkill, imported.Scope)
		if err != nil {
			return false
		}
		expected := defaultPlacement.Root().String()
		if installTo := imported.Placements[selectedTarget]; installTo != "" {
			expected = installTo
		}
		actual := defaultPlacement.Root().String()
		if placement, explicit := existing.Target[string(selectedTarget)]; explicit {
			actual = placement.InstallTo
		}
		if actual != expected {
			return false
		}
	}
	return true
}

func classifyImportHookMerge(existing existingDeclarations, hook adoptmodel.Hook) (adoptmodel.MergeResult, []targetpkg.Target) {
	resource := "hook/" + hook.ResourceName
	for _, block := range existing.Hooks {
		if block.Hook.Name != hook.ResourceName {
			continue
		}
		if !sameImportedHookBase(block.Hook, hook) {
			return adoptmodel.MergeResult{
				Resource: resource,
				Status:   adoptmodel.MergeStatusConflict,
				Detail:   "existing hook has the same name with a different command hook shape",
			}, nil
		}
		if containsStringTarget(block.Hook.Targets, hook.Target) && !sameImportedHookOverride(block.Hook, hook) {
			return adoptmodel.MergeResult{
				Resource: resource,
				Status:   adoptmodel.MergeStatusConflict,
				Detail:   "existing hook target override differs from imported hook condition",
			}, nil
		}
		return classifyImportTargets(resource, block.Hook.Targets, []targetpkg.Target{hook.Target})
	}
	return adoptmodel.MergeResult{Resource: resource, Status: adoptmodel.MergeStatusAdd, Detail: "append imported hook"}, nil
}

func classifyImportMCPServerMerge(existing existingDeclarations, server adoptmodel.MCPServer) adoptmodel.MergeResult {
	resource := "mcp_server/" + server.ResourceName
	imported := manifestMCPServerFromImportMCPServer(server)
	for _, block := range existing.MCPServers {
		if block.Server.Name != server.ResourceName {
			continue
		}
		if sameImportedMCPServer(block.Server, imported) {
			return adoptmodel.MergeResult{
				Resource: resource,
				Status:   adoptmodel.MergeStatusNoop,
				Detail:   "existing mcp_server already matches imported standalone projection",
			}
		}
		return adoptmodel.MergeResult{
			Resource: resource,
			Status:   adoptmodel.MergeStatusConflict,
			Detail:   "existing mcp_server has the same name with a different standalone projection shape",
		}
	}
	return adoptmodel.MergeResult{Resource: resource, Status: adoptmodel.MergeStatusAdd, Detail: "append imported mcp_server"}
}

func classifyImportTargets(resource string, existingTargets []string, importedTargets []targetpkg.Target) (adoptmodel.MergeResult, []targetpkg.Target) {
	missing := missingImportTargets(existingTargets, importedTargets)
	if len(missing) == 0 {
		return adoptmodel.MergeResult{
			Resource: resource,
			Status:   adoptmodel.MergeStatusNoop,
			Detail:   "existing resource already covers imported target(s)",
		}, nil
	}
	return adoptmodel.MergeResult{
		Resource: resource,
		Status:   adoptmodel.MergeStatusMergeTargets,
		Detail:   "add targets " + importDomainTargetsText(missing) + " to existing resource",
	}, missing
}

func conflictingSkillDestination(existing existingDeclarations, imported adoptmodel.Skill) string {
	importedTargets := imported.Targets
	if len(importedTargets) == 0 {
		importedTargets = []targetpkg.Target{imported.Target}
	}
	for _, block := range existing.Skills {
		if block.Skill.Scope != string(imported.Scope) || block.Skill.Name != imported.InstallName {
			continue
		}
		for _, target := range importedTargets {
			if containsStringTarget(block.Skill.Targets, target) {
				return fmt.Sprintf(
					"skill destination target=%s scope=%s name=%q is already used by skill id %q",
					target,
					imported.Scope,
					imported.InstallName,
					importSkillResourceID(block.Skill),
				)
			}
		}
	}
	return ""
}

func sameImportedHookBase(existing declaration.Hook, imported adoptmodel.Hook) bool {
	return existing.Event == imported.Event &&
		existing.Matcher == imported.Matcher &&
		existing.Type == declarationHookTypeCommand &&
		existing.Command == imported.Command &&
		existing.TimeoutSeconds == imported.Timeout &&
		existing.StatusMessage == imported.StatusMessage &&
		existing.Scope == string(imported.Scope)
}

func sameImportedHookOverride(existing declaration.Hook, imported adoptmodel.Hook) bool {
	override, ok := hookOverrideFor(existing.TargetOverrides, imported.Target)
	if imported.Condition == "" {
		return !ok || (override.Condition == "" && override.Matcher == "")
	}
	return ok && override.Condition == imported.Condition && override.Matcher == ""
}

func sameImportedMCPServer(existing declarationcodec.MCPServer, imported declarationcodec.MCPServer) bool {
	return existing.Name == imported.Name &&
		existing.Scope == imported.Scope &&
		existing.Transport == imported.Transport &&
		existing.Command == imported.Command &&
		sameStringSlice(existing.Targets, imported.Targets) &&
		sameStringSlice(existing.Args, imported.Args) &&
		sameMCPServerEnv(existing.Env, imported.Env)
}

func sameMCPServerEnv(left map[string]declarationcodec.MCPEnvReference, right map[string]declarationcodec.MCPEnvReference) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftReference := range left {
		rightReference, ok := right[key]
		if !ok || leftReference.FromEnv != rightReference.FromEnv {
			return false
		}
	}
	return true
}

func sameStringSlice(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func hookOverrideFor(overrides []declaration.HookTargetOverride, target targetpkg.Target) (declaration.HookTargetOverride, bool) {
	for _, override := range overrides {
		if override.Target == string(target) {
			return override, true
		}
	}
	return declaration.HookTargetOverride{}, false
}
