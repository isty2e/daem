package journal

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

// RequireNoInterruptedApply applies the canonical recovery-root readiness
// policy through the production filesystem and state codecs.
func RequireNoInterruptedApply(ctx context.Context, recoveryRoot string) error {
	return ensureNoActive(ctx, recoveryRoot, inventoryOptions{
		Filesystem: storagecommit.Adapter{},
		StateCodec: statefile.Codec{},
	})
}

// ensureNoActive permits mutation only when the canonical recovery inventory
// is clean or contains inert finalized GC residue.
func ensureNoActive(
	ctx context.Context,
	recoveryRoot string,
	options inventoryOptions,
) error {
	inventory, err := loadRecoveryRootInventory(ctx, recoveryRoot, options)
	if err != nil {
		return err
	}
	switch inventory.decision.State() {
	case retirement.StateClean, retirement.StateFinalized:
		return nil
	case retirement.StateActive,
		retirement.StatePrepared,
		retirement.StateRetained,
		retirement.StateFinalizing:
		return fmt.Errorf("interrupted apply operation found; run: daem recover --dry-run")
	case retirement.StateBlocked:
		return fmt.Errorf("recovery inventory is blocked: %s", inventory.decision.Detail())
	default:
		return fmt.Errorf("recovery inventory classification is invalid")
	}
}

func isSafeRecoveryOperationID(value string) bool {
	return retirement.ValidateOperationID(value) == nil
}
