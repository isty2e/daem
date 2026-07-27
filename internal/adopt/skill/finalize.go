package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/supply/artifact"
	targetpkg "github.com/isty2e/daem/internal/target"
)

type importSkillGroupKey struct {
	InstallName string
	Scope       targetpkg.Scope
	ContentHash artifact.ContentHash
}

type importSkillManifestGroupKey struct {
	Targets    string
	Placements string
	Scope      targetpkg.Scope
}

func Finalize(skills []adopt.Skill) []adopt.Skill {
	groups := make([][]adopt.Skill, 0, len(skills))
	groupIndexes := make(map[importSkillGroupKey]int, len(skills))
	installGroups := make(map[string][]int, len(skills))

	for _, skill := range skills {
		key := importSkillGroupKey{
			InstallName: skill.InstallName,
			Scope:       skill.Scope,
			ContentHash: skill.ContentHash,
		}
		groupIndex, exists := groupIndexes[key]
		if !exists {
			groupIndex = len(groups)
			groupIndexes[key] = groupIndex
			groups = append(groups, nil)
			installGroups[skill.InstallName] = append(installGroups[skill.InstallName], groupIndex)
		}
		groups[groupIndex] = append(groups[groupIndex], skill)
	}

	usedResourceNames := make(map[string]int, len(groups))
	finalized := make([]adopt.Skill, 0, len(groups))
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}

		representative := group[0]
		targets := make([]targetpkg.Target, 0, len(group))
		placements := make(map[targetpkg.Target]string)
		for _, skill := range group {
			targets = append(targets, skill.Target)
			for selectedTarget, installTo := range skill.Placements {
				if _, present := placements[selectedTarget]; !present {
					placements[selectedTarget] = installTo
				}
			}
		}
		representative.Targets = uniqueTargets(targets)
		representative.Target = representative.Targets[0]
		representative.Placements = placements

		resourceName := representative.InstallName
		if len(installGroups[representative.InstallName]) > 1 {
			resourceName = qualifiedImportSkillResourceName(representative.Target, representative.Scope, representative.InstallName)
		}
		representative.ResourceName = uniqueImportSkillResourceName(resourceName, usedResourceNames)
		finalized = append(finalized, representative)
	}

	return finalized
}

func AssignGroupSources(sourceDirectory adopt.SourceDirectory, skills []adopt.Skill) ([]adopt.Skill, error) {
	groups := make(map[importSkillManifestGroupKey][]int, len(skills))
	for index, skill := range skills {
		if !importSkillGroupEligible(skill) {
			continue
		}
		targetsKey, err := importSkillTargetsKey(skill.Targets)
		if err != nil {
			return nil, err
		}
		placementsKey, err := importSkillPlacementsKey(skill.Placements)
		if err != nil {
			return nil, err
		}
		key := importSkillManifestGroupKey{
			Targets:    targetsKey,
			Placements: placementsKey,
			Scope:      skill.Scope,
		}
		groups[key] = append(groups[key], index)
	}

	for _, indexes := range groups {
		if len(indexes) < 2 {
			continue
		}
		groupSkills := make([]adopt.Skill, 0, len(indexes))
		for _, index := range indexes {
			groupSkills = append(groupSkills, skills[index])
		}
		groupRoot, err := importSkillGroupSourceRoot(sourceDirectory, groupSkills)
		if err != nil {
			return nil, err
		}
		for _, index := range indexes {
			skills[index].GroupRoot = groupRoot
			skills[index].SourcePath = filepath.Join(groupRoot, skills[index].InstallName)
		}
	}

	return skills, nil
}

func uniqueTargets(targets []targetpkg.Target) []targetpkg.Target {
	result := make([]targetpkg.Target, 0, len(targets))
	seen := make(map[targetpkg.Target]struct{}, len(targets))
	for _, selectedTarget := range targets {
		if _, duplicate := seen[selectedTarget]; duplicate {
			continue
		}
		seen[selectedTarget] = struct{}{}
		result = append(result, selectedTarget)
	}
	return result
}

func importSkillTargetsKey(targets []targetpkg.Target) (string, error) {
	canonical, err := targetpkg.CanonicalSet(targets)
	if err != nil {
		return "", err
	}
	values := make([]string, 0, len(canonical))
	for _, selectedTarget := range canonical {
		values = append(values, string(selectedTarget))
	}
	return strings.Join(values, "\x00"), nil
}

func importSkillPlacementsKey(placements map[targetpkg.Target]string) (string, error) {
	targets := make([]targetpkg.Target, 0, len(placements))
	for selectedTarget := range placements {
		targets = append(targets, selectedTarget)
	}
	canonical, err := targetpkg.CanonicalSet(targets)
	if err != nil {
		return "", err
	}
	values := make([]string, 0, len(canonical))
	for _, selectedTarget := range canonical {
		values = append(values, string(selectedTarget)+"="+placements[selectedTarget])
	}
	return strings.Join(values, "\x00"), nil
}

func importSkillGroupEligible(skill adopt.Skill) bool {
	return skill.ResourceName == skill.InstallName && skill.InstallName != "" && len(skill.Targets) != 0
}

func importSkillGroupSourceRoot(sourceDirectory adopt.SourceDirectory, skills []adopt.Skill) (string, error) {
	hashInput := make([]string, 0, len(skills))
	for _, skill := range skills {
		hashInput = append(hashInput, skill.InstallName+"\x00"+string(skill.ContentHash))
	}
	sort.Strings(hashInput)

	digest := sha256.New()
	for _, value := range hashInput {
		digest.Write([]byte(value))
		digest.Write([]byte{0})
	}

	return sourceDirectory.Resolve(filepath.Join(
		importSkillGroupSourceDirectoryName,
		"sha256-"+hex.EncodeToString(digest.Sum(nil)),
	))
}
