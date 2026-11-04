package journal

import "testing"

func TestNativeRecordQueriesDistinguishZeroFromEmptyFamilyFacts(t *testing.T) {
	zero := nativeJournalSnapshots{}
	if entries, err := zero.recordedEntries(); err == nil || len(entries) != 0 {
		t.Fatal("zero native supplied ordinary entry facts")
	}
	if documents, err := zero.recordedDocuments(); err == nil || len(documents) != 0 {
		t.Fatal("zero native supplied document facts")
	}
	if entries, err := zero.ordinaryEntries(t.Context()); err == nil || len(entries) != 0 {
		t.Fatal("zero native supplied canonical entries")
	}
	if present, err := zero.HasDocumentGroups(); err == nil || present || zero.RequireDocumentOnly() == nil {
		t.Fatal("zero native supplied staging facts")
	}
	if values, err := zero.ClaimTransitions(); err == nil || len(values) != 0 {
		t.Fatal("zero native supplied valid empty transitions")
	}
	if values, err := zero.ProvisionalAcquires(); err == nil || len(values) != 0 {
		t.Fatal("zero native supplied valid empty intents")
	}
	fixture := nativeJournalFixture(t)
	value := nativeJournalSnapshots{record: fixture.record, owner: fixture.claims.owner, before: fixture.before, after: fixture.after}
	if zero.MatchesContext(fixture.claims.owner, fixture.before, fixture.after) || !value.MatchesContext(fixture.claims.owner, fixture.before, fixture.after) {
		t.Fatal("native context did not distinguish initialized family facts")
	}
	if entries, err := value.recordedEntries(); err != nil || len(entries) != 0 {
		t.Fatal("valid empty ordinary entries were refused", err)
	}
	if present, err := value.HasDocumentGroups(); err != nil || !present || value.RequireDocumentOnly() != nil {
		t.Fatal("valid document-only facts were lost", err)
	}
	if values, err := value.ClaimTransitions(); err != nil || len(values) != 0 {
		t.Fatal("valid empty ordinary transitions were refused", err)
	}
	if values, err := value.ProvisionalAcquires(); err != nil || len(values) != 0 {
		t.Fatal("valid empty ordinary intents were refused", err)
	}
	value.record.DocumentGroups = nil
	if present, err := value.HasDocumentGroups(); err != nil || present {
		t.Fatal("valid no-document facts were confused with a zero native value", err)
	}
	if value.RequireDocumentOnly() == nil {
		t.Fatal("no-document record passed document-only staging admission")
	}
}
