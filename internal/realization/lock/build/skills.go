package build

import (
	"context"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	skillcompat "github.com/isty2e/daem/internal/supply/compat/skill"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	resourcetopology "github.com/isty2e/daem/internal/topology/resource"
)

func lockResolvedSkills(ctx context.Context, inputs []skillLockAssemblyInput, options Options) ([]lock.LockedSubjectContract, error) {
	lockedSkills := make([]lock.LockedSubjectContract, 0, len(inputs))

	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		lockedSkill, err := lockSkill(ctx, input.value, input.skillSetDeclaration, input.artifact)
		if err != nil {
			options.Events.Emit(input.event(EventResourceLockFailed, err))
			return nil, err
		}

		options.Events.Emit(input.event(EventResourceLocked, nil))
		lockedSkills = append(lockedSkills, lockedSkill...)
	}

	return lockedSkills, nil
}

func lockSkill(
	ctx context.Context,
	value skill.Skill,
	skillSetDeclaration *skill.SkillSetDeclarationIdentity,
	resolved resolvedArtifactInput,
) ([]lock.LockedSubjectContract, error) {
	var (
		supply lock.LockedSubjectContract
		err    error
	)
	if value.CompatRepair() {
		supply, err = lockRepairedSkill(ctx, value, skillSetDeclaration, resolved)
	} else {
		supply, err = lockOriginalSkill(ctx, value, skillSetDeclaration, resolved)
	}
	if err != nil {
		return nil, err
	}
	projections, err := refine.SkillPathProjections(value)
	if err != nil {
		return nil, err
	}
	return append([]lock.LockedSubjectContract{supply}, projections...), nil
}

func lockOriginalSkill(
	ctx context.Context,
	value skill.Skill,
	skillSetDeclaration *skill.SkillSetDeclarationIdentity,
	resolved resolvedArtifactInput,
) (lock.LockedSubjectContract, error) {
	entityID := value.ID()
	identity, view, err := resolved.exactArtifact(entityID)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("lock skill %q: %w", value.ID().Name(), err)
	}
	if err := skillcompat.ValidateSkillArtifact(ctx, view, identity.SourceID()); err != nil {
		return lock.LockedSubjectContract{}, skillRepairGuidanceError(
			ctx,
			value,
			identity,
			view,
			fmt.Errorf("validate skill %q: %w", value.ID().Name(), err),
		)
	}
	if err := validateSkillTargetPolicies(ctx, view, identity.SourceID(), value); err != nil {
		return lock.LockedSubjectContract{}, skillRepairGuidanceError(
			ctx,
			value,
			identity,
			view,
			fmt.Errorf("validate skill %q: %w", value.ID().Name(), err),
		)
	}
	if err := view.Verify(ctx, identity); err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("verify skill %q before locking: %w", value.ID().Name(), err)
	}

	derivation, err := lock.NewDirectResolutionDerivation(identity)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("lock skill %q derivation: %w", value.ID().Name(), err)
	}
	correlation, err := skillSetMemberCorrelation(skillSetDeclaration)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("lock skill %q correlation: %w", value.ID().Name(), err)
	}
	subjectID, err := resourcetopology.Subject(value.ID())
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("lower skill %q topology: %w", value.ID().Name(), err)
	}
	lockedSkill, err := lock.NewExactSupplySubjectContract(lock.ExactSupplySubjectInput{
		EntityID:                  value.ID(),
		SubjectID:                 subjectID,
		ExactSupply:               identity,
		Derivation:                derivation,
		SkillSetMemberCorrelation: correlation,
	})
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("lock skill %q: %w", value.ID().Name(), err)
	}
	return lockedSkill, nil
}

func skillSetMemberCorrelation(identity *skill.SkillSetDeclarationIdentity) (*lock.SkillSetMemberCorrelation, error) {
	if identity == nil {
		return nil, nil
	}
	correlation, err := lock.NewSkillSetMemberCorrelation(*identity)
	if err != nil {
		return nil, err
	}
	return &correlation, nil
}

func skillRepairGuidanceError(
	ctx context.Context,
	value skill.Skill,
	identity artifact.ExactIdentity,
	view access.View,
	cause error,
) error {
	classification, err := skillrepair.Classify(
		ctx,
		identity,
		view,
		value.InstallName(),
		value.Targets(),
	)
	if err != nil {
		return cause
	}

	switch classification.Repairability() {
	case skillrepair.RepairabilityMechanical:
		return fmt.Errorf(
			"%w; repairability=mechanical; next: set compat_repair = true on this manifest resource and rerun daem lock; repair actions: %s",
			cause,
			strings.Join(classification.Actions(), "; "),
		)
	case skillrepair.RepairabilityManual:
		return fmt.Errorf(
			"%w; repairability=manual; manual edit required: %s",
			cause,
			strings.Join(classification.ManualReasons(), "; "),
		)
	default:
		return cause
	}
}

func validateSkillTargetPolicies(
	ctx context.Context,
	view access.View,
	sourceID artifact.SourceID,
	value skill.Skill,
) error {
	for _, selectedTarget := range value.Targets() {
		if !profile.Profile(selectedTarget).Supports(entity.KindSkill) {
			continue
		}
		if err := skillcompat.Validate(ctx, view, sourceID, value.InstallName(), selectedTarget); err != nil {
			return err
		}
	}

	return nil
}
