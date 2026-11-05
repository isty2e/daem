package record

import (
	"bytes"
	"testing"
)

func TestAdmittedOrdinaryRecordKeepsEncodingAndPlanningFacts(t *testing.T) {
	fixture := ordinaryPromotionRecordFixture(t)
	record := fixture.record
	encoded, err := record.Encode(testStateCodec())
	if err != nil || !bytes.Equal(encoded, fixture.content) || record.OperationID() != fixture.intent.OperationID() {
		t.Fatalf("admitted ordinary facts did not preserve their artifact: %v", err)
	}
	basis, err := record.RecoveryPlanningBasis(fixture.directory, fixture.fingerprint, nil)
	if err != nil || basis.Validate() != nil || len(basis.AllEntries()) != 1 || len(basis.ProvisionalAcquires()) != 1 {
		t.Fatalf("admitted family lost its planning facts: %v", err)
	}
}

func TestOrdinaryRecordZeroRefusesUsableMetadataAndConsumers(t *testing.T) {
	zero := Ordinary{}
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
