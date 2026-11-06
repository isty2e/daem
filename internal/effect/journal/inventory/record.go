package inventory

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

type JournalDecoder func(context.Context, mutationfs.RegularFileSnapshot, string, durable.SnapshotCodec) (journalrecord.Loaded, error)

// Native inventory admits only v16; default lifecycle entrypoints stay on v15.
func LoadNativeRoot(ctx context.Context, root string, options Options) (RootInventory, error) {
	options.Decoder = journalrecord.DecodeNativeSnapshot
	return LoadRoot(ctx, root, options)
}
