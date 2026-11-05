package record

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

// Loaded records retain decoded family facts and native codec custody. Neither
// this value nor its projections prove publication or grant execution rights.
type Loaded struct {
	ordinary    *Ordinary
	native      *Native
	nativeCodec durable.SnapshotCodec
}

func (record Loaded) Initialized() bool {
	return record.ordinary != nil && record.native == nil && record.nativeCodec == nil && record.ordinary.Validate() == nil ||
		record.native != nil && record.ordinary == nil && record.native.requireNativeValue() == nil && record.nativeCodec != nil
}

func (record Loaded) Ordinary() (Ordinary, bool) {
	if !record.Initialized() || record.ordinary == nil {
		return Ordinary{}, false
	}
	return *record.ordinary, true
}

func (record Loaded) Native() (Native, bool) {
	if !record.Initialized() || record.native == nil {
		return Native{}, false
	}
	return *record.native, true
}

// OperationID returns the decoded operation identity, or an empty string for zero.
func (record Loaded) OperationID() string {
	if !record.Initialized() {
		return ""
	}
	if record.ordinary != nil {
		return record.ordinary.OperationID()
	}
	return record.native.OperationID()
}

func (record Loaded) AuthorityFingerprint(ctx context.Context, codec durable.SnapshotCodec) (string, error) {
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
	return Fingerprint(content), nil
}

func DecodeOrdinarySnapshot(ctx context.Context, snapshot mutationfs.RegularFileSnapshot, path string, codec durable.SnapshotCodec) (Loaded, error) {
	if err := ctx.Err(); err != nil {
		return Loaded{}, err
	}
	journal, err := decodeOrdinarySnapshot(snapshot, path, codec)
	if err != nil {
		return Loaded{}, err
	}
	return Loaded{ordinary: &journal}, nil
}

func DecodeNativeSnapshot(ctx context.Context, snapshot mutationfs.RegularFileSnapshot, path string, codec durable.SnapshotCodec) (Loaded, error) {
	if err := ctx.Err(); err != nil {
		return Loaded{}, err
	}
	if snapshot.Identity() == nil || snapshot.Identity().Kind() != mutationfs.EntryKindFile || snapshot.Mode().Perm() != Mode {
		return Loaded{}, fmt.Errorf("native inventory journal %q requires a private regular-file snapshot", path)
	}
	content := snapshot.Content()
	if int64(len(content)) > MaximumBytes {
		return Loaded{}, fmt.Errorf("native inventory journal exceeds the journal byte envelope")
	}
	version, err := jsonstrict.ValidateVersionedObject(content, "recovery inventory journal", MaximumJSONDepth)
	if err != nil {
		return Loaded{}, err
	}
	if version != NativeVersion {
		return Loaded{}, UnsupportedVersion(version)
	}
	journal, err := decodeNativeJournalRecord(ctx, content, codec)
	if err != nil {
		return Loaded{}, err
	}
	return Loaded{native: &journal, nativeCodec: codec}, nil
}
