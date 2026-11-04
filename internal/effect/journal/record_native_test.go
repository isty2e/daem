package journal

import (
	"testing"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
)

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

func TestNativeRecoveryQueriesKeepHeadersAndRefuseInvalidRemovalFacts(t *testing.T) {
	fixture := nativeJournalFixture(t)
	value := nativeJournalSnapshots{record: fixture.record, owner: fixture.claims.owner, before: fixture.before, after: fixture.after}
	if !value.Owner().Equal(fixture.claims.owner) || !value.Before().Equal(fixture.before) || !value.After().Equal(fixture.after) || value.OperationID() != fixture.record.OperationID {
		t.Fatal("native header projection changed complete operation facts")
	}
	zero := nativeJournalSnapshots{}
	if zero.OperationID() != "" || !zero.Owner().Equal((CapturedNativeJournal{}).Owner()) || !zero.Before().Equal((CapturedNativeJournal{}).BeforeState()) || !zero.After().Equal((CapturedNativeJournal{}).AfterState()) {
		t.Fatal("zero header projection changed the existing value-getter contract")
	}
	if intents, err := zero.OrdinaryRemovalIntents(); err == nil || len(intents) != 0 {
		t.Fatal("zero supplied ordinary removal facts")
	}
	if intents, err := zero.RemovalIntents(); err == nil || len(intents) != 0 {
		t.Fatal("zero supplied combined removal facts")
	}
	if demands, err := zero.RemovalDemands(); err == nil || demands.Len() != 0 {
		t.Fatal("zero supplied removal demands")
	}
	if intents, err := value.RemovalIntents(); err != nil || len(intents) != 0 {
		t.Fatal("initialized empty removal facts were refused", err)
	}
	value.record.DocumentGroups[0].Removal = &recoveryDocumentRemoval{}
	document, err := value.recordedDocumentAt(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, present, err := document.Removal(); err == nil || present {
		t.Fatal("failed removal conversion supplied a present result")
	}
	if intents, err := value.RemovalIntents(); err == nil || len(intents) != 0 {
		t.Fatal("invalid document removal supplied combined intents")
	}
	if demands, err := value.RemovalDemands(); err == nil || demands.Len() != 0 {
		t.Fatal("invalid document removal supplied combined demands")
	}
}

func TestZeroNativeQualificationRefusesBeforeRootQualification(t *testing.T) {
	_, err := (nativeJournalSnapshots{}).qualifyDocumentNamespaces(t.Context(), nativeDocumentReadOptions{
		filesystem: journalTestFilesystem(), budget: imageBudget(t),
	}, journalnamespace.Roots{})
	if err == nil || err.Error() != "native journal record is required" {
		t.Fatal("root selection obscured the uninitialized record refusal", err)
	}
}
