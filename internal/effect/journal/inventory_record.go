package journal

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

type inventoryJournalDecoder func(context.Context, mutationfs.RegularFileSnapshot, string, durable.SnapshotCodec) (journalrecord.Loaded, error)

// Native inventory admits only v16; default lifecycle entrypoints stay on v15.
func loadNativeRecoveryRootInventory(ctx context.Context, root string, options inventoryOptions) (recoveryRootInventory, error) {
	options.recordDecoder = journalrecord.DecodeNativeSnapshot
	return loadRecoveryRootInventory(ctx, root, options)
}
