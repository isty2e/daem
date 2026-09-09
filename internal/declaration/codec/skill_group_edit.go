package codec

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/declaration"
)

// ApplySkillGroupAdd edits a named group's member-set identity without splitting
// groups or editing selector-backed membership.
func ApplySkillGroupAdd(original []byte, group SkillGroup) (declaration.EditResult, error) {
	if len(group.Names) == 0 || len(group.Include) != 0 || len(group.Exclude) != 0 {
		return declaration.EditResult{}, fmt.Errorf("skill_group authoring requires explicit members without selectors")
	}
	header, err := declaration.DecodeManifestHeader(original)
	if err != nil {
		return declaration.EditResult{}, err
	}
	return declaration.ApplyAddDeclaration(declaration.AddEditInput[SkillGroup]{
		Original:    original,
		Header:      header,
		Declaration: group,
		Codec: declaration.AddEditContract[SkillGroup]{
			Kind: declaration.KindSkillGroup,
			Scan: scanNamedSkillGroupEditBlocks,
			Key: func(value SkillGroup) (declaration.Key, error) {
				names := slices.Clone(value.Names)
				slices.Sort(names)
				return declaration.Key{Kind: declaration.KindSkillGroup, Name: renderStringArray(names)}, nil
			},
			ExplicitTargets: func(value SkillGroup) declaration.Targets {
				return declaration.Targets(value.Targets)
			},
			SameIdentity: func(existing SkillGroup, incoming SkillGroup, header declaration.ManifestHeader) bool {
				return existing.Source == incoming.Source &&
					header.EffectiveScope(existing.Scope) == header.EffectiveScope(incoming.Scope) &&
					skillTargetMapsCompatible(existing.Target, incoming.Target) &&
					skillCoveredTargetSettingsMatch(existing.Target, incoming.Target, header.EffectiveTargets(existing.Targets))
			},
			RenderBlock: RenderSkillGroupBlock,
			RenderBlockWithTargets: func(block string, existing SkillGroup, incoming SkillGroup, targets declaration.Targets, _ declaration.ManifestHeader) (string, error) {
				updated, err := ReplaceSkillGroupTargets(block, targets.Values())
				if err != nil {
					return "", err
				}
				return mergeSkillTargetTables(updated, "skill_group", existing.Target, incoming.Target), nil
			},
			DuplicateError: func(key declaration.Key) error {
				return fmt.Errorf("conflicting skill_group declaration for members %s", key.Name)
			},
			InheritsTargetsError: func(key declaration.Key) error {
				return fmt.Errorf("skill_group members %s inherit manifest targets; edit the manifest manually to change target inheritance", key.Name)
			},
		},
	})
}

func scanNamedSkillGroupEditBlocks(content []byte) ([]declaration.EditBlock[SkillGroup], error) {
	blocks, err := ScanSkillGroupBlocks(content)
	if err != nil {
		return nil, err
	}
	result := make([]declaration.EditBlock[SkillGroup], 0, len(blocks))
	for _, block := range blocks {
		if len(block.Group.Names) == 0 {
			continue
		}
		result = append(result, declaration.EditBlock[SkillGroup]{
			Range: declaration.DocumentRange{Start: block.Start, End: block.End},
			Value: block.Group,
		})
	}
	return result, nil
}
