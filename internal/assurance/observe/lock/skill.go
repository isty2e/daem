package lock

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/skill"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	skillcompat "github.com/isty2e/daem/internal/supply/compat/skill"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

type skillObservationCandidate struct {
	resource        skill.Skill
	locked          lock.LockedSubjectContract
	selectedTargets []target.Target
}

func skillLockObservationCandidates(
	skills []skill.Skill,
	locked lock.File,
	selection targetselection.Selection,
) []skillObservationCandidate {
	candidates := make([]skillObservationCandidate, 0, len(skills))
	for _, resource := range skills {
		selectedTargets := selectedLockedManagedPathTargets(locked, resource.ID(), selection)
		if len(selectedTargets) == 0 {
			continue
		}

		lockedContract, ok := locked.Locked.ExactSupplySubject(resource.ID())
		if !ok {
			continue
		}
		candidates = append(candidates, skillObservationCandidate{
			resource:        resource,
			locked:          lockedContract,
			selectedTargets: selectedTargets,
		})
	}
	return candidates
}

func skillLockObservations(
	ctx context.Context,
	epoch SourceEpoch,
	candidates []skillObservationCandidate,
) ([]observe.ExactSupplyObservation, error) {
	observations := make([]observe.ExactSupplyObservation, 0, len(candidates))
	for _, candidate := range candidates {
		resource := candidate.resource
		resolution, err := epoch.sourceResolution(resource.ID(), resource.Source())
		if err != nil {
			return nil, fmt.Errorf("skill %q: inspect source: %w", resource.ID().Name(), err)
		}
		lockedIdentity, ok := candidate.locked.ExactSupply()
		if !ok {
			return nil, fmt.Errorf("skill %q: locked exact Supply identity is missing", resource.ID().Name())
		}
		recipe, repaired := candidate.locked.RepairRecipe()
		expectedSource := lockedIdentity
		if repaired {
			expectedSource = recipe.Input()
		}
		if !resolution.Identity().Equal(expectedSource) {
			observation, err := observe.NewExactSupplyObservation(candidate.locked.SubjectID(), true)
			if err != nil {
				return nil, err
			}
			observations = append(observations, observation)
			continue
		}

		identity := resolution.Identity()
		view := resolution.View()
		var cleanup func() error
		if repaired {
			replayed, replayErr := skillrepair.Replay(ctx, recipe, view)
			if replayErr != nil {
				return nil, fmt.Errorf("skill %q: replay repair: %w", resource.ID().Name(), replayErr)
			}
			identity = replayed.Identity()
			cleanup = replayed.Release
			view, err = replayed.View()
			if err != nil {
				return nil, errors.Join(
					fmt.Errorf("skill %q: access replayed source: %w", resource.ID().Name(), err),
					cleanup(),
				)
			}
		} else if err := view.Verify(ctx, identity); err != nil {
			return nil, fmt.Errorf("skill %q: verify source: %w", resource.ID().Name(), err)
		}
		if !identity.Equal(lockedIdentity) {
			return nil, releaseObservedSkill(
				cleanup,
				fmt.Errorf("skill %q: replayed identity does not match lockfile entry", resource.ID().Name()),
			)
		}
		validationErr := skillcompat.ValidateSkillArtifact(ctx, view, identity.SourceID())
		if validationErr == nil {
			validationErr = validateSelectedSkillTargetPolicies(
				ctx,
				view,
				identity.SourceID(),
				resource.InstallName(),
				candidate.selectedTargets,
			)
		}
		if validationErr != nil {
			return nil, releaseObservedSkill(
				cleanup,
				fmt.Errorf("skill %q: validate source: %w", resource.ID().Name(), validationErr),
			)
		}
		if err := releaseObservedSkill(cleanup, nil); err != nil {
			return nil, fmt.Errorf("skill %q: %w", resource.ID().Name(), err)
		}

		observation, err := observe.NewExactSupplyObservation(candidate.locked.SubjectID(), false)
		if err != nil {
			return nil, err
		}
		observations = append(observations, observation)
	}

	return observations, nil
}

func releaseObservedSkill(cleanup func() error, operationErr error) error {
	if cleanup == nil {
		return operationErr
	}
	cleanupErr := cleanup()
	if cleanupErr != nil {
		cleanupErr = fmt.Errorf("release replayed skill source: %w", cleanupErr)
	}
	return errors.Join(operationErr, cleanupErr)
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

func selectedLockedManagedPathTargets(
	locked lock.File,
	entityID entity.ID,
	selection targetselection.Selection,
) []target.Target {
	result := make([]target.Target, 0)
	seen := make(map[target.Target]struct{})
	for _, contract := range locked.Locked.Subjects() {
		if contract.EntityID() != entityID {
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
		for _, consumer := range projection.ConsumerTargets() {
			if !selection.Includes(consumer) {
				continue
			}
			if _, duplicate := seen[consumer]; duplicate {
				continue
			}
			seen[consumer] = struct{}{}
			result = append(result, consumer)
		}
	}

	return result
}
