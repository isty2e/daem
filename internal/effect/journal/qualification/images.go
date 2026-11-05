package qualification

import (
	"context"
	"errors"
	"fmt"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

func LoadImages(ctx context.Context, original, expected journalrecord.DocumentImage, root *rootedpath.CapturedRoot, filesystem mutationfs.RootedReader, budget *physicalwork.Budget, maximumBytes int64) (journalrecord.LoadedImage, journalrecord.LoadedImage, error) {
	before, err := LoadImage(ctx, original, root, filesystem, budget, maximumBytes)
	if err != nil {
		return journalrecord.LoadedImage{}, journalrecord.LoadedImage{}, err
	}
	after, err := LoadImage(ctx, expected, root, filesystem, budget, maximumBytes)
	if err != nil {
		return journalrecord.LoadedImage{}, journalrecord.LoadedImage{}, err
	}
	return before, after, nil
}

func LoadImage(ctx context.Context, record journalrecord.DocumentImage, root *rootedpath.CapturedRoot, filesystem mutationfs.RootedReader, budget *physicalwork.Budget, maximumBytes int64) (journalrecord.LoadedImage, error) {
	if ctx == nil {
		return journalrecord.LoadedImage{}, fmt.Errorf("document image context is required")
	}
	if err := ctx.Err(); err != nil {
		return journalrecord.LoadedImage{}, err
	}
	plan, err := record.AdmitRead(maximumBytes)
	if err != nil {
		return journalrecord.LoadedImage{}, err
	}
	if !plan.Exists() {
		return plan.Absent()
	}
	if root == nil || filesystem == nil || budget == nil {
		return journalrecord.LoadedImage{}, fmt.Errorf("document image root, filesystem and shared budget are required")
	}
	size := plan.Size()
	if size > maximumBytes || size > budget.RemainingBytes() {
		return journalrecord.LoadedImage{}, fmt.Errorf("document image exceeds the remaining byte envelope")
	}
	rootAuthority, err := root.AuthorityBounded(budget)
	if err != nil {
		return journalrecord.LoadedImage{}, err
	}
	relative, err := rootedpath.NewRelativeDestination(plan.Reference())
	if err != nil {
		return journalrecord.LoadedImage{}, err
	}
	destination, err := rootAuthority.Bind(relative)
	if err != nil {
		return journalrecord.LoadedImage{}, err
	}
	capability, err := root.AcquireBounded(destination, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return journalrecord.LoadedImage{}, err
	}
	content, mode, _, readErr := filesystem.ReadRootedRegularFileUpTo(ctx, capability, size)
	closeErr := capability.Close()
	charged := int64(len(content))
	if readErr != nil {
		charged = size
	}
	work, workErr := workmeasure.NewArtifactWork(0, charged)
	if workErr == nil {
		workErr = budget.AdmitTree(work)
	}
	if err := errors.Join(readErr, closeErr, workErr); err != nil {
		return journalrecord.LoadedImage{}, err
	}
	loaded, err := plan.Load(content, recovery.PermissionMode(mode.Perm()))
	if err != nil {
		return journalrecord.LoadedImage{}, err
	}
	if err := ctx.Err(); err != nil {
		return journalrecord.LoadedImage{}, err
	}
	return loaded, nil
}
