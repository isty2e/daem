package build

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source/directfile"
	resourcetopology "github.com/isty2e/daem/internal/topology/resource"
)

func lockResolvedHookAssets(
	ctx context.Context,
	inputs []hookAssetLockAssemblyInput,
	options Options,
) ([]lock.LockedSubjectContract, error) {
	lockedAssets := make([]lock.LockedSubjectContract, 0, len(inputs))

	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		lockedAsset, err := lockHookAsset(ctx, input)
		if err != nil {
			options.Events.Emit(input.event(EventResourceLockFailed, err))
			return nil, err
		}

		options.Events.Emit(input.event(EventResourceLocked, nil))
		lockedAssets = append(lockedAssets, lockedAsset)
	}

	return lockedAssets, nil
}

func lockHookAsset(ctx context.Context, input hookAssetLockAssemblyInput) (lock.LockedSubjectContract, error) {
	asset := input.value
	resolution := input.artifact.resolution
	identity := resolution.Identity()
	if identity.Kind() != artifact.ArtifactKindFile {
		return lock.LockedSubjectContract{}, fmt.Errorf("validate hook_asset %q source: expected file artifact", asset.ID().Name())
	}

	content, err := directfile.ReadExact(ctx, resolution.View(), identity)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("read hook_asset %q source: %w", asset.ID().Name(), err)
	}
	materialization, err := artifact.NewFileMaterialization(
		identity,
		content.Bytes(),
		content.Mode().Perm()&0o111 != 0,
		asset.Executable(),
	)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("materialize hook_asset %q: %w", asset.ID().Name(), err)
	}
	fileUse, err := lock.NewExactFileUse(
		asset.Scope(),
		asset.Executable(),
	)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("lock hook_asset %q file use: %w", asset.ID().Name(), err)
	}
	derivation, err := lock.NewFileMaterializationDerivation(materialization)
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("lock hook_asset %q derivation: %w", asset.ID().Name(), err)
	}
	subjectID, err := resourcetopology.Subject(asset.ID())
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("lower hook_asset %q topology: %w", asset.ID().Name(), err)
	}
	lockedAsset, err := lock.NewExactSupplySubjectContract(lock.ExactSupplySubjectInput{
		EntityID:     asset.ID(),
		SubjectID:    subjectID,
		ExactSupply:  identity,
		ExactFileUse: &fileUse,
		Derivation:   derivation,
	})
	if err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("lock hook_asset %q: %w", asset.ID().Name(), err)
	}
	if err := lockedAsset.ValidateFileMaterialization(materialization); err != nil {
		return lock.LockedSubjectContract{}, fmt.Errorf("lock hook_asset %q materialization: %w", asset.ID().Name(), err)
	}
	return lockedAsset, nil
}
