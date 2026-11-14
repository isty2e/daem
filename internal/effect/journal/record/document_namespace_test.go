package record

import (
	"maps"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestNativeNamespaceFactsComeFromTheStoredInterpretationBeforeAndAfterEncoding(t *testing.T) {
	fixture := observedNativeRecordFixture(t)
	createdAt, err := time.Parse(time.RFC3339, fixture.record.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := fixture.record.ManifestRootProvenance.canonical()
	if err != nil {
		t.Fatal(err)
	}
	groups := []SealedDocumentGroup{{record: fixture.record.DocumentGroups[0], initialized: true}}
	captured, err := NewNativeCapture(fixture.record.OperationID, createdAt, fixture.claims.owner, manifest, fixture.before, fixture.after, groups)
	if err != nil {
		t.Fatal(err)
	}
	content, err := captured.Encode(t.Context(), fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	var expectedRoots map[output.RootReference]rootedpath.AuthorityProvenance
	for _, value := range []Native{captured, decoded} {
		view, err := value.DocumentAt(0)
		if err != nil {
			t.Fatal(err)
		}
		pin, err := view.WriterPin()
		if err != nil || pin.Validate() != nil || pin.Provenance() != fixture.provenance {
			t.Fatal("namespace lowering changed the recorded writer", err)
		}
		if _, bound := pin.RootReference(); bound {
			t.Fatal("native16 lowering invented a writer root reference")
		}
		if child, directory := pin.Coordinate().DirectoryChild(); !directory || child != fixture.relative.Path() {
			t.Fatal("namespace lowering changed the physical writer coordinate", child)
		}
		aliases := view.Aliases()
		location, err := aliases[0].Location()
		if err != nil {
			t.Fatal(err)
		}
		frozen, err := native16RootBindingForAlias(fixture.claims.alias)
		if err != nil || location != frozen {
			t.Fatal("capture and decode do not share the frozen alias interpretation", err)
		}
		roots, err := value.RecordedRootAnchors()
		if err != nil {
			t.Fatal(err)
		}
		if expectedRoots == nil {
			expectedRoots = maps.Clone(roots)
		} else if !maps.Equal(expectedRoots, roots) {
			t.Fatal("serialization changed namespace root facts")
		}
		clear(roots)
		copied := value
		again, err := copied.RecordedRootAnchors()
		if err != nil || !maps.Equal(expectedRoots, again) {
			t.Fatal("returned map mutation changed shared namespace facts", err)
		}
		copiedView, err := copied.DocumentAt(0)
		if err != nil || !copiedView.Aliases()[0].MatchesRoot(fixture.provenance) {
			t.Fatal("copy lost the recorded alias root", err)
		}
	}
}

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
	pin, err := views[0].WriterPin()
	child, directory := pin.Coordinate().DirectoryChild()
	_, bound := pin.RootReference()
	if err != nil || bound || pin.Provenance() != fixture.provenance || !directory || child != fixture.relative.Path() {
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
	if _, err := document.WriterPin(); err == nil {
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
