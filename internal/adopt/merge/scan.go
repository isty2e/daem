package merge

import (
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired/hook"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/desired/skill"
)

type existingSkillDeclaration struct {
	Skill           skill.Skill
	CanMergeTargets bool
}

type existingDeclarations struct {
	ManifestRoot string
	Header       declaration.ManifestHeader
	Instructions []instructions.Instructions
	Skills       []existingSkillDeclaration
	Hooks        []hook.Hook
	MCPServers   []declarationcodec.MCPServerBlock
	Extensions   []declarationcodec.ExtensionBlock
}

func scanExistingDeclarations(
	content []byte,
	manifestRoot string,
	selectorBackedSkills []skill.Skill,
	requireSelectorMembership bool,
) (existingDeclarations, error) {
	if !filepath.IsAbs(manifestRoot) || filepath.Clean(manifestRoot) != manifestRoot {
		return existingDeclarations{}, fmt.Errorf("merge manifest root must be an absolute clean path")
	}
	environment, err := declarationmanifest.Decode(content)
	if err != nil {
		return existingDeclarations{}, fmt.Errorf("decode merge output manifest: %w", err)
	}
	if requireSelectorMembership && len(environment.SkillSets()) != 0 && len(selectorBackedSkills) == 0 {
		return existingDeclarations{}, fmt.Errorf(
			"selector-backed skill_group membership is required for skill merge classification",
		)
	}
	if len(selectorBackedSkills) != 0 {
		environment, err = environment.WithGeneratedSkills(selectorBackedSkills)
		if err != nil {
			return existingDeclarations{}, fmt.Errorf(
				"correlate locked selector-backed skill_group members: %w",
				err,
			)
		}
	}
	header, err := declaration.DecodeManifestHeader(content)
	if err != nil {
		return existingDeclarations{}, err
	}
	skillBlocks, err := declarationcodec.ScanSkillBlocks(content)
	if err != nil {
		return existingDeclarations{}, err
	}
	directSkillIDs := make(map[string]struct{}, len(skillBlocks))
	for _, block := range skillBlocks {
		directSkillIDs[importSkillResourceID(block.Skill)] = struct{}{}
	}
	skills := make([]existingSkillDeclaration, 0, len(environment.Skills()))
	for _, existingSkill := range environment.Skills() {
		_, canMergeTargets := directSkillIDs[existingSkill.ID().Name()]
		skills = append(skills, existingSkillDeclaration{
			Skill:           existingSkill,
			CanMergeTargets: canMergeTargets,
		})
	}
	mcpServers, err := declarationcodec.ScanMCPServerBlocks(content)
	if err != nil {
		return existingDeclarations{}, err
	}
	extensions, err := declarationcodec.ScanExtensionBlocks(content)
	if err != nil {
		return existingDeclarations{}, err
	}
	return existingDeclarations{
		ManifestRoot: manifestRoot,
		Header:       header,
		Instructions: environment.Instructions(),
		Skills:       skills,
		Hooks:        environment.Hooks(),
		MCPServers:   mcpServers,
		Extensions:   extensions,
	}, nil
}

func validateCanonicalManifest(content []byte) error {
	_, err := declarationmanifest.Decode(content)
	return err
}
