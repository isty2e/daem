package lock

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/skill"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

const defaultMaxParallelSourceObservations = 4

type sourceObservationFact struct {
	source     source.Source
	resolution acquisition.Resolution
	err        error
}

// SourceEpoch owns raw source resolutions for one exact resource selection.
// It never stores repaired output or authorizes reuse across commands.
type SourceEpoch struct {
	selectedTargets []target.Target
	instructions    []instructionObservationCandidate
	skills          []skillObservationCandidate
	hookAssets      []hookAssetObservationCandidate
	byEntity        map[entity.ID]sourceObservationFact
	initialized     bool
}

// ResolveSourceEpoch resolves all selected exact-Supply source requests in one
// bounded batch while retaining entity-specific result slots.
func ResolveSourceEpoch(
	ctx context.Context,
	paths daempaths.Paths,
	environment desired.Environment,
	locked lock.File,
	selection targetselection.Selection,
) (SourceEpoch, error) {
	resolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		return SourceEpoch{}, err
	}
	return resolveSourceEpochWithResolver(ctx, resolver, environment, locked, selection)
}

func resolveSourceEpochWithResolver(
	ctx context.Context,
	resolver acquisition.BatchResolver,
	environment desired.Environment,
	locked lock.File,
	selection targetselection.Selection,
) (SourceEpoch, error) {
	instructionCandidates := instructionLockObservationCandidates(
		environment.Instructions(),
		locked,
		selection,
	)
	skillCandidates := skillLockObservationCandidates(environment.Skills(), locked, selection)
	hookAssetCandidates, err := hookAssetLockObservationCandidates(
		environment.HookAssets(),
		environment.Hooks(),
		locked,
		selection,
	)
	if err != nil {
		return SourceEpoch{}, err
	}

	requests := make([]acquisition.Request, 0, len(instructionCandidates)+len(skillCandidates)+len(hookAssetCandidates))
	entities := make([]entity.ID, 0, cap(requests))
	appendRequest := func(id entity.ID, sourceSpec source.Source) error {
		request, err := acquisition.NewRequest(
			acquisition.RequestID(fmt.Sprintf("lock-observe:%s:%s", id.Kind(), id.Name())),
			len(requests),
			acquisition.OperationResolve,
			sourceSpec,
		)
		if err != nil {
			return err
		}
		requests = append(requests, request)
		entities = append(entities, id)
		return nil
	}
	for _, candidate := range instructionCandidates {
		if err := appendRequest(candidate.resource.ID(), candidate.resource.Source()); err != nil {
			return SourceEpoch{}, err
		}
	}
	for _, candidate := range skillCandidates {
		if err := appendRequest(candidate.resource.ID(), candidate.resource.Source()); err != nil {
			return SourceEpoch{}, err
		}
	}
	for _, candidate := range hookAssetCandidates {
		if err := appendRequest(candidate.resource.ID(), candidate.resource.Source()); err != nil {
			return SourceEpoch{}, err
		}
	}

	results, err := resolver.ResolveBatch(
		ctx,
		requests,
		acquisition.NewBatchOptions(defaultMaxParallelSourceObservations, nil),
	)
	if err != nil {
		return SourceEpoch{}, err
	}
	if len(results) != len(requests) {
		return SourceEpoch{}, fmt.Errorf(
			"lock observation source batch returned %d results for %d requests",
			len(results),
			len(requests),
		)
	}

	byEntity := make(map[entity.ID]sourceObservationFact, len(results))
	for index, result := range results {
		if err := result.Validate(); err != nil {
			return SourceEpoch{}, fmt.Errorf("lock observation source result[%d]: %w", index, err)
		}
		if !result.Request().Equal(requests[index]) {
			return SourceEpoch{}, fmt.Errorf("lock observation source result[%d] does not match its request", index)
		}
		fact := sourceObservationFact{
			source: requests[index].Source(),
			err:    result.Err(),
		}
		if fact.err == nil {
			resolution, ok := result.Resolution()
			if !ok {
				return SourceEpoch{}, fmt.Errorf(
					"lock observation source result[%d] has no resolution",
					index,
				)
			}
			fact.resolution = resolution
		}
		byEntity[entities[index]] = fact
	}

	return SourceEpoch{
		selectedTargets: selection.Targets(),
		instructions:    instructionCandidates,
		skills:          skillCandidates,
		hookAssets:      hookAssetCandidates,
		byEntity:        byEntity,
		initialized:     true,
	}, nil
}

func (epoch SourceEpoch) sourceResolution(
	id entity.ID,
	sourceSpec source.Source,
) (acquisition.Resolution, error) {
	if !epoch.initialized {
		return acquisition.Resolution{}, fmt.Errorf("lock observation source epoch is not initialized")
	}
	fact, ok := epoch.byEntity[id]
	if !ok {
		return acquisition.Resolution{}, fmt.Errorf(
			"lock observation source epoch has no result for %s %q",
			id.Kind(),
			id.Name(),
		)
	}
	if fact.source != sourceSpec {
		return acquisition.Resolution{}, fmt.Errorf(
			"lock observation source epoch source changed for %s %q",
			id.Kind(),
			id.Name(),
		)
	}
	if fact.err != nil {
		return acquisition.Resolution{}, fact.err
	}
	return fact.resolution, nil
}

// SkillResolution returns one raw local skill resolution only when the caller
// supplies the exact selection and desired source used to create this epoch.
func (epoch SourceEpoch) SkillResolution(
	resource skill.Skill,
	selection targetselection.Selection,
) (acquisition.Resolution, bool) {
	if !epoch.initialized || !slices.Equal(epoch.selectedTargets, selection.Targets()) {
		return acquisition.Resolution{}, false
	}
	if _, local := resource.Source().Local(); !local {
		return acquisition.Resolution{}, false
	}
	resolution, err := epoch.sourceResolution(resource.ID(), resource.Source())
	return resolution, err == nil
}

// Observations validates the epoch's raw resolutions against the lockfile.
func (epoch SourceEpoch) Observations(ctx context.Context) (ObservationSet, error) {
	if !epoch.initialized {
		return ObservationSet{}, fmt.Errorf("lock observation source epoch is not initialized")
	}
	instructions, err := instructionLockObservations(ctx, epoch, epoch.instructions)
	if err != nil {
		return ObservationSet{}, err
	}
	skills, err := skillLockObservations(ctx, epoch, epoch.skills)
	if err != nil {
		return ObservationSet{}, err
	}
	hookAssets, err := hookAssetLockObservations(ctx, epoch, epoch.hookAssets)
	if err != nil {
		return ObservationSet{}, err
	}
	exactSupplies := make([]observe.ExactSupplyObservation, 0, len(skills)+len(instructions)+len(hookAssets))
	exactSupplies = append(exactSupplies, skills...)
	exactSupplies = append(exactSupplies, instructions...)
	exactSupplies = append(exactSupplies, hookAssets...)
	return ObservationSet{exactSupplies: exactSupplies}, nil
}
