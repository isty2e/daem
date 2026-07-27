package build

import (
	"context"
	"fmt"

	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/directfile"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
)

// lockedFileMaterialization is the verified file content and deterministic
// transformation shared by Instructions and HookAsset payload construction.
type lockedFileMaterialization struct {
	content        access.FileContent
	transformation artifact.FileMaterialization
}

func materializeLockedFile(
	ctx context.Context,
	resolver sourceresolution.Resolver,
	sourceSpec source.Source,
	locked lock.LockedSubjectContract,
	requiredExecutable bool,
) (lockedFileMaterialization, error) {
	resolution, err := resolver.Resolve(ctx, sourceSpec, acquisition.OperationOptions{})
	if err != nil {
		return lockedFileMaterialization{}, fmt.Errorf("resolve source: %w", err)
	}
	identity := resolution.Identity()
	if identity.Kind() != artifact.ArtifactKindFile {
		return lockedFileMaterialization{}, fmt.Errorf("source: expected file artifact")
	}
	lockedIdentity, ok := locked.ExactSupply()
	if !ok || !lockedIdentity.Equal(identity) {
		return lockedFileMaterialization{}, fmt.Errorf("source identity does not match lockfile entry")
	}
	content, err := directfile.ReadExact(ctx, resolution.View(), identity)
	if err != nil {
		return lockedFileMaterialization{}, fmt.Errorf("read source: %w", err)
	}
	transformation, err := artifact.NewFileMaterialization(
		identity,
		content.Bytes(),
		content.Mode().Perm()&0o111 != 0,
		requiredExecutable,
	)
	if err != nil {
		return lockedFileMaterialization{}, fmt.Errorf("materialize source: %w", err)
	}
	if err := locked.ValidateFileMaterialization(transformation); err != nil {
		return lockedFileMaterialization{}, fmt.Errorf("validate materialization: %w", err)
	}
	return lockedFileMaterialization{content: content, transformation: transformation}, nil
}
