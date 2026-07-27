package normalize

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/desired"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

func normalizeSkills(
	rawSkills []declaration.Skill,
	rawSkillGroups []declaration.SkillGroup,
	defaultTargets []target.Target,
	defaults desired.Defaults,
) ([]desiredskill.Skill, []desiredskill.SkillSet, error) {
	skills := make([]desiredskill.Skill, 0, len(rawSkills))
	skillSets := make([]desiredskill.SkillSet, 0, len(rawSkillGroups))

	for index, rawSkill := range rawSkills {
		context := fmt.Sprintf("skill[%d]", index)

		installName, err := desiredskill.ParseName(rawSkill.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("%s.name: %w", context, err)
		}
		resourceName, err := skillIntentIdentity(rawSkill.ID, installName, context)
		if err != nil {
			return nil, nil, err
		}

		sourceSpec, err := normalizeRequiredSource(rawSkill.Source, context+".source")
		if err != nil {
			return nil, nil, err
		}

		skill, err := normalizeSkillIntent(
			resourceName,
			installName,
			sourceSpec,
			rawSkill.Targets,
			rawSkill.Scope,
			rawSkill.InstallMode,
			rawSkill.Portable,
			rawSkill.CompatRepair,
			rawSkill.Target,
			defaultTargets,
			defaults,
			context,
		)
		if err != nil {
			return nil, nil, err
		}

		skills = append(skills, skill)
	}

	for groupIndex, rawGroup := range rawSkillGroups {
		groupContext := fmt.Sprintf("skill_group[%d]", groupIndex)
		sourceRoot, err := normalizeRequiredSource(rawGroup.Source, groupContext+".source")
		if err != nil {
			return nil, nil, err
		}
		include, err := normalizeSkillGroupSelectors(rawGroup.Include, groupContext+".include")
		if err != nil {
			return nil, nil, err
		}
		exclude, err := normalizeSkillGroupSelectors(rawGroup.Exclude, groupContext+".exclude")
		if err != nil {
			return nil, nil, err
		}

		hasNames := len(rawGroup.Names) != 0
		hasSelectors := len(include) != 0
		switch {
		case hasNames && hasSelectors:
			return nil, nil, fmt.Errorf("%s: use either names or include selectors, not both", groupContext)
		case !hasNames && !hasSelectors:
			return nil, nil, fmt.Errorf("%s: names or include selectors are required", groupContext)
		case hasNames && len(exclude) != 0:
			return nil, nil, fmt.Errorf("%s.exclude: exclude selectors require include selectors", groupContext)
		case hasSelectors:
			set, err := normalizeSkillGroupIntent(
				sourceRoot,
				include,
				exclude,
				rawGroup.Targets,
				rawGroup.Scope,
				rawGroup.InstallMode,
				rawGroup.Portable,
				rawGroup.CompatRepair,
				rawGroup.Target,
				defaultTargets,
				defaults,
				groupContext,
			)
			if err != nil {
				return nil, nil, err
			}
			skillSets = append(skillSets, set)
			continue
		}

		for nameIndex, rawName := range rawGroup.Names {
			nameContext := fmt.Sprintf("%s.names[%d]", groupContext, nameIndex)
			name, err := desiredskill.ParseName(rawName)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: %w", nameContext, err)
			}

			sourceSpec, err := sourceRoot.Child(name)
			if err != nil {
				return nil, nil, fmt.Errorf("%s.source: %w", nameContext, err)
			}

			skill, err := normalizeSkillIntent(
				name,
				name,
				sourceSpec,
				rawGroup.Targets,
				rawGroup.Scope,
				rawGroup.InstallMode,
				rawGroup.Portable,
				rawGroup.CompatRepair,
				rawGroup.Target,
				defaultTargets,
				defaults,
				nameContext,
			)
			if err != nil {
				return nil, nil, err
			}

			skills = append(skills, skill)
		}
	}

	return skills, skillSets, nil
}

func normalizeSkillGroupSelectors(values []string, context string) ([]desiredskill.Selector, error) {
	selectors := make([]desiredskill.Selector, 0, len(values))
	for index, value := range values {
		parsedSelector, err := desiredskill.ParseSelector(value)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", context, index, err)
		}
		selectors = append(selectors, parsedSelector)
	}

	return selectors, nil
}

func normalizeSkillGroupIntent(
	sourceRoot source.Source,
	include []desiredskill.Selector,
	exclude []desiredskill.Selector,
	rawTargets []string,
	rawScope string,
	rawInstallMode string,
	rawPortable *bool,
	compatRepair bool,
	rawPlacements map[string]declaration.SkillTarget,
	defaultTargets []target.Target,
	defaults desired.Defaults,
	context string,
) (desiredskill.SkillSet, error) {
	targets, err := targetsWithDefault(rawTargets, defaultTargets, context+".targets")
	if err != nil {
		return desiredskill.SkillSet{}, err
	}

	scope, err := scopeWithDefault(rawScope, defaults.Scope(), context+".scope")
	if err != nil {
		return desiredskill.SkillSet{}, err
	}

	installMode, err := installModeWithDefault(rawInstallMode, defaults.InstallMode(), context+".install_mode")
	if err != nil {
		return desiredskill.SkillSet{}, err
	}

	portable := true
	if rawPortable != nil {
		portable = *rawPortable
	}
	placements, err := normalizeSkillTargetPlacements(rawPlacements, scope, context)
	if err != nil {
		return desiredskill.SkillSet{}, err
	}

	set, err := desiredskill.NewSkillSet(desiredskill.SkillSetSpec{
		Source:       sourceRoot,
		Include:      include,
		Exclude:      exclude,
		Targets:      targets,
		Placements:   placements,
		Scope:        scope,
		InstallMode:  installMode,
		Portable:     portable,
		CompatRepair: compatRepair,
	})
	if err != nil {
		return desiredskill.SkillSet{}, fmt.Errorf("%s: %w", context, err)
	}
	return set, nil
}

func normalizeSkillIntent(
	resourceName string,
	installName string,
	sourceSpec source.Source,
	rawTargets []string,
	rawScope string,
	rawInstallMode string,
	rawPortable *bool,
	compatRepair bool,
	rawPlacements map[string]declaration.SkillTarget,
	defaultTargets []target.Target,
	defaults desired.Defaults,
	context string,
) (desiredskill.Skill, error) {
	targets, err := targetsWithDefault(rawTargets, defaultTargets, context+".targets")
	if err != nil {
		return desiredskill.Skill{}, err
	}

	scope, err := scopeWithDefault(rawScope, defaults.Scope(), context+".scope")
	if err != nil {
		return desiredskill.Skill{}, err
	}

	installMode, err := installModeWithDefault(rawInstallMode, defaults.InstallMode(), context+".install_mode")
	if err != nil {
		return desiredskill.Skill{}, err
	}

	portable := true
	if rawPortable != nil {
		portable = *rawPortable
	}
	placements, err := normalizeSkillTargetPlacements(rawPlacements, scope, context)
	if err != nil {
		return desiredskill.Skill{}, err
	}

	skill, err := desiredskill.New(desiredskill.Spec{
		Name:         resourceName,
		InstallName:  installName,
		Source:       sourceSpec,
		Targets:      targets,
		Placements:   placements,
		Scope:        scope,
		InstallMode:  installMode,
		Portable:     portable,
		CompatRepair: compatRepair,
	})
	if err != nil {
		return desiredskill.Skill{}, fmt.Errorf("%s: %w", context, err)
	}
	return skill, nil
}

func normalizeSkillTargetPlacements(
	rawPlacements map[string]declaration.SkillTarget,
	scope target.Scope,
	context string,
) (map[target.Target]desiredskill.TargetPlacement, error) {
	placements := make(map[target.Target]desiredskill.TargetPlacement, len(rawPlacements))
	for rawTarget, rawPlacement := range rawPlacements {
		parsedTarget, err := target.ParseTarget(rawTarget)
		if err != nil {
			return nil, fmt.Errorf("%s.target.%s: %w", context, rawTarget, err)
		}
		placement, err := desiredskill.NewTargetPlacement(scope, rawPlacement.InstallTo)
		if err != nil {
			return nil, fmt.Errorf("%s.target.%s.install_to: %w", context, rawTarget, err)
		}
		placements[parsedTarget] = placement
	}
	return placements, nil
}

func skillIntentIdentity(rawID string, installName string, context string) (string, error) {
	if strings.TrimSpace(rawID) == "" {
		return installName, nil
	}

	resourceName, err := desiredskill.ParseName(rawID)
	if err != nil {
		return "", fmt.Errorf("%s.id: %w", context, err)
	}
	return resourceName, nil
}
