package journal

import (
	"bytes"
	"testing"
	"time"
)

func TestOrdinaryCaptureEvidenceEnclosesNativeJoinWithoutChangingInputs(t *testing.T) {
	fixture := mixedStageFixture(t)
	source, _ := fixture.source(t, imageBudget(t))
	before := nativeRecordJSON(t, fixture.journal.record)
	entries := nativeRecordJSON(t, source.record.entries)
	views, err := source.record.recordedEntries()
	if err != nil {
		t.Fatal(err)
	}
	joined, err := fixture.journal.withOrdinaryCapture(source.record)
	if err != nil {
		t.Fatalf("matching complete capture facts did not join: %v", err)
	}
	joinedViews, err := joined.recordedEntries()
	if err != nil || len(joinedViews) != len(views) || len(joinedViews) == 0 {
		t.Fatalf("native join lost its entry facts: %v", err)
	}
	joined.record.Entries[0].Path += "-changed"
	if !bytes.Equal(before, nativeRecordJSON(t, fixture.journal.record)) || !bytes.Equal(entries, nativeRecordJSON(t, source.record.entries)) {
		t.Fatal("native join exposed a shared mutable record array")
	}
	wrong := source.record
	wrong.operationID = "other"
	if result, err := fixture.journal.withOrdinaryCapture(wrong); err == nil || result.record.Version != 0 {
		t.Fatal("foreign capture facts returned a partial native record")
	}
}

func TestOrdinaryCaptureEvidenceRefusesZeroWithoutARecord(t *testing.T) {
	zero := ordinaryCaptureMaterial{}
	if entries, err := zero.recordedEntries(); err == nil || len(entries) != 0 {
		t.Fatal("zero evidence supplied capture facts")
	}
	if content, err := zero.encodeOrdinary([]byte(`{}`), []byte(`{}`)); err == nil || len(content) != 0 {
		t.Fatal("zero evidence produced an ordinary record")
	}
	if record, err := (nativeJournalSnapshots{}).withOrdinaryCapture(zero); err == nil || record.record.Version != 0 {
		t.Fatal("zero evidence joined a native record")
	}
}

func TestOrdinaryCaptureBasisRetainsTimeUntilTheNativeJoin(t *testing.T) {
	fixture := mixedStageFixture(t)
	fixture.journal.record.CreatedAt = "2026-11-04T12:00:00+09:00"
	basis, err := fixture.journal.ordinaryCaptureBasis()
	if err != nil || basis.Validate() != nil || basis.CreatedAt().Format(time.RFC3339) != fixture.journal.record.CreatedAt {
		t.Fatalf("capture basis changed the admitted metadata: %v", err)
	}
	source, _ := fixture.source(t, imageBudget(t))
	if _, err := fixture.journal.withOrdinaryCapture(source.record); err == nil {
		t.Fatal("native join lost its original-versus-UTC header comparison")
	}
	if err := (ordinaryCaptureBasis{}).Validate(); err == nil {
		t.Fatal("zero basis supplied capture inputs")
	}
}
