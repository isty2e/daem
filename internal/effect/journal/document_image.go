package journal

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

func loadDocumentImages(ctx context.Context, original, expected recordedDocumentImage, root *rootedpath.CapturedRoot, filesystem mutationfs.RootedReader, budget *physicalwork.Budget, maximumBytes int64) (loadedDocumentImages, error) {
	before, err := loadDocumentImage(ctx, original, root, filesystem, budget, maximumBytes)
	if err != nil {
		return loadedDocumentImages{}, err
	}
	after, err := loadDocumentImage(ctx, expected, root, filesystem, budget, maximumBytes)
	if err != nil {
		return loadedDocumentImages{}, err
	}
	return loadedDocumentImagePair(before, after)
}

func loadDocumentImage(ctx context.Context, record recordedDocumentImage, root *rootedpath.CapturedRoot, filesystem mutationfs.RootedReader, budget *physicalwork.Budget, maximumBytes int64) (loadedDocumentImage, error) {
	if ctx == nil {
		return loadedDocumentImage{}, fmt.Errorf("document image context is required")
	}
	if err := ctx.Err(); err != nil {
		return loadedDocumentImage{}, err
	}
	plan, err := record.readPlan(maximumBytes)
	if err != nil {
		return loadedDocumentImage{}, err
	}
	if !plan.Exists() {
		return plan.absentImage()
	}
	if root == nil || filesystem == nil || budget == nil {
		return loadedDocumentImage{}, fmt.Errorf("document image root, filesystem and shared budget are required")
	}
	size := plan.Size()
	if size > maximumBytes || size > budget.RemainingBytes() {
		return loadedDocumentImage{}, fmt.Errorf("document image exceeds the remaining byte envelope")
	}
	rootAuthority, err := root.AuthorityBounded(budget)
	if err != nil {
		return loadedDocumentImage{}, err
	}
	relative, err := rootedpath.NewRelativeDestination(plan.Reference())
	if err != nil {
		return loadedDocumentImage{}, err
	}
	destination, err := rootAuthority.Bind(relative)
	if err != nil {
		return loadedDocumentImage{}, err
	}
	capability, err := root.AcquireBounded(destination, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return loadedDocumentImage{}, err
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
		return loadedDocumentImage{}, err
	}
	loaded, err := plan.loadedImage(content, recovery.PermissionMode(mode.Perm()))
	if err != nil {
		return loadedDocumentImage{}, err
	}
	if err := ctx.Err(); err != nil {
		return loadedDocumentImage{}, err
	}
	return loaded, nil
}
