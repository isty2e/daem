package journal

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func readNativeRecoveryDocument(ctx context.Context, namespace NativeDocumentNamespace, maximumBytes int64, budget *physicalwork.Budget) (aggregate.Document, *recovery.PermissionMode, mutationfs.EntryIdentity, error) {
	if maximumBytes <= 0 || maximumBytes > recovery.MaximumRecoveryBackupFileBytes || budget == nil {
		return aggregate.Document{}, nil, nil, fmt.Errorf("native document read limit or shared budget is invalid")
	}
	limit := min(maximumBytes, budget.RemainingBytes())
	capability, err := namespace.AcquireReferent(ctx, budget)
	if err != nil {
		return aggregate.Document{}, nil, nil, err
	}
	content, mode, identity, readErr := namespace.filesystem.ReadRootedRegularFileUpTo(ctx, capability, limit)
	closeErr := capability.Close()
	charged := int64(len(content))
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		charged = limit
	}
	work, workErr := workmeasure.NewArtifactWork(0, charged)
	if workErr == nil {
		workErr = budget.AdmitTree(work)
	}
	if closeErr == nil && workErr == nil && errors.Is(readErr, fs.ErrNotExist) {
		return aggregate.AbsentDocument(), nil, nil, ctx.Err()
	}
	if err := errors.Join(readErr, closeErr, workErr, ctx.Err()); err != nil {
		return aggregate.Document{}, nil, nil, err
	}
	return aggregate.ExistingDocument(content), recovery.NewPermissionMode(mode), identity, nil
}
