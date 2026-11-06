package inventory

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/retirement"
)

// ErrInterruptedApply reports an active or prepared apply journal.
var ErrInterruptedApply = errors.New("interrupted apply operation found")

// ErrIncompleteJournalCleanup reports retained or finalizing journal cleanup.
var ErrIncompleteJournalCleanup = errors.New("journal cleanup is incomplete")

// RequireClear permits mutation only for clean inventory or inert finalized GC residue.
func RequireClear(ctx context.Context, recoveryRoot string, options Options) error {
	inventory, err := LoadRoot(ctx, recoveryRoot, options)
	if err != nil {
		return err
	}
	switch inventory.Decision().State() {
	case retirement.StateClean, retirement.StateFinalized:
		return nil
	case retirement.StateActive, retirement.StatePrepared:
		return fmt.Errorf("%w; run: daem recover --dry-run", ErrInterruptedApply)
	case retirement.StateRetained,
		retirement.StateFinalizing:
		return fmt.Errorf("%w; run: daem recover --dry-run", ErrIncompleteJournalCleanup)
	case retirement.StateBlocked:
		return fmt.Errorf("recovery inventory is blocked: %s", inventory.Decision().Detail())
	default:
		return fmt.Errorf("recovery inventory classification is invalid")
	}
}
