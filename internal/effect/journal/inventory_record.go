package journal

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/durable"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

// Inventory records retain their complete admitted family. Neither variant is
// an executable plan or permission to retire the selected directory.
type inventoryJournalRecord interface {
	operationID() string
	authorityFingerprint(context.Context, durable.SnapshotCodec) (string, error)
}

func (record recoveryJournal) operationID() string { return record.OperationID }

func (record recoveryJournal) authorityFingerprint(ctx context.Context, codec durable.SnapshotCodec) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return recoveryJournalAuthorityFingerprint(record, codec)
}

type inventoryJournalDecoder func(context.Context, mutationfs.RegularFileSnapshot, string, durable.SnapshotCodec) (inventoryJournalRecord, error)

func decodeLegacyInventoryJournal(ctx context.Context, snapshot mutationfs.RegularFileSnapshot, path string, codec durable.SnapshotCodec) (inventoryJournalRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	journal, err := decodeRecoveryJournalSnapshot(snapshot, path, codec)
	if err != nil {
		return nil, err
	}
	return journal, nil
}
