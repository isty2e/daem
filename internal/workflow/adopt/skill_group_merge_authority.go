package adopt

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lockfile"
)

type selectorSkillMembershipWitness struct {
	environment  desired.Environment
	lockfilePath string
	members      []desiredskill.Skill
}

func lockedSelectorBackedSkills(
	ctx context.Context,
	lockfilePath string,
	environment desired.Environment,
) ([]desiredskill.Skill, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	locked, err := lockfile.Load(ctx, lockfilePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf(
				"selector-backed skill_group membership requires lockfile %q; run daem lock",
				lockfilePath,
			)
		}
		return nil, fmt.Errorf(
			"load selector-backed skill_group membership from %q: %w",
			lockfilePath,
			err,
		)
	}
	members, err := locked.Locked.SkillSetChildren(
		environment.Skills(),
		environment.SkillSets(),
	)
	if err != nil {
		return nil, fmt.Errorf("resolve selector-backed skill_group membership: %w", err)
	}
	if _, err := environment.WithGeneratedSkills(members); err != nil {
		return nil, fmt.Errorf("validate selector-backed skill_group membership: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return members, nil
}

func captureSelectorSkillMembershipWitness(
	ctx context.Context,
	plan adoptmodel.Plan,
) (selectorSkillMembershipWitness, error) {
	environment, required, err := selectorSkillMergeEnvironment(plan)
	if err != nil || !required {
		return selectorSkillMembershipWitness{}, err
	}
	paths, err := daempaths.Resolve(plan.Output())
	if err != nil {
		return selectorSkillMembershipWitness{}, err
	}
	members, err := lockedSelectorBackedSkills(ctx, paths.LockfilePath, environment)
	if err != nil {
		return selectorSkillMembershipWitness{}, err
	}
	members, err = canonicalSelectorSkillMembers(members)
	if err != nil {
		return selectorSkillMembershipWitness{}, err
	}
	return selectorSkillMembershipWitness{
		environment:  environment,
		lockfilePath: paths.LockfilePath,
		members:      members,
	}, nil
}

func (witness selectorSkillMembershipWitness) MatchesCurrent(ctx context.Context) (bool, error) {
	if witness.lockfilePath == "" {
		return true, nil
	}
	members, err := lockedSelectorBackedSkills(ctx, witness.lockfilePath, witness.environment)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return false, contextErr
		}
		return false, nil
	}
	current, err := canonicalSelectorSkillMembers(members)
	if err != nil {
		return false, err
	}
	if len(witness.members) != len(current) {
		return false, nil
	}
	for index := range current {
		if !witness.members[index].Equal(current[index]) {
			return false, nil
		}
	}
	return true, nil
}

func selectorSkillMergeEnvironment(plan adoptmodel.Plan) (desired.Environment, bool, error) {
	if !plan.Merge() || len(plan.SkillSourceAuthorities()) == 0 {
		return desired.Environment{}, false, nil
	}
	environment, err := declarationmanifest.Decode(plan.OriginalContent())
	if err != nil {
		return desired.Environment{}, false, fmt.Errorf(
			"decode selector-backed skill merge authority: %w",
			err,
		)
	}
	return environment, len(environment.SkillSets()) != 0, nil
}

func canonicalSelectorSkillMembers(
	members []desiredskill.Skill,
) ([]desiredskill.Skill, error) {
	canonical := append([]desiredskill.Skill(nil), members...)
	sort.Slice(canonical, func(left int, right int) bool {
		return canonical[left].ID().String() < canonical[right].ID().String()
	})
	previousID := ""
	for index, member := range canonical {
		if err := member.Validate(); err != nil {
			return nil, err
		}
		id := member.ID().String()
		if index > 0 && id == previousID {
			return nil, fmt.Errorf("duplicate selector-backed skill identity %q", id)
		}
		previousID = id
	}
	return canonical, nil
}
