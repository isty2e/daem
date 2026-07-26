package merge

import (
	"fmt"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
)

func applyImportInstructionTargetMerge(content []byte, name string, instruction declarationcodec.Instruction) ([]byte, string, error) {
	blocks, err := declarationcodec.ScanInstructionBlocks(content)
	if err != nil {
		return nil, "", err
	}
	for _, block := range blocks {
		if block.Name != name {
			continue
		}
		if len(block.Instruction.Targets) == 0 {
			return nil, "", fmt.Errorf("instruction %q inherits manifest targets; edit the manifest manually to change target inheritance", name)
		}
		mergedTargets := mergeImportStringTargets(block.Instruction.Targets, instruction.Targets)
		if sameImportStringTargets(block.Instruction.Targets, mergedTargets) {
			return nil, "", fmt.Errorf("instruction %q already has the selected targets", name)
		}
		updatedBlock := declarationcodec.ReplaceInstructionTargets(string(content[block.Start:block.End]), name, mergedTargets)
		return declaration.ReplaceDocumentRange(content, declaration.DocumentRange{Start: block.Start, End: block.End}, []byte(updatedBlock)), "update instruction targets", nil
	}
	return declaration.AppendDocumentBlock(content, declarationcodec.RenderInstructionBlock(name, instruction)), "append instruction resource", nil
}

func applyImportSkillTargetMerge(content []byte, skill declarationcodec.Skill) ([]byte, string, error) {
	blocks, err := declarationcodec.ScanSkillBlocks(content)
	if err != nil {
		return nil, "", err
	}
	incomingID := importSkillResourceID(skill)
	for _, block := range blocks {
		if importSkillResourceID(block.Skill) != incomingID {
			continue
		}
		if len(block.Skill.Targets) == 0 {
			return nil, "", fmt.Errorf("skill %q inherits manifest targets; edit the manifest manually to change target inheritance", incomingID)
		}
		mergedTargets := mergeImportStringTargets(block.Skill.Targets, skill.Targets)
		if sameImportStringTargets(block.Skill.Targets, mergedTargets) {
			return nil, "", fmt.Errorf("skill %q already has the selected targets", incomingID)
		}
		updatedBlock := declarationcodec.ReplaceSkillTargets(string(content[block.Start:block.End]), mergedTargets)
		return declaration.ReplaceDocumentRange(content, declaration.DocumentRange{Start: block.Start, End: block.End}, []byte(updatedBlock)), "update skill targets", nil
	}
	return declaration.AppendDocumentBlock(content, declarationcodec.RenderSkillBlock(skill)), "append skill resource", nil
}

func applyImportHookTargetMerge(content []byte, hook declarationcodec.Hook) ([]byte, string, error) {
	blocks, err := declarationcodec.ScanHookBlocks(content)
	if err != nil {
		return nil, "", err
	}
	for _, block := range blocks {
		if block.Hook.Name != hook.Name {
			continue
		}
		if len(block.Hook.Targets) == 0 {
			return nil, "", fmt.Errorf("hook %q inherits manifest targets; edit the manifest manually to change target inheritance", hook.Name)
		}
		mergedTargets := mergeImportStringTargets(block.Hook.Targets, hook.Targets)
		if sameImportStringTargets(block.Hook.Targets, mergedTargets) {
			return nil, "", fmt.Errorf("hook %q already has the selected targets", hook.Name)
		}
		mergedHook := block.Hook
		mergedHook.Targets = mergedTargets
		existingOverrides := make(map[string]struct{}, len(block.Hook.TargetOverrides))
		for _, override := range block.Hook.TargetOverrides {
			existingOverrides[override.Target] = struct{}{}
		}
		for _, override := range hook.TargetOverrides {
			if _, exists := existingOverrides[override.Target]; exists {
				return nil, "", fmt.Errorf("hook %q already has target_override for target %q", hook.Name, override.Target)
			}
			mergedHook.TargetOverrides = append(mergedHook.TargetOverrides, override)
		}
		return declarationcodec.ReplaceHookBlock(content, block, mergedHook), "update hook targets", nil
	}
	return declaration.AppendDocumentBlock(content, declarationcodec.RenderHookBlock(hook)), "append hook resource", nil
}
