package execute

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

func readNativeCommittedState(ctx context.Context, authority *mutationAuthority, input NativeApplyInput) (durable.Snapshot, error) {
	return authority.readNativeState(ctx, input.StateCodec, input.Budget)
}

func (authority *mutationAuthority) readNativeState(ctx context.Context, codec durable.SnapshotCodec, budget *physicalwork.Budget) (result durable.Snapshot, resultErr error) {
	if ctx == nil || authority == nil || authority.projectStatefile == nil || codec == nil || budget == nil || authority.physicalWorkBudget != budget {
		return durable.Snapshot{}, fmt.Errorf("native state read requires selected authority, codec and original allocation")
	}
	if err := ctx.Err(); err != nil {
		return durable.Snapshot{}, err
	}
	capability, err := authority.projectStatefile.Acquire()
	if err != nil {
		return durable.Snapshot{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, capability.Close(), ctx.Err()) }()
	maximum := min(recovery.MaximumRecoveryBackupFileBytes, budget.RemainingBytes())
	if maximum <= 0 {
		return durable.Snapshot{}, fmt.Errorf("native state revalidation exhausted its byte allocation")
	}
	content, _, _, readErr := authority.filesystem.ReadRootedRegularFileUpTo(ctx, capability, maximum)
	charged := int64(len(content))
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		charged = maximum
	}
	work, err := workmeasure.NewArtifactWork(0, charged)
	if err == nil {
		err = budget.AdmitTree(work)
	}
	if err != nil {
		return durable.Snapshot{}, err
	}
	if errors.Is(readErr, os.ErrNotExist) {
		return durable.EmptySnapshot(), nil
	}
	if readErr != nil {
		return durable.Snapshot{}, readErr
	}
	return codec.Decode(content)
}
