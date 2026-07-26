package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"

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
	Targets string
	Scope   targetpkg.Scope
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
		for _, skill := range group {
			targets = append(targets, skill.Target)
		}
		representative.Targets = adopt.UniqueTargets(targets)
		representative.Target = representative.Targets[0]

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
		key := importSkillManifestGroupKey{
			Targets: adopt.TargetsKey(skill.Targets),
			Scope:   skill.Scope,
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
