package journal

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

// Loaded records retain decoded family facts and native codec custody. Neither
// this value nor its projections prove publication or grant execution rights.
type loadedJournalRecord struct {
	ordinary    *ordinaryRecord
	native      *nativeJournalSnapshots
	nativeCodec durable.SnapshotCodec
}

func (record loadedJournalRecord) Initialized() bool {
	return record.ordinary != nil && record.native == nil && record.nativeCodec == nil && record.ordinary.Validate() == nil ||
		record.native != nil && record.ordinary == nil && record.native.requireNativeValue() == nil && record.nativeCodec != nil
}

func (record loadedJournalRecord) Ordinary() (ordinaryRecord, bool) {
	if !record.Initialized() || record.ordinary == nil {
		return ordinaryRecord{}, false
	}
	return *record.ordinary, true
}

func (record loadedJournalRecord) Native() (nativeJournalSnapshots, bool) {
	if !record.Initialized() || record.native == nil {
		return nativeJournalSnapshots{}, false
	}
	return *record.native, true
}

// OperationID returns the decoded operation identity, or an empty string for zero.
func (record loadedJournalRecord) OperationID() string {
	if !record.Initialized() {
		return ""
	}
	if record.ordinary != nil {
		return record.ordinary.OperationID()
	}
	return record.native.OperationID()
}

func (record loadedJournalRecord) AuthorityFingerprint(ctx context.Context, codec durable.SnapshotCodec) (string, error) {
	if !record.Initialized() {
		return "", fmt.Errorf("loaded recovery journal record is required")
	}
	if record.ordinary != nil {
		return record.ordinary.AuthorityFingerprint(ctx, codec)
	}
	journal := *record.native
	content, err := marshalNativeJournalRecord(ctx, journal.record, journal.before, journal.after, record.nativeCodec)
	if err != nil {
		return "", err
	}
	return recoveryJournalRecordFingerprint(content), nil
}

func decodeLegacyInventoryJournal(ctx context.Context, snapshot mutationfs.RegularFileSnapshot, path string, codec durable.SnapshotCodec) (loadedJournalRecord, error) {
	if err := ctx.Err(); err != nil {
		return loadedJournalRecord{}, err
	}
	journal, err := decodeOrdinarySnapshot(snapshot, path, codec)
	if err != nil {
		return loadedJournalRecord{}, err
	}
	return loadedJournalRecord{ordinary: &journal}, nil
}

func decodeNativeInventoryJournal(ctx context.Context, snapshot mutationfs.RegularFileSnapshot, path string, codec durable.SnapshotCodec) (loadedJournalRecord, error) {
	if err := ctx.Err(); err != nil {
		return loadedJournalRecord{}, err
	}
	if snapshot.Identity() == nil || snapshot.Identity().Kind() != mutationfs.EntryKindFile || snapshot.Mode().Perm() != recoveryJournalMode {
		return loadedJournalRecord{}, fmt.Errorf("native inventory journal %q requires a private regular-file snapshot", path)
	}
	content := snapshot.Content()
	if int64(len(content)) > maximumRecoveryJournalBytes {
		return loadedJournalRecord{}, fmt.Errorf("native inventory journal exceeds the journal byte envelope")
	}
	version, err := jsonstrict.ValidateVersionedObject(content, "recovery inventory journal", maximumRecoveryJournalJSONDepth)
	if err != nil {
		return loadedJournalRecord{}, err
	}
	if version != nativeRecoveryJournalVersion {
		return loadedJournalRecord{}, unsupportedRecoveryJournalVersion(version)
	}
	journal, err := decodeNativeJournalRecord(ctx, content, codec)
	if err != nil {
		return loadedJournalRecord{}, err
	}
	return loadedJournalRecord{native: &journal, nativeCodec: codec}, nil
}
