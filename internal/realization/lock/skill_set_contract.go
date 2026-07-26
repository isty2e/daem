package lock

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
)

// SkillSetChildren reconstructs selector-backed Desired children from exact
// locked membership correlation without enumerating a source root.
func (section LockedSection) SkillSetChildren(
	explicitSkills []skill.Skill,
	skillSets []skill.SkillSet,
) ([]skill.Skill, error) {
	explicitIDs := make(map[entity.ID]struct{}, len(explicitSkills))
	for _, explicitSkill := range explicitSkills {
		explicitIDs[explicitSkill.ID()] = struct{}{}
	}

	setByIdentity := make(map[string]int, len(skillSets))
	for index, set := range skillSets {
		identity, err := set.DeclarationIdentity()
		if err != nil {
			return nil, fmt.Errorf("skill_group[%d] declaration identity: %w", index, err)
		}
		key := identity.String()
		if previous, exists := setByIdentity[key]; exists {
			return nil, fmt.Errorf("skill_group[%d] duplicates declaration identity of skill_group[%d]", index, previous)
		}
		setByIdentity[key] = index
	}

	children := make([]skill.Skill, 0)
	matchedBySet := make([]int, len(skillSets))
	for _, contract := range section.subjects {
		correlation, correlated := contract.SkillSetMemberCorrelation()
		if !correlated {
			continue
		}
		if _, explicit := explicitIDs[contract.EntityID()]; explicit {
			return nil, fmt.Errorf(
				"locked skill %q is selector-backed but current manifest also declares it directly; run daem lock after narrowing one declaration",
				contract.EntityID().Name(),
			)
		}
		setIndex, current := setByIdentity[correlation.DeclarationIdentity().String()]
		if !current {
			return nil, fmt.Errorf("locked skill %q belongs to a stale skill_group declaration; run daem lock", contract.EntityID().Name())
		}
		identity, ok := contract.ExactSupply()
		if !ok {
			return nil, fmt.Errorf("locked skill %q is missing exact Supply identity", contract.EntityID().Name())
		}
		childSource, err := lockedSkillSetChildSource(skillSets[setIndex].Source(), contract.EntityID().Name(), identity)
		if err != nil {
			return nil, fmt.Errorf("skill_group[%d].include: %w", setIndex, err)
		}
		child, err := skillSets[setIndex].Child(contract.EntityID().Name(), childSource)
		if err != nil {
			return nil, fmt.Errorf("skill_group[%d].include: %w", setIndex, err)
		}
		children = append(children, child)
		matchedBySet[setIndex]++
	}

	for setIndex, count := range matchedBySet {
		if count == 0 {
			return nil, fmt.Errorf("skill_group[%d]: no locked members match the current declaration; run daem lock", setIndex)
		}
	}
	return children, nil
}

func lockedSkillSetChildSource(
	root source.Source,
	name string,
	identity artifact.ExactIdentity,
) (source.Source, error) {
	resolvedRef := artifact.ResolvedRef("")
	if root.Kind() == source.SourceKindGit {
		resolvedRef = identity.ResolvedRef()
	}
	child, err := root.ResolvedChild(name, resolvedRef)
	if err != nil {
		return source.Source{}, err
	}
	sourceID, err := source.SourceIDFor(child)
	if err != nil {
		return source.Source{}, err
	}
	if sourceID != identity.SourceID() {
		return source.Source{}, fmt.Errorf("locked skill %q source identity does not belong to its skill_group root", name)
	}
	return child, nil
}
