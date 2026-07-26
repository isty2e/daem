package refine

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredhook "github.com/isty2e/daem/internal/desired/hook"
	desiredhookasset "github.com/isty2e/daem/internal/desired/hookasset"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/hook"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// HookAssetPathProjections correlates desired assets, lowered topology, and
// exact-Supply contracts before refining every referenced path projection.
func HookAssetPathProjections(
	assets []desiredhookasset.HookAsset,
	lowered topologyhook.Model,
	lockedSupplies []lock.LockedSubjectContract,
) ([]lock.LockedSubjectContract, error) {
	assetByEntity := make(map[entity.ID]desiredhookasset.HookAsset, len(assets))
	for _, asset := range assets {
		assetByEntity[asset.ID()] = asset
	}
	supplyByEntity := make(map[entity.ID]lock.LockedSubjectContract, len(lockedSupplies))
	for _, supply := range lockedSupplies {
		if supply.EntityID().Kind() != entity.KindHookAsset {
			continue
		}
		if _, exact := supply.ExactSupply(); !exact {
			continue
		}
		supplyByEntity[supply.EntityID()] = supply
	}
	projections := make([]lock.LockedSubjectContract, 0, len(lowered.AssetProjections()))
	for _, projection := range lowered.AssetProjections() {
		asset, present := assetByEntity[projection.EntityID()]
		if !present {
			return nil, fmt.Errorf("HookAsset topology subject %q has no desired HookAsset", projection.SubjectID())
		}
		supply, ok := supplyByEntity[asset.ID()]
		if !ok {
			return nil, fmt.Errorf("HookAsset %q referenced without exact Supply", asset.ID().Name())
		}
		materialized, ok := supply.MaterializedFileIdentity()
		if !ok {
			return nil, fmt.Errorf("HookAsset %q referenced without materialized file identity", asset.ID().Name())
		}
		contract, err := HookAssetPathProjection(
			asset, projection, materialized, lowered.ConsumerTargetsOf(projection.SubjectID()),
		)
		if err != nil {
			return nil, err
		}
		projections = append(projections, contract)
	}
	return projections, nil
}

// HookAssetPathProjection refines one exact HookAsset file use into its managed path.
func HookAssetPathProjection(
	asset desiredhookasset.HookAsset,
	projection topologyhook.AssetProjection,
	materialized artifact.ExactIdentity,
	consumers []target.Target,
) (lock.LockedSubjectContract, error) {
	if err := asset.Validate(); err != nil {
		return lock.LockedSubjectContract{}, err
	}
	if materialized.Kind() != artifact.ArtifactKindFile {
		return lock.LockedSubjectContract{}, fmt.Errorf("HookAsset %q materialized identity must be a file", asset.ID().Name())
	}
	if projection.EntityID() != asset.ID() || projection.Scope() != asset.Scope() {
		return lock.LockedSubjectContract{}, fmt.Errorf("HookAsset %q topology projection does not match desired identity or scope", asset.ID().Name())
	}
	placement, err := profile.HookAssetPlacementFor(asset.Scope(), consumers)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("select HookAsset %q placement: %w", asset.ID().Name(), err)
	}
	writeRoute, err := profile.HookAssetOperationRoute(placement, profile.OperationWrite)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("select HookAsset %q write route: %w", asset.ID().Name(), err)
	}
	removeRoute, err := profile.HookAssetOperationRoute(placement, profile.OperationRemove)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("select HookAsset %q remove route: %w", asset.ID().Name(), err)
	}
	spec, err := placement.Realize(asset.ID().Name(), materialized.ContentHash(), asset.Executable(), writeRoute)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("realize HookAsset %q: %w", asset.ID().Name(), err)
	}
	expectedSubject, err := topologyhook.AssetSubjectID(asset.ID(), asset.Scope())
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("lower HookAsset %q topology: %w", asset.ID().Name(), err)
	}
	if projection.SubjectID() != expectedSubject || placement.ID() != projection.SubjectID().Namespace() {
		return lock.LockedSubjectContract{}, fmt.Errorf("HookAsset %q topology subject is not canonical for its placement", asset.ID().Name())
	}
	contract, err := lock.NewManagedPathSubjectContract(lock.ManagedPathSubjectInput{
		EntityID: asset.ID(), SubjectID: projection.SubjectID(), Realization: spec,
		WriteRouteID: writeRoute.RouteID(), RemoveRouteID: removeRoute.RouteID(),
	})
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("lock HookAsset %q path projection: %w", asset.ID().Name(), err)
	}
	return contract, nil
}

// HookContributions refines every admitted native Hook target into an aggregate contract.
func HookContributions(
	values []desiredhook.Hook,
	lowered topologyhook.Model,
	encoder commandhook.ContributionEncoder,
) ([]lock.LockedSubjectContract, error) {
	contributions, err := commandhook.PortableContributions(values, lowered, encoder)
	if err != nil {
		return nil, err
	}
	contracts := make([]lock.LockedSubjectContract, 0, len(contributions))
	for _, item := range contributions {
		contribution := item.Contribution()
		placement, admitted := aggregate.HookPlacementFor(contribution.Target(), contribution.Scope())
		if !admitted {
			return nil, fmt.Errorf("Hook contribution subject %q has no native placement", item.SubjectID())
		}
		id, entityBacked := topologyprojection.EntityID(item.SubjectID())
		if !entityBacked {
			return nil, fmt.Errorf("Hook contribution subject %q is not entity-backed", item.SubjectID())
		}
		contract, err := lock.NewHookContributionSubjectContract(id, item.SubjectID(), contribution, placement)
		if err != nil {
			return nil, err
		}
		contracts = append(contracts, contract)
	}
	sort.Slice(contracts, func(left int, right int) bool {
		return contracts[left].CompareIdentity(contracts[right]) < 0
	})
	return contracts, nil
}
