package record

import (
	"bytes"
	"testing"
	"time"
)

func TestOrdinaryCaptureEvidenceEnclosesNativeJoinWithoutChangingInputs(t *testing.T) {
	journal, evidence := ordinaryEvidenceFixture(t, "2026-11-04T00:00:00Z")
	before := nativeRecordJSON(t, journal.record)
	entries := nativeRecordJSON(t, evidence.entries)
	views, err := evidence.RecordedEntries()
	if err != nil {
		t.Fatal(err)
	}
	joined, err := journal.WithOrdinaryEvidence(evidence)
	if err != nil {
		t.Fatalf("matching complete capture facts did not join: %v", err)
	}
	joinedViews, err := joined.RecordedEntries()
	if err != nil || len(joinedViews) != len(views) || len(joinedViews) == 0 {
		t.Fatalf("native join lost its entry facts: %v", err)
	}
	joined.record.Entries[0].Path += "-changed"
	if !bytes.Equal(before, nativeRecordJSON(t, journal.record)) || !bytes.Equal(entries, nativeRecordJSON(t, evidence.entries)) {
		t.Fatal("native join exposed a shared mutable record array")
	}
	wrong := evidence
	wrong.operationID = "other"
	if result, err := journal.WithOrdinaryEvidence(wrong); err == nil || result.record.Version != 0 {
		t.Fatal("foreign capture facts returned a partial native record")
	}
}

func TestOrdinaryCaptureEvidenceRefusesZeroWithoutARecord(t *testing.T) {
	zero := OrdinaryEvidence{}
	if entries, err := zero.RecordedEntries(); err == nil || len(entries) != 0 {
		t.Fatal("zero evidence supplied capture facts")
	}
	if content, err := zero.EncodeOrdinary([]byte(`{}`), []byte(`{}`)); err == nil || len(content) != 0 {
		t.Fatal("zero evidence produced an ordinary record")
	}
	if record, err := (Native{}).WithOrdinaryEvidence(zero); err == nil || record.record.Version != 0 {
		t.Fatal("zero evidence joined a native record")
	}
}

func TestOrdinaryCaptureBasisRetainsTimeUntilTheNativeJoin(t *testing.T) {
	journal, evidence := ordinaryEvidenceFixture(t, "2026-11-04T12:00:00+09:00")
	basis, err := journal.OrdinaryCaptureBasis()
	if err != nil || basis.Validate() != nil || basis.CreatedAt().Format(time.RFC3339) != journal.record.CreatedAt {
		t.Fatalf("capture basis changed the admitted metadata: %v", err)
	}
	if _, err := journal.WithOrdinaryEvidence(evidence); err == nil {
		t.Fatal("native join lost its original-versus-UTC header comparison")
	}
	if err := (OrdinaryCaptureBasis{}).Validate(); err == nil {
		t.Fatal("zero basis supplied capture inputs")
	}
}
