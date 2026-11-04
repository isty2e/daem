package journal

import (
	"context"

	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

// RecoveryInventoryProfile admits journal families through their own codecs.
// It contains no host, statefile, Registry or root acquisition capability.
type RecoveryInventoryProfile struct{ decoder inventoryJournalDecoder }

func PrepareJournalCleanupWithInventoryProfile(ctx context.Context, plan retirement.CleanupPlan, root string, depth int, budget *physicalwork.Budget, filesystem mutationfs.RootedStore, profile RecoveryInventoryProfile) (*RetirementContinuation, error) {
	prepared, err := PrepareJournalCleanup(ctx, plan, root, depth, budget, filesystem)
	if err != nil {
		return nil, err
	}
	prepared.journalDecoder = profile.decoder
	return prepared, nil
}
