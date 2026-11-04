package journal

import (
	"bytes"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestNativeRecordAliasAndAnchorQueriesReturnCompleteIndependentFacts(t *testing.T) {
	fixture := nativeJournalFixture(t)
	value := nativeJournalSnapshots{record: fixture.record, owner: fixture.claims.owner, before: fixture.before, after: fixture.after}
	aliases, err := value.DocumentAliases()
	if err != nil || len(aliases) != 1 || len(aliases[0]) != 1 || aliases[0][0] != fixture.claims.alias {
		t.Fatal("complete aliases changed", err)
	}
	aliases[0][0] = aggregate.DocumentAddress{}
	again, err := value.DocumentAliases()
	if err != nil || again[0][0] != fixture.claims.alias {
		t.Fatal("caller changed recorded aliases", err)
	}
	anchors, err := value.RecordedRootAnchors()
	manifest, manifestErr := fixture.record.ManifestRootProvenance.canonical()
	if err != nil || manifestErr != nil || anchors[output.RootProject] != manifest {
		t.Fatal("recorded manifest anchor changed", err, manifestErr)
	}
	delete(anchors, output.RootProject)
	againAnchors, err := value.RecordedRootAnchors()
	if err != nil || againAnchors[output.RootProject] != manifest {
		t.Fatal("caller changed recorded anchors", err)
	}
	bad := value.record.DocumentGroups[0].Aliases[0]
	bad.Address.Destination = ""
	value.record.DocumentGroups[0].Aliases = append(value.record.DocumentGroups[0].Aliases, bad)
	if result, err := value.DocumentAliases(); err == nil || result != nil {
		t.Fatal("invalid trailing alias supplied partial groups")
	}
	if result, err := value.RecordedRootAnchors(); err == nil || result != nil {
		t.Fatal("invalid trailing alias supplied partial anchors")
	}
}

func TestNativeRecordEncodingKeepsTheExistingFamilyAdmission(t *testing.T) {
	fixture := nativeJournalFixture(t)
	value := nativeJournalSnapshots{record: fixture.record, owner: fixture.claims.owner, before: fixture.before, after: fixture.after}
	want, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	got, err := value.Encode(t.Context(), fixture.codec)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("native encode changed admitted bytes", err)
	}
	value.record.OperationID = ""
	if content, err := value.Encode(t.Context(), fixture.codec); err == nil || content != nil {
		t.Fatal("native encode bypassed header admission")
	}
}

func TestNativeRecordTransitionsAndAliasQueriesRefuseZero(t *testing.T) {
	zero := nativeJournalSnapshots{}
	if values, err := zero.DocumentAliases(); err == nil || values != nil {
		t.Fatal("zero supplied aliases")
	}
	if values, err := zero.RecordedRootAnchors(); err == nil || values != nil {
		t.Fatal("zero supplied anchors")
	}
	if _, err := zero.ProvisionalAcquireEntry(ownership.ProvisionalAcquireIntent{}); err == nil {
		t.Fatal("zero supplied an expected entry")
	}
	if value, err := zero.PromoteProvisionalAcquire(ownership.ProvisionalAcquireIntent{}, ownershipmutation.ClaimTransition{}); err == nil || value.requireNativeValue() == nil {
		t.Fatal("zero supplied ordinary promotion")
	}
	if value, changed, err := zero.PromoteDocument(t.Context(), 0, nil, pathauthority.Exact{}); err == nil || changed || value.requireNativeValue() == nil {
		t.Fatal("zero supplied document promotion")
	}
	if content, err := zero.Encode(t.Context(), testStateCodec()); err == nil || content != nil {
		t.Fatal("zero supplied encoded bytes")
	}
}

func TestNativeOrdinaryPromotionKeepsFactsSeparateFromFamilyAdmission(t *testing.T) {
	fixture := newPromotionFixture(t, journalTestFilesystem())
	capability, _, ordinary, _, err := readPromotionRecord(t.Context(), fixture.filesystem, fixture.authority, testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	if err := capability.Close(); err != nil {
		t.Fatal(err)
	}
	value := nativeJournalSnapshots{record: nativeJournalRecord{
		Version: nativeRecoveryJournalVersion, OperationID: ordinary.OperationID(),
		Entries: ordinary.record.Entries, ClaimTransitions: ordinary.record.ClaimTransitions, ProvisionalAcquires: ordinary.record.ProvisionalAcquires,
	}}
	if _, err := value.ProvisionalAcquireEntry(fixture.intent); err != nil {
		t.Fatal("matching ordinary entry was lost", err)
	}
	promoted, err := value.PromoteProvisionalAcquire(fixture.intent, fixture.transition)
	if err != nil {
		t.Fatal(err)
	}
	intents, err := promoted.ProvisionalAcquires()
	if err != nil || len(intents) != 0 {
		t.Fatal("promotion retained its matching intent", err)
	}
	transitions, err := promoted.ClaimTransitions()
	if err != nil || len(transitions) != 1 || !transitions[0].Equal(fixture.transition) {
		t.Fatal("promotion changed its exact transition", err)
	}
	original, err := value.ProvisionalAcquires()
	if err != nil || len(original) != 1 || !original[0].Equal(fixture.intent) {
		t.Fatal("promotion changed its input facts", err)
	}
	if content, err := promoted.Encode(t.Context(), testStateCodec()); err == nil || content != nil {
		t.Fatal("promoted facts without a complete native header bypassed family admission")
	}
}
