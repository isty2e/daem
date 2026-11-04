package record

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestRecordedDocumentViewsRetainFactsWithoutMutableCollections(t *testing.T) {
	fixture := observedNativeRecordFixture(t)
	content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	views, err := journal.Documents()
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 {
		t.Fatal("fixture requires one actual recorded document")
	}
	addresses, err := views[0].Addresses(t.Context())
	if err != nil || len(addresses) != 1 || addresses[0] != fixture.claims.alias {
		t.Fatalf("recorded aliases changed: %v", err)
	}
	if err := views[0].RequireCapturedNamespace(fixture.observation); err != nil {
		t.Fatal(err)
	}
	if err := views[0].RequireRetainedNamespace(t.Context(), fixture.observation); err != nil {
		t.Fatal(err)
	}
	provenance, relative, err := views[0].Writer()
	if err != nil || provenance != fixture.provenance || relative != fixture.relative {
		t.Fatalf("recorded writer changed: %v", err)
	}
	aliases := views[0].Aliases()
	if !aliases[0].MatchesRoot(fixture.provenance) {
		t.Fatal("alias lost its recorded root")
	}
	if err := aliases[0].RequireObservations(fixture.observation); err != nil {
		t.Fatal(err)
	}

	addresses[0], aliases[0], views[0] = aggregate.DocumentAddress{}, DocumentAlias{}, Document{}
	view, err := journal.DocumentAt(0)
	if err != nil {
		t.Fatal(err)
	}
	addresses, err = view.Addresses(t.Context())
	if err != nil || len(addresses) != 1 || addresses[0] != fixture.claims.alias {
		t.Fatal("caller mutation changed recorded addresses", err)
	}
	address, err := view.Aliases()[0].Address()
	if err != nil || address != fixture.claims.alias || !view.HasExactClaims() {
		t.Fatal("caller mutation changed recorded alias or claim facts", err)
	}
	if _, err := journal.DocumentAt(-1); err == nil {
		t.Fatal("negative document index supplied a record")
	}
	if _, err := journal.DocumentAt(1); err == nil {
		t.Fatal("missing document index supplied a record")
	}
}

func TestRecordedDocumentViewsRejectZeroFacts(t *testing.T) {
	document := Document{}
	if addresses, err := document.Addresses(t.Context()); err == nil || addresses != nil {
		t.Fatal("zero view supplied addresses")
	}
	if _, _, err := document.Writer(); err == nil {
		t.Fatal("zero view supplied writer facts")
	}
	if err := document.RequireCapturedNamespace(mutation.DirectoryEntryAuthorityObservation{}); err == nil {
		t.Fatal("zero view qualified a captured namespace")
	}
	if err := document.RequireRetainedNamespace(t.Context(), mutation.DirectoryEntryAuthorityObservation{}); err == nil {
		t.Fatal("zero view qualified retained ownership")
	}
	if _, present, err := document.PiQualification(); err == nil || present {
		t.Fatal("zero view supplied order facts")
	}
	if _, present, err := document.Removal(); err == nil || present {
		t.Fatal("zero view supplied removal facts")
	}
	if subjects, err := document.SelectedSubjects(t.Context()); err == nil || subjects != nil {
		t.Fatal("zero view supplied subjects")
	}
	if document.HasExactClaims() || document.Aliases() != nil {
		t.Fatal("zero view supplied usable claim or alias facts")
	}

	alias := DocumentAlias{}
	if _, err := alias.Address(); err == nil {
		t.Fatal("zero alias supplied a document address")
	}
	if alias.MatchesRoot(rootedpath.AuthorityProvenance{}) {
		t.Fatal("zero alias qualified a root")
	}
	if err := alias.RequireObservations(mutation.DirectoryEntryAuthorityObservation{}); err == nil {
		t.Fatal("zero alias qualified observations")
	}
}
