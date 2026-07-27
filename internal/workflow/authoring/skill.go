package authoring

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
)

func BuildAddSkillChange(document ManifestDocument, request AddSkillRequest) (Change, error) {
	if err := document.validateOriginal(); err != nil {
		return Change{}, err
	}

	skill, err := SkillFromAddRequest(request, document.Root)
	if err != nil {
		return Change{}, err
	}

	content, changeKind, err := ApplyAddSkillToManifest(document.Content, skill)
	if err != nil {
		return Change{}, err
	}
	if err := document.validateResult(content); err != nil {
		return Change{}, err
	}

	return Change{
		ManifestPath:  document.Path,
		Original:      document.Content,
		Content:       content,
		ResourceID:    skill.ResourceID(),
		ChangeKind:    changeKind,
		ManifestBlock: strings.TrimRight(declarationcodec.RenderSkillBlock(skill), "\n"),
		Warnings:      skillWarnings(skill, document.Root),
	}, nil
}

func SkillFromAddRequest(request AddSkillRequest, manifestRoot string) (declarationcodec.Skill, error) {
	source, inferredName, err := skillSource(request, manifestRoot)
	if err != nil {
		return declarationcodec.Skill{}, err
	}
	name := request.Name
	if name == "" {
		name = inferredName
	}

	skill := declarationcodec.Skill{
		ID:      request.ID,
		Name:    name,
		Source:  source,
		Targets: append([]string(nil), request.Targets...),
		Scope:   request.Scope,
	}
	if source.Mode == string(sourcepkg.LocalSourceModeLink) {
		portable := false
		skill.Portable = &portable
	}
	return skill, nil
}

func BuildRemoveSkillChange(document ManifestDocument, request RemoveSkillRequest) (Change, error) {
	if err := document.validateOriginal(); err != nil {
		return Change{}, err
	}

	content, changeKind, err := ApplyRemoveSkillToManifest(document.Content, request)
	if err != nil {
		return Change{}, err
	}
	if err := document.validateResult(content); err != nil {
		return Change{}, err
	}

	return Change{
		ManifestPath: document.Path,
		Original:     document.Content,
		Content:      content,
		ResourceID:   request.ResourceKey,
		ChangeKind:   changeKind,
	}, nil
}

func ApplyAddSkillToManifest(original []byte, skill declarationcodec.Skill) ([]byte, string, error) {
	change, err := declarationcodec.ApplySkillAdd(original, skill)
	if err != nil {
		return nil, "", err
	}
	changeKind, err := addDeclarationChangeKind(change.Outcome, "append skill resource", "update skill targets")
	if err != nil {
		return nil, "", err
	}
	return change.Content, changeKind, nil
}

func ApplyRemoveSkillToManifest(original []byte, request RemoveSkillRequest) ([]byte, string, error) {
	header, err := declaration.DecodeManifestHeader(original)
	if err != nil {
		return nil, "", err
	}
	candidates, err := removeSkillCandidates(original, header)
	if err != nil {
		return nil, "", err
	}
	matches := filterRemoveSkillCandidates(candidates, request)
	if len(matches) == 0 {
		if selectorBackedSkillGroupsExist(candidates) {
			return nil, "", fmt.Errorf("skill resource %q not found in direct skill declarations or explicit skill_group names; selector-backed skill_group children are not edited by remove skill; edit include/exclude selectors manually and run daem lock", request.ResourceKey)
		}
		return nil, "", fmt.Errorf("skill resource %q not found", request.ResourceKey)
	}
	if len(matches) > 1 {
		return nil, "", fmt.Errorf("skill resource key %q is ambiguous; use a unique id or narrow with --target/--scope", request.ResourceKey)
	}
	return applyRemoveSkillCandidate(original, matches[0], request.Targets)
}

func BuildAddSkillGroupChange(document ManifestDocument, request AddSkillGroupRequest) (Change, error) {
	if err := document.validateOriginal(); err != nil {
		return Change{}, err
	}

	group, err := SkillGroupFromAddRequest(request, document.Root)
	if err != nil {
		return Change{}, err
	}

	content := ApplyAddSkillGroupToManifest(document.Content, group)
	if err := document.validateResult(content); err != nil {
		return Change{}, err
	}

	return Change{
		ManifestPath:  document.Path,
		Original:      document.Content,
		Content:       content,
		ResourceID:    strings.Join(group.Names, ","),
		ChangeKind:    "append skill_group resource",
		ManifestBlock: strings.TrimRight(declarationcodec.RenderSkillGroupBlock(group), "\n"),
		Warnings:      skillGroupWarnings(group, document.Root),
	}, nil
}

func SkillGroupFromAddRequest(request AddSkillGroupRequest, manifestRoot string) (declarationcodec.SkillGroup, error) {
	names, err := cleanSkillGroupNames(request.Names)
	if err != nil {
		return declarationcodec.SkillGroup{}, err
	}
	source, err := skillGroupSource(request, manifestRoot)
	if err != nil {
		return declarationcodec.SkillGroup{}, err
	}

	group := declarationcodec.SkillGroup{
		Names:   names,
		Source:  source,
		Targets: append([]string(nil), request.Targets...),
		Scope:   request.Scope,
	}
	if source.Mode == string(sourcepkg.LocalSourceModeLink) {
		portable := false
		group.Portable = &portable
	}
	return group, nil
}

func ApplyAddSkillGroupToManifest(original []byte, group declarationcodec.SkillGroup) []byte {
	return declaration.AppendDocumentBlock(original, declarationcodec.RenderSkillGroupBlock(group))
}

type removeSkillCandidate struct {
	kind        string
	resourceID  string
	installName string
	scope       string
	targets     []string
	start       int
	end         int
	skill       declarationcodec.Skill
	group       declarationcodec.SkillGroup
	nameIndex   int
}

type SkillGroupPartialTargetRemovalError struct {
	ResourceID       string
	RemainingTargets []string
}

func (err SkillGroupPartialTargetRemovalError) Error() string {
	return fmt.Sprintf("skill_group member %q shares targets with other members; split the group before partial target removal", err.ResourceID)
}

func removeSkillCandidates(content []byte, header declaration.ManifestHeader) ([]removeSkillCandidate, error) {
	skillBlocks, err := declarationcodec.ScanSkillBlocks(content)
	if err != nil {
		return nil, err
	}
	groupBlocks, err := declarationcodec.ScanSkillGroupBlocks(content)
	if err != nil {
		return nil, err
	}

	candidates := make([]removeSkillCandidate, 0, len(skillBlocks)+len(groupBlocks))
	for _, block := range skillBlocks {
		skill := block.Skill
		candidates = append(candidates, removeSkillCandidate{
			kind:        "skill",
			resourceID:  skill.ResourceID(),
			installName: skill.Name,
			scope:       header.EffectiveScope(skill.Scope),
			targets:     header.EffectiveTargets(skill.Targets),
			start:       block.Start,
			end:         block.End,
			skill:       skill,
		})
	}
	for _, block := range groupBlocks {
		group := block.Group
		if len(group.Include) != 0 {
			candidates = append(candidates, removeSkillCandidate{
				kind:  "selector_skill_group",
				group: group,
				start: block.Start,
				end:   block.End,
			})
			continue
		}
		for nameIndex, name := range block.Group.Names {
			candidates = append(candidates, removeSkillCandidate{
				kind:        "skill_group",
				resourceID:  name,
				installName: name,
				scope:       header.EffectiveScope(group.Scope),
				targets:     header.EffectiveTargets(group.Targets),
				start:       block.Start,
				end:         block.End,
				group:       group,
				nameIndex:   nameIndex,
			})
		}
	}
	return candidates, nil
}

func filterRemoveSkillCandidates(candidates []removeSkillCandidate, request RemoveSkillRequest) []removeSkillCandidate {
	matches := make([]removeSkillCandidate, 0)
	for _, candidate := range candidates {
		if candidate.resourceID != request.ResourceKey && candidate.installName != request.ResourceKey {
			continue
		}
		if request.Scope != "" && candidate.scope != request.Scope {
			continue
		}
		if len(request.Targets) != 0 && !declaration.Targets(candidate.targets).Intersects(declaration.Targets(request.Targets)) {
			continue
		}
		matches = append(matches, candidate)
	}
	return matches
}

func selectorBackedSkillGroupsExist(candidates []removeSkillCandidate) bool {
	for _, candidate := range candidates {
		if candidate.kind == "selector_skill_group" {
			return true
		}
	}

	return false
}

func applyRemoveSkillCandidate(original []byte, candidate removeSkillCandidate, selectedTargets []string) ([]byte, string, error) {
	if len(selectedTargets) == 0 {
		return removeSkillCandidateCompletely(original, candidate)
	}

	remainingTargets, removed := declaration.RemoveTargets(
		declaration.Targets(candidate.targets),
		declaration.Targets(selectedTargets),
	)
	if !removed {
		return nil, "", fmt.Errorf("skill resource %q does not include selected targets", candidate.resourceID)
	}
	if len(remainingTargets) == 0 {
		return removeSkillCandidateCompletely(original, candidate)
	}
	if candidate.kind == "skill_group" && len(candidate.group.Names) > 1 {
		return nil, "", SkillGroupPartialTargetRemovalError{
			ResourceID:       candidate.resourceID,
			RemainingTargets: remainingTargets.Values(),
		}
	}

	change, err := declaration.ApplyTargetRemoval(declaration.TargetRemovalInput{
		Original:        original,
		Range:           declaration.DocumentRange{Start: candidate.start, End: candidate.end},
		ExistingTargets: declaration.Targets(candidate.targets),
		SelectedTargets: declaration.Targets(selectedTargets),
		NoSelectedTargetsError: func() error {
			return fmt.Errorf("skill resource %q does not include selected targets", candidate.resourceID)
		},
		BeforeTargetReplace: func(originalBlock string) string {
			return declarationcodec.RemoveSkillTargetTables(
				originalBlock,
				skillCandidateTableRoot(candidate),
				selectedTargets,
			)
		},
		RenderBlockWithTargets: func(originalBlock string, remainingTargets declaration.Targets) (string, error) {
			if candidate.kind == "skill_group" {
				return declarationcodec.ReplaceSkillGroupTargets(originalBlock, remainingTargets.Values()), nil
			}
			return declarationcodec.ReplaceSkillTargets(originalBlock, remainingTargets.Values()), nil
		},
	})
	if err != nil {
		return nil, "", err
	}
	changeKind, err := targetRemovalChangeKind(change.Outcome, "remove skill resource", "update skill targets")
	if err != nil {
		return nil, "", err
	}
	return change.Content, changeKind, nil
}

func skillCandidateTableRoot(candidate removeSkillCandidate) string {
	if candidate.kind == "skill_group" {
		return "skill_group"
	}
	return "skill"
}

func removeSkillCandidateCompletely(original []byte, candidate removeSkillCandidate) ([]byte, string, error) {
	switch candidate.kind {
	case "skill":
		return declaration.RemoveDocumentRange(original, declaration.DocumentRange{Start: candidate.start, End: candidate.end}), "remove skill resource", nil
	case "skill_group":
		names := append([]string{}, candidate.group.Names[:candidate.nameIndex]...)
		names = append(names, candidate.group.Names[candidate.nameIndex+1:]...)
		if len(names) == 0 {
			return declaration.RemoveDocumentRange(original, declaration.DocumentRange{Start: candidate.start, End: candidate.end}), "remove empty skill_group", nil
		}
		updatedBlock := declarationcodec.ReplaceSkillGroupNames(string(original[candidate.start:candidate.end]), names)
		content := declaration.ReplaceDocumentRange(
			original,
			declaration.DocumentRange{Start: candidate.start, End: candidate.end},
			[]byte(updatedBlock),
		)
		return content, "remove skill_group member", nil
	default:
		return nil, "", fmt.Errorf("unsupported remove candidate kind %q", candidate.kind)
	}
}
