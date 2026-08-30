package recoverygate

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

// CaptureStateDir captures no-follow StateDir incarnation authority without
// RecoveryDir or journal domains. Lock mutation and other StateDir-only
// effect paths must use this instead of joint EffectAuthority.
func CaptureStateDir(ctx context.Context, stateDir string) (transaction.StateDirAuthority, error) {
	if err := requireBarrierContext(ctx); err != nil {
		return transaction.StateDirAuthority{}, err
	}
	return transaction.CaptureStateDirAuthority(ctx, stateDir)
}

// CaptureStateDirBounded captures StateDir authority while charging the
// caller-owned physical traversal budget. Recover active-journal planning uses
// this path; cleanup-only recovery must not call it.
func CaptureStateDirBounded(
	ctx context.Context,
	stateDir string,
	maximumPhysicalDepth int,
	physicalWorkBudget rootedpath.PhysicalTraversalBudget,
) (transaction.StateDirAuthority, error) {
	if err := requireBarrierContext(ctx); err != nil {
		return transaction.StateDirAuthority{}, err
	}
	return transaction.CaptureStateDirAuthorityBounded(
		ctx,
		stateDir,
		maximumPhysicalDepth,
		physicalWorkBudget,
	)
}

func requireBarrierContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("recovery barrier context is required")
	}
	return ctx.Err()
}
