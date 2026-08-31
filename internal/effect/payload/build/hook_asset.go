package build

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	hookassetresource "github.com/isty2e/daem/internal/desired/hookasset"
	"github.com/isty2e/daem/internal/effect/payload"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func buildHookAssetPayloads(
	ctx context.Context,
	resolvers *sourceResolverOnce,
	hookAssets []hookassetresource.HookAsset,
	locked lock.File,
	subjects []topology.SubjectID,
) ([]payload.Payload, error) {
	if len(subjects) == 0 {
		return nil, nil
	}

	assetByName := assetsByName(hookAssets)
	resolver, err := resolvers.get()
	if err != nil {
		return nil, err
	}

	seen := make(map[topology.SubjectID]struct{}, len(subjects))
	payloads := make([]payload.Payload, 0, len(subjects))
	for index, subject := range subjects {
		if _, duplicate := seen[subject]; duplicate {
			return nil, fmt.Errorf("hook_asset payload subjects[%d] duplicates %q", index, subject)
		}
		seen[subject] = struct{}{}
		entityID, entityBacked := topologyprojection.EntityID(subject)
		if !entityBacked || entityID.Kind() != entity.KindHookAsset {
			continue
		}
		asset, declared := assetByName[entityID.Name()]
		if !declared {
			return nil, fmt.Errorf("hook_asset payload subject %q has no desired entity", subject)
		}
		pathContract, ok := locked.Locked.Subject(subject)
		if !ok {
			return nil, fmt.Errorf("hook_asset %q: missing locked path projection", entityID.Name())
		}
		spec, ok := pathContract.Realization()
		if !ok {
			return nil, fmt.Errorf("hook_asset %q: path subject has no realization", entityID.Name())
		}
		projection, ok := spec.ManagedPathProjection()
		if !ok || projection.ContentKind() != realization.PathProjectionFile {
			return nil, fmt.Errorf("hook_asset %q: subject is not a managed file projection", entityID.Name())
		}
		supply, ok := locked.Locked.ExactSupplySubject(asset.ID())
		if !ok {
			return nil, fmt.Errorf("hook_asset %q: missing exact Supply", entityID.Name())
		}
		built, err := buildHookAssetPayload(ctx, resolver, subject, asset, projection, supply)
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, built)
	}

	return payloads, nil
}

func buildHookAssetPayload(
	ctx context.Context,
	resolver sourceresolution.Resolver,
	subject topology.SubjectID,
	asset hookassetresource.HookAsset,
	projection realization.ManagedPathProjection,
	locked lock.LockedSubjectContract,
) (payload.Payload, error) {
	fileUse, ok := locked.ExactFileUse()
	if !ok || fileUse.Scope() != asset.Scope() {
		return payload.Payload{}, fmt.Errorf("hook_asset %q: file use does not match lockfile entry", asset.ID().Name())
	}
	materialized, err := materializeLockedFile(ctx, resolver, asset.Source(), locked, asset.Executable())
	if err != nil {
		return payload.Payload{}, fmt.Errorf("hook_asset %q: %w", asset.ID().Name(), err)
	}
	placement, err := hostsurfacecatalog.Product().HookAssetPlacementFor(
		projection.Scope(),
		projection.ConsumerTargets(),
	)
	if err != nil {
		return payload.Payload{}, fmt.Errorf("hook_asset %q: select path placement: %w", asset.ID().Name(), err)
	}
	projectedHash, err := placement.ContentHash(asset.ID().Name(), projection.Destination())
	if err != nil {
		return payload.Payload{}, fmt.Errorf("hook_asset %q: recover path hash: %w", asset.ID().Name(), err)
	}
	if materialized.transformation.OutputIdentity().ContentHash() != projectedHash {
		return payload.Payload{}, fmt.Errorf("hook_asset %q: path projection hash does not match materialized output", asset.ID().Name())
	}
	exactMode, ok := projection.ExactPermissionMode()
	if !ok {
		return payload.Payload{}, fmt.Errorf("hook_asset %q: path projection has no exact permission mode", asset.ID().Name())
	}
	expectedMode := exactMode.FileMode()
	wantMode, err := placement.ExactPermissionMode(asset.Executable())
	if err != nil {
		return payload.Payload{}, fmt.Errorf("hook_asset %q: select exact permission mode: %w", asset.ID().Name(), err)
	}
	if expectedMode != wantMode.FileMode() {
		return payload.Payload{}, fmt.Errorf("hook_asset %q: path projection permission mode does not match executable intent", asset.ID().Name())
	}
	built, err := payload.NewFilePayload(subject, materialized.content.Bytes(), expectedMode)
	if err != nil {
		return payload.Payload{}, fmt.Errorf("hook_asset %q: construct payload: %w", asset.ID().Name(), err)
	}
	if built.Hash() != materialized.transformation.OutputIdentity().ContentHash() {
		return payload.Payload{}, fmt.Errorf("hook_asset %q: payload hash does not match materialized output", asset.ID().Name())
	}
	return built, nil
}

func assetsByName(assets []hookassetresource.HookAsset) map[string]hookassetresource.HookAsset {
	result := make(map[string]hookassetresource.HookAsset, len(assets))
	for _, asset := range assets {
		result[asset.ID().Name()] = asset
	}
	return result
}
