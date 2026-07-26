package lock

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/entity"
	hookresource "github.com/isty2e/daem/internal/desired/hook"
	hookassetresource "github.com/isty2e/daem/internal/desired/hookasset"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source/directfile"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
)

type hookAssetObservationCandidate struct {
	resource hookassetresource.HookAsset
	locked   lock.LockedSubjectContract
}

func hookAssetLockObservationCandidates(
	assets []hookassetresource.HookAsset,
	hooks []hookresource.Hook,
	locked lock.File,
	selection targetselection.Selection,
) ([]hookAssetObservationCandidate, error) {
	lowered, err := topologyhook.Lower(assets, hooks)
	if err != nil {
		return nil, err
	}
	if len(lowered.AssetProjections()) == 0 {
		return nil, nil
	}

	assetByID := hookAssetsByID(assets)
	candidates := make([]hookAssetObservationCandidate, 0, len(lowered.AssetProjections()))
	for _, projection := range lowered.AssetProjections() {
		if !selection.IncludesAny(lowered.ConsumerTargetsOf(projection.SubjectID())) {
			continue
		}
		asset, declared := assetByID[projection.EntityID()]
		if !declared {
			return nil, fmt.Errorf("HookAsset topology subject %q has no desired entity", projection.SubjectID())
		}
		lockedContract, ok := locked.Locked.ExactSupplySubject(asset.ID())
		if !ok {
			continue
		}
		candidates = append(candidates, hookAssetObservationCandidate{
			resource: asset,
			locked:   lockedContract,
		})
	}
	return candidates, nil
}

func hookAssetLockObservations(
	ctx context.Context,
	epoch SourceEpoch,
	candidates []hookAssetObservationCandidate,
) ([]observe.ExactSupplyObservation, error) {
	observations := make([]observe.ExactSupplyObservation, 0, len(candidates))
	for _, candidate := range candidates {
		asset := candidate.resource
		resolution, err := epoch.sourceResolution(asset.ID(), asset.Source())
		if err != nil {
			return nil, fmt.Errorf("hook_asset %q: %w", asset.ID().Name(), err)
		}
		identity := resolution.Identity()
		if identity.Kind() != artifact.ArtifactKindFile {
			return nil, fmt.Errorf("hook_asset %q: validate source: expected file artifact", asset.ID().Name())
		}
		content, err := directfile.ReadExact(ctx, resolution.View(), identity)
		if err != nil {
			return nil, fmt.Errorf("hook_asset %q: read source: %w", asset.ID().Name(), err)
		}
		materialization, err := artifact.NewFileMaterialization(
			identity,
			content.Bytes(),
			content.Mode().Perm()&0o111 != 0,
			asset.Executable(),
		)
		if err != nil {
			return nil, fmt.Errorf("hook_asset %q: materialize source: %w", asset.ID().Name(), err)
		}
		fileUse, hasFileUse := candidate.locked.ExactFileUse()

		observation, err := observe.NewExactSupplyObservation(
			candidate.locked.SubjectID(),
			!hasFileUse || fileUse.Scope() != asset.Scope() ||
				candidate.locked.ValidateFileMaterialization(materialization) != nil,
		)
		if err != nil {
			return nil, err
		}
		observations = append(observations, observation)
	}

	return observations, nil
}

func hookAssetsByID(assets []hookassetresource.HookAsset) map[entity.ID]hookassetresource.HookAsset {
	result := make(map[entity.ID]hookassetresource.HookAsset, len(assets))
	for _, asset := range assets {
		result[asset.ID()] = asset
	}
	return result
}
