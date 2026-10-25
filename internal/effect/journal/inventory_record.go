package journal

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
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

func (record nativeJournalSnapshots) operationID() string { return record.record.OperationID }

func (record nativeJournalSnapshots) authorityFingerprint(ctx context.Context, codec durable.SnapshotCodec) (string, error) {
	content, err := marshalNativeJournalRecord(ctx, record.record, record.before, record.after, codec)
	if err != nil {
		return "", err
	}
	return recoveryJournalRecordFingerprint(content), nil
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

// The positive native entrypoint still understands ordinary v15 journals.
// Default inventory and all existing public lifecycle entrypoints stay frozen.
func loadNativeRecoveryRootInventory(ctx context.Context, root string, options inventoryOptions) (recoveryRootInventory, error) {
	options.recordDecoder = decodeNativeInventoryJournal
	return loadRecoveryRootInventory(ctx, root, options)
}

func decodeNativeInventoryJournal(ctx context.Context, snapshot mutationfs.RegularFileSnapshot, path string, codec durable.SnapshotCodec) (inventoryJournalRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if snapshot.Identity() == nil || snapshot.Identity().Kind() != mutationfs.EntryKindFile || snapshot.Mode().Perm() != recoveryJournalMode {
		return nil, fmt.Errorf("native inventory journal %q requires a private regular-file snapshot", path)
	}
	content := snapshot.Content()
	if int64(len(content)) > maximumRecoveryJournalBytes {
		return nil, fmt.Errorf("native inventory journal exceeds the journal byte envelope")
	}
	version, err := jsonstrict.ValidateVersionedObject(content, "recovery inventory journal", maximumRecoveryJournalJSONDepth)
	if err != nil {
		return nil, err
	}
	switch version {
	case recoveryJournalVersion:
		return decodeLegacyInventoryJournal(ctx, snapshot, path, codec)
	case nativeRecoveryJournalVersion:
		journal, err := decodeNativeJournalRecord(ctx, content, codec)
		if err != nil {
			return nil, err
		}
		return journal, nil
	default:
		return nil, unsupportedRecoveryJournalVersion(version)
	}
}
