package build

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	skillresource "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/effect/payload"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	skillcompat "github.com/isty2e/daem/internal/supply/compat/skill"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func buildSkillPayloads(
	ctx context.Context,
	resolvers *sourceResolverOnce,
	skills []skillresource.Skill,
	locked lock.File,
	selection targetselection.Selection,
	subjects []topology.SubjectID,
) (managedPathPayloads, error) {
	required, err := requiredSkillProjectionSubjects(subjects)
	if err != nil {
		return managedPathPayloads{}, err
	}
	if len(required) == 0 {
		return managedPathPayloads{payloads: []payload.Payload{}}, nil
	}
	resolver, err := resolvers.get()
	if err != nil {
		return managedPathPayloads{}, err
	}

	result := managedPathPayloads{
		payloads: make([]payload.Payload, 0, len(skills)),
	}
	for _, skill := range skills {
		requiredSubjects, selected := required[skill.ID()]
		if !selected {
			continue
		}

		lockedContract, ok := locked.Locked.ExactSupplySubject(skill.ID())
		if !ok {
			return result, fmt.Errorf("skill %q: missing lockfile entry", skill.ID().Name())
		}

		resolution, err := resolver.Resolve(ctx, skill.Source(), acquisition.OperationOptions{})
		if err != nil {
			return result, fmt.Errorf("skill %q: resolve source: %w", skill.ID().Name(), err)
		}
		identity, view, cleanup, err := materializeLockedSkill(
			ctx,
			skill.ID().Name(),
			lockedContract,
			resolution,
		)
		if cleanup != nil {
			result.cleanups = append(result.cleanups, cleanup)
		}
		if err != nil {
			return result, err
		}
		if err := skillcompat.ValidateSkillArtifact(ctx, view, identity.SourceID()); err != nil {
			return result, fmt.Errorf("skill %q: validate source: %w", skill.ID().Name(), err)
		}
		projectionSubjects, projectionTargets := selectedProjectionSubjects(
			locked,
			skill,
			selection,
			requiredSubjects,
		)
		if len(projectionSubjects) == 0 {
			return result, fmt.Errorf("skill %q: selected managed-path projection is missing", skill.ID().Name())
		}
		if err := validateSelectedSkillTargetPolicies(
			ctx,
			view,
			identity.SourceID(),
			skill.InstallName(),
			projectionTargets,
		); err != nil {
			return result, fmt.Errorf("skill %q: validate source: %w", skill.ID().Name(), err)
		}

		for _, subject := range projectionSubjects {
			delete(requiredSubjects, subject)
			built, err := payload.NewDirectoryPayload(ctx, subject, identity, view)
			if err != nil {
				return result, fmt.Errorf("skill %q: construct payload: %w", skill.ID().Name(), err)
			}
			result.payloads = append(result.payloads, built)
		}
	}
	for entityID, subjects := range required {
		if len(subjects) != 0 {
			return result, fmt.Errorf("required Skill projection for %q was not materialized", entityID.Name())
		}
	}

	return result, nil
}

func requiredSkillProjectionSubjects(
	values []topology.SubjectID,
) (map[entity.ID]map[topology.SubjectID]struct{}, error) {
	result := make(map[entity.ID]map[topology.SubjectID]struct{})
	for index, subject := range values {
		entityID, ok := topologyprojection.EntityID(subject)
		if !ok {
			return nil, fmt.Errorf("required Skill projection subject[%d] is not an entity-backed Skill projection", index)
		}
		if entityID.Kind() != entity.KindSkill {
			continue
		}
		subjects := result[entityID]
		if subjects == nil {
			subjects = make(map[topology.SubjectID]struct{})
			result[entityID] = subjects
		}
		if _, duplicate := subjects[subject]; duplicate {
			return nil, fmt.Errorf("required Skill projection subject[%d] duplicates %q", index, subject)
		}
		subjects[subject] = struct{}{}
	}
	return result, nil
}

func selectedProjectionSubjects(
	locked lock.File,
	skill skillresource.Skill,
	selection targetselection.Selection,
	required map[topology.SubjectID]struct{},
) ([]topology.SubjectID, []target.Target) {
	result := make([]topology.SubjectID, 0)
	targets := make([]target.Target, 0)
	seenTargets := make(map[target.Target]struct{})
	for _, contract := range locked.Locked.Subjects() {
		if contract.EntityID() != skill.ID() {
			continue
		}
		if _, needed := required[contract.SubjectID()]; !needed {
			continue
		}
		realization, ok := contract.Realization()
		if !ok {
			continue
		}
		projection, ok := realization.ManagedPathProjection()
		if !ok {
			continue
		}
		selected := false
		for _, consumer := range projection.ConsumerTargets() {
			if !selection.Includes(consumer) {
				continue
			}
			selected = true
			if _, duplicate := seenTargets[consumer]; duplicate {
				continue
			}
			seenTargets[consumer] = struct{}{}
			targets = append(targets, consumer)
		}
		if !selected {
			continue
		}
		result = append(result, contract.SubjectID())
	}
	return result, targets
}

func materializeLockedSkill(
	ctx context.Context,
	name string,
	locked lock.LockedSubjectContract,
	resolution acquisition.Resolution,
) (artifact.ExactIdentity, access.View, func() error, error) {
	lockedIdentity, ok := locked.ExactSupply()
	if !ok {
		return artifact.ExactIdentity{}, access.View{}, nil, fmt.Errorf("skill %q: locked exact Supply identity is missing", name)
	}
	recipe, repaired := locked.RepairRecipe()
	if !repaired {
		if !resolution.Identity().Equal(lockedIdentity) {
			return artifact.ExactIdentity{}, access.View{}, nil, fmt.Errorf(
				"skill %q: source identity does not match lockfile entry",
				name,
			)
		}
		if err := resolution.View().Verify(ctx, lockedIdentity); err != nil {
			return artifact.ExactIdentity{}, access.View{}, nil, fmt.Errorf(
				"skill %q: verify locked source: %w",
				name,
				err,
			)
		}
		return lockedIdentity, resolution.View(), nil, nil
	}
	if !resolution.Identity().Equal(recipe.Input()) {
		return artifact.ExactIdentity{}, access.View{}, nil, fmt.Errorf(
			"skill %q: source identity does not match repair recipe input",
			name,
		)
	}
	repairedResult, err := skillrepair.Replay(ctx, recipe, resolution.View())
	if err != nil {
		return artifact.ExactIdentity{}, access.View{}, nil, fmt.Errorf("skill %q: replay repair: %w", name, err)
	}
	cleanup := repairedResult.Release
	if !repairedResult.Identity().Equal(lockedIdentity) {
		return artifact.ExactIdentity{}, access.View{}, cleanup, fmt.Errorf(
			"skill %q: replayed identity does not match lockfile entry",
			name,
		)
	}
	view, err := repairedResult.View()
	if err != nil {
		return artifact.ExactIdentity{}, access.View{}, cleanup, fmt.Errorf(
			"skill %q: access replayed artifact: %w",
			name,
			err,
		)
	}
	return lockedIdentity, view, cleanup, nil
}

func validateSelectedSkillTargetPolicies(
	ctx context.Context,
	view access.View,
	sourceID artifact.SourceID,
	installName string,
	targets []target.Target,
) error {
	for _, target := range targets {
		if err := skillcompat.Validate(ctx, view, sourceID, installName, target); err != nil {
			return err
		}
	}

	return nil
}
