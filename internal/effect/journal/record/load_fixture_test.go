package record

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func loadRecoveryJournal(
	ctx context.Context,
	filesystem mutationfs.PathReader,
	path string,
	stateCodec durable.SnapshotCodec,
) (recoveryJournal, error) {
	snapshot, err := filesystem.ReadRegularFileSnapshotUpTo(ctx, path, MaximumBytes)
	if err != nil {
		return recoveryJournal{}, fmt.Errorf("read recovery journal: %w", err)
	}
	return decodeRecoveryJournalSnapshot(snapshot, path, stateCodec)
}
