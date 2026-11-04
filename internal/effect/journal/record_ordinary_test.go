package journal

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestAdmittedOrdinaryRecordKeepsEncodingAndPlanningFacts(t *testing.T) {
	fixture := newPromotionFixture(t, journalTestFilesystem())
	capability, snapshot, record, fingerprint, err := readPromotionRecord(t.Context(), fixture.filesystem, fixture.authority, testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	if err := capability.Close(); err != nil {
		t.Fatal(err)
	}
	encoded, err := record.Encode(testStateCodec())
	if err != nil || !bytes.Equal(encoded, snapshot.Content()) || record.OperationID() != fixture.intent.OperationID() {
		t.Fatalf("admitted ordinary facts did not preserve their artifact: %v", err)
	}
	basis, err := record.RecoveryPlanningBasis(filepath.Dir(fixture.recordPath), fingerprint, nil)
	if err != nil || basis.Validate() != nil || len(basis.AllEntries()) != 1 || len(basis.ProvisionalAcquires()) != 1 {
		t.Fatalf("admitted family lost its planning facts: %v", err)
	}
}

func TestOrdinaryRecordZeroRefusesUsableMetadataAndConsumers(t *testing.T) {
	zero := ordinaryRecord{}
	if zero.Validate() == nil || zero.SameAuthorityFingerprint(zero, testStateCodec()) {
		t.Fatal("zero family record appeared admitted")
	}
	if encoded, err := zero.Encode(testStateCodec()); err == nil || len(encoded) != 0 {
		t.Fatal("zero record supplied bytes")
	}
	if basis, err := zero.RecoveryPlanningBasis(testOperationDir, "unused", nil); err == nil || basis.Validate() == nil {
		t.Fatal("zero record supplied planning facts")
	}
}
