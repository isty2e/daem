package journal

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/durable"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

type inventoryJournalDecoder func(context.Context, mutationfs.RegularFileSnapshot, string, durable.SnapshotCodec) (inventoryJournalRecord, error)

// Native inventory admits only v16; default lifecycle entrypoints stay on v15.
func loadNativeRecoveryRootInventory(ctx context.Context, root string, options inventoryOptions) (recoveryRootInventory, error) {
	options.recordDecoder = decodeNativeInventoryJournal
	return loadRecoveryRootInventory(ctx, root, options)
}
