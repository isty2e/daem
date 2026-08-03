package build

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/target"
)

type lockableSkill struct {
	Resource            skill.Skill
	SkillSetDeclaration *skill.SkillSetDeclarationIdentity
}

func lockableSkills(
	ctx context.Context,
	direct []skill.Skill,
	sets []skill.SkillSet,
	resolver acquisition.Resolver,
	options Options,
) ([]lockableSkill, error) {
	skills := make([]lockableSkill, 0, len(direct))
	for _, resource := range direct {
		skills = append(skills, lockableSkill{
			Resource: resource,
		})
	}

	groupSkills, err := expandLockableSkillSets(ctx, sets, resolver, options)
	if err != nil {
		return nil, err
	}
	skills = append(skills, groupSkills...)

	if err := validateLockableSkillResources(lockableSkillResources(skills)); err != nil {
		return nil, err
	}

	return skills, nil
}

func validateLockableSkillResources(skills []skill.Skill) error {
	seenIDs := make(map[string]struct{}, len(skills))
	destinations := make(map[skillDestination]string)
	for _, value := range skills {
		if err := value.Validate(); err != nil {
			return err
		}
		name := value.ID().Name()
		if _, exists := seenIDs[name]; exists {
			return fmt.Errorf("duplicate skill id %q", name)
		}
		seenIDs[name] = struct{}{}
		for _, selected := range value.Targets() {
			key := skillDestination{target: selected, scope: value.Scope(), installName: value.InstallName()}
			if existing, exists := destinations[key]; exists {
				return fmt.Errorf("duplicate skill destination name %q for target %s scope %s already used by skill id %q", value.InstallName(), selected, value.Scope(), existing)
			}
			destinations[key] = name
		}
	}
	return nil
}

type skillDestination struct {
	target      target.Target
	scope       target.Scope
	installName string
}

func expandLockableSkillSets(
	ctx context.Context,
	sets []skill.SkillSet,
	resolver acquisition.Resolver,
	options Options,
) ([]lockableSkill, error) {
	if len(sets) == 0 {
		return nil, nil
	}

	return expandLockableSkillSetsFromListings(ctx, sets, resolver, options)
}

func expandLockableSkillSetsFromListings(
	ctx context.Context,
	sets []skill.SkillSet,
	resolver acquisition.Resolver,
	options Options,
) ([]lockableSkill, error) {
	tasks := make([]sourceTask, 0, len(sets))
	for index, set := range sets {
		tasks = append(tasks, newSkillGroupListTask(index, set))
	}

	results, err := sourceTaskResults(ctx, resolver, tasks, options)
	if err != nil {
		return nil, err
	}
	if err := firstSourceTaskError(ctx, results); err != nil {
		return nil, err
	}
	if len(results) != len(sets) {
		return nil, fmt.Errorf("skill group listing returned %d results for %d declarations", len(results), len(sets))
	}

	skills := make([]lockableSkill, 0)
	expansionBudget := skill.NewExpansionBudget()
	for index, result := range results {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		set := sets[index]
		resources, err := set.ExpandWithBudget(result.listing, expansionBudget)
		if err != nil {
			return nil, fmt.Errorf("skill_group[%d]: %w", index, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		event := result.task.event(EventSkillGroupExpanded, nil)
		event.Count = len(resources)
		options.Events.Emit(event)
		declarationIdentity, err := set.DeclarationIdentity()
		if err != nil {
			return nil, fmt.Errorf("skill_group[%d] declaration identity: %w", index, err)
		}
		skills = append(skills, lockableSkillGroupResources(resources, declarationIdentity)...)
	}

	return skills, nil
}

func lockableSkillGroupResources(
	resources []skill.Skill,
	declarationIdentity skill.SkillSetDeclarationIdentity,
) []lockableSkill {
	skills := make([]lockableSkill, 0, len(resources))
	for _, resource := range resources {
		identity := declarationIdentity
		skills = append(skills, lockableSkill{
			Resource:            resource,
			SkillSetDeclaration: &identity,
		})
	}

	return skills
}

func lockableSkillResources(skills []lockableSkill) []skill.Skill {
	resources := make([]skill.Skill, 0, len(skills))
	for _, lockable := range skills {
		resources = append(resources, lockable.Resource)
	}

	return resources
}
