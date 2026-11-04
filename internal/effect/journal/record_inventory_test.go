package journal

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func privateJournalSnapshotForTest(t *testing.T, content []byte) mutationfs.RegularFileSnapshot {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "journal.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := journalTestFilesystem().ReadRegularFileSnapshotUpTo(t.Context(), path, maximumRecoveryJournalBytes)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestLoadedRecordKeepsNativeAdmissionCodecAndOrdinaryCallerCodec(t *testing.T) {
	fixture := nativeJournalFixture(t)
	content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	native, err := decodeNativeInventoryJournal(t.Context(), privateJournalSnapshotForTest(t, content), "journal.json", fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	caller := &rejectingFingerprintCodec{}
	fingerprint, err := native.AuthorityFingerprint(t.Context(), caller)
	if err != nil || fingerprint != recoveryJournalRecordFingerprint(content) || caller.calls != 0 {
		t.Fatalf("native fingerprint lost its admission codec: %q %v (%d calls)", fingerprint, err, caller.calls)
	}
	projected, ok := native.Native()
	if !native.Initialized() || !ok || !projected.owner.Equal(fixture.claims.owner) || !projected.before.Equal(fixture.before) {
		t.Fatal("native decode lost family facts")
	}
	if _, ok := native.Ordinary(); ok {
		t.Fatal("native value projected an ordinary family")
	}
	ordinaryContent, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := decodeLegacyInventoryJournal(t.Context(), privateJournalSnapshotForTest(t, ordinaryContent), "journal.json", testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ordinary.AuthorityFingerprint(t.Context(), caller); !errors.Is(err, errRejectedFingerprintCodec) || caller.calls != 1 {
		t.Fatalf("ordinary fingerprint silently bound its decode codec: %v (%d calls)", err, caller.calls)
	}
	if value, ok := ordinary.Ordinary(); !ok || value.OperationID() != testOperationID {
		t.Fatal("ordinary decode lost its family identity")
	}
	if _, ok := ordinary.Native(); ok {
		t.Fatal("ordinary value projected a native family")
	}
}

func TestLoadedRecordZeroDoesNotSupplyDecodedFamilyFacts(t *testing.T) {
	zero := loadedJournalRecord{}
	if zero.Initialized() || zero.OperationID() != "" {
		t.Fatal("zero record appeared decoded")
	}
	if _, ok := zero.Ordinary(); ok {
		t.Fatal("zero record projected ordinary facts")
	}
	if _, ok := zero.Native(); ok {
		t.Fatal("zero record projected native facts")
	}
	if fingerprint, err := zero.AuthorityFingerprint(t.Context(), testStateCodec()); err == nil || fingerprint != "" {
		t.Fatal("zero record supplied a fingerprint")
	}
}

var errRejectedFingerprintCodec = errors.New("fingerprint caller codec rejected")

type rejectingFingerprintCodec struct{ calls int }

func (codec *rejectingFingerprintCodec) Encode(durable.Snapshot) ([]byte, error) {
	codec.calls++
	return nil, errRejectedFingerprintCodec
}

func (*rejectingFingerprintCodec) Decode([]byte) (durable.Snapshot, error) {
	return durable.Snapshot{}, errRejectedFingerprintCodec
}
