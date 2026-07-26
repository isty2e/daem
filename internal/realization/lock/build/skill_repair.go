package build

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/realization/lock"
	skillcompat "github.com/isty2e/daem/internal/supply/compat/skill"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	resourcetopology "github.com/isty2e/daem/internal/topology/resource"
)

func lockRepairedSkill(
	ctx context.Context,
	value skill.Skill,
	skillSetDeclaration *skill.SkillSetDeclarationIdentity,
	original resolvedArtifactInput,
) (locked lock.LockedSubjectContract, resultErr error) {
	entityID := value.ID()
	inputIdentity, inputView, err := original.exactArtifact(entityID)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("lock skill %q: %w", value.ID().Name(), err)
	}
	result, err := skillrepair.Repair(
		ctx,
		inputIdentity,
		inputView,
		value.InstallName(),
		value.Targets(),
	)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("repair skill %q: %w", value.ID().Name(), err)
	}
	defer func() {
		if releaseErr := result.Release(); releaseErr != nil {
			resultErr = errors.Join(
				resultErr,
				fmt.Errorf("release repaired skill %q: %w", value.ID().Name(), releaseErr),
			)
		}
	}()

	repairedView, err := result.View()
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("access repaired skill %q: %w", value.ID().Name(), err)
	}
	repairedIdentity := result.Identity()
	if err := skillcompat.ValidateSkillArtifact(ctx, repairedView, repairedIdentity.SourceID()); err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("validate repaired skill %q: %w", value.ID().Name(), err)
	}
	if err := validateSkillTargetPolicies(ctx, repairedView, repairedIdentity.SourceID(), value); err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("validate repaired skill %q: %w", value.ID().Name(), err)
	}
	if err := repairedView.Verify(ctx, repairedIdentity); err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("verify repaired skill %q before locking: %w", value.ID().Name(), err)
	}

	recipe, present := result.Recipe()
	if !present {
		return lock.LockedSubjectContract{}, fmt.Errorf("repair skill %q did not produce a canonical recipe", value.ID().Name())
	}
	derivation, err := lock.NewDeterministicTransformDerivation(lock.DeterministicTransform{
		InputIdentity:          inputIdentity,
		RecipeHash:             recipe.Hash(),
		AlgorithmID:            skillrepair.DerivationAlgorithmID,
		AlgorithmVersion:       fmt.Sprintf("v%d", recipe.Version()),
		ExecutionDomain:        skillrepair.DerivationExecutionDomain,
		ExpectedOutputIdentity: repairedIdentity,
	})
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("lock repaired skill %q derivation: %w", value.ID().Name(), err)
	}
	correlation, err := skillSetMemberCorrelation(skillSetDeclaration)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("lock repaired skill %q correlation: %w", value.ID().Name(), err)
	}
	subjectID, err := resourcetopology.Subject(value.ID())
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("lower repaired skill %q topology: %w", value.ID().Name(), err)
	}
	locked, err = lock.NewExactSupplySubjectContract(lock.ExactSupplySubjectInput{
		EntityID:                  value.ID(),
		SubjectID:                 subjectID,
		ExactSupply:               repairedIdentity,
		Derivation:                derivation,
		RepairRecipe:              &recipe,
		SkillSetMemberCorrelation: correlation,
	})
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("lock repaired skill %q: %w", value.ID().Name(), err)
	}
	return locked, nil
}
