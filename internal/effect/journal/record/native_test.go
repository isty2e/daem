package record

import (
	"bytes"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
)

func TestNativeCaptureBoxesOnlySealedGroupsWithoutMovingFamilyAdmission(t *testing.T) {
	fixture := nativeJournalFixture(t)
	createdAt, err := time.Parse(time.RFC3339, fixture.record.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := fixture.record.ManifestRootProvenance.canonical()
	if err != nil {
		t.Fatal(err)
	}
	groups := []SealedDocumentGroup{{record: fixture.record.DocumentGroups[0], initialized: true}}
	value, err := NewNativeCapture(fixture.record.OperationID, createdAt, fixture.claims.owner, manifest, fixture.before, fixture.after, groups)
	if err != nil {
		t.Fatal(err)
	}
	want, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	groups[0] = SealedDocumentGroup{}
	got, err := value.Encode(t.Context(), fixture.codec)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("native capture changed bytes or retained the caller group array", err)
	}
	if result, err := NewNativeCapture(fixture.record.OperationID, createdAt, fixture.claims.owner, manifest, fixture.before, fixture.after, groups); err == nil || result.requireNativeValue() == nil {
		t.Fatal("zero sealed group produced a native value")
	}
	if result, err := NewNativeCapture(fixture.record.OperationID, createdAt, stateauthority.Authority{}, manifest, fixture.before, fixture.after, nil); err == nil || result.requireNativeValue() == nil {
		t.Fatal("zero owner produced a native value")
	}
	empty, err := NewNativeCapture(fixture.record.OperationID, createdAt, fixture.claims.owner, manifest, fixture.before, fixture.after, nil)
	if err != nil {
		t.Fatal(err)
	}
	if present, err := empty.HasDocumentGroups(); err != nil || present {
		t.Fatal("empty captured facts were confused with zero Native", err)
	}
	if aliases, err := empty.DocumentAliases(); err != nil || len(aliases) != 0 {
		t.Fatal("empty captured aliases were confused with zero Native", err)
	}
	unadmitted, err := NewNativeCapture("", createdAt, fixture.claims.owner, manifest, fixture.before, fixture.after, []SealedDocumentGroup{{record: fixture.record.DocumentGroups[0], initialized: true}})
	if err != nil {
		t.Fatal("family header admission moved into construction", err)
	}
	if content, err := unadmitted.Encode(t.Context(), fixture.codec); err == nil || content != nil {
		t.Fatal("incomplete native header bypassed encoding admission")
	}
}

func TestNativeRecordQueriesDistinguishZeroFromEmptyFamilyFacts(t *testing.T) {
	zero := Native{}
	if entries, err := zero.RecordedEntries(); err == nil || len(entries) != 0 {
		t.Fatal("zero native supplied ordinary entry facts")
	}
	if documents, err := zero.Documents(); err == nil || len(documents) != 0 {
		t.Fatal("zero native supplied document facts")
	}
	if entries, err := zero.OrdinaryEntries(t.Context()); err == nil || len(entries) != 0 {
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
	value := fixture.nativeValue(t)
	if zero.MatchesContext(fixture.claims.owner, fixture.before, fixture.after) || !value.MatchesContext(fixture.claims.owner, fixture.before, fixture.after) {
		t.Fatal("native context did not distinguish initialized family facts")
	}
	if entries, err := value.RecordedEntries(); err != nil || len(entries) != 0 {
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
	fixture.record.DocumentGroups = nil
	value = fixture.nativeValue(t)
	if present, err := value.HasDocumentGroups(); err != nil || present {
		t.Fatal("valid no-document facts were confused with a zero native value", err)
	}
	if value.RequireDocumentOnly() == nil {
		t.Fatal("no-document record passed document-only staging admission")
	}
}

func TestNativeRecoveryQueriesKeepHeadersAndRefuseInvalidRemovalFacts(t *testing.T) {
	fixture := nativeJournalFixture(t)
	value := fixture.nativeValue(t)
	if !value.Owner().Equal(fixture.claims.owner) || !value.Before().Equal(fixture.before) || !value.After().Equal(fixture.after) || value.OperationID() != fixture.record.OperationID {
		t.Fatal("native header projection changed complete operation facts")
	}
	zero := Native{}
	if zero.OperationID() != "" || !zero.Owner().Equal(stateauthority.Authority{}) || !zero.Before().Equal(durable.Snapshot{}) || !zero.After().Equal(durable.Snapshot{}) {
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
	document, err := value.DocumentAt(0)
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
