package qualification_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	"github.com/isty2e/daem/internal/assurance/durable"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
)

func preparedNativeFixture(t *testing.T) (nativeRecordFixture, journalrecord.Native, journaldocument.Comparison) {
	t.Helper()
	fixture := nativeJournalFixture(t)
	return fixture, fixture.journal, fixture.comparison
}

func assertNoPreparedDocumentRecord(t *testing.T, value journalrecord.PreparedDocuments, err error, codec durable.SnapshotCodec) {
	t.Helper()
	if err == nil {
		t.Fatal("invalid preparation returned no error")
	}
	if comparisons, err := value.Comparisons(); err == nil || comparisons != nil {
		t.Fatal("invalid preparation supplied complete or partial staging comparisons")
	}
	if encoded, err := value.Encode(t.Context(), codec); err == nil || encoded != nil {
		t.Fatal("invalid preparation produced an encodable record")
	}
}

func assertNoPreparedNativeDocuments(t *testing.T, value journalrecord.LoadedDocuments, err error) {
	t.Helper()
	if err == nil || value.Len() != 0 {
		t.Fatalf("invalid preparation exposed a complete or partial document result: %v", err)
	}
}

func TestNativeDocumentReaderKeepsMetadataRefusalBeforeImageReads(t *testing.T) {
	for _, kind := range []string{"selection", "claim"} {
		t.Run(kind, func(t *testing.T) {
			fixture := nativeJournalFixture(t)
			decoded := publishedNativeGroupVariant(t, fixture, func(group map[string]json.RawMessage) {
				if kind == "selection" {
					var subjects []map[string]json.RawMessage
					if err := json.Unmarshal(group["selected_subjects"], &subjects); err != nil {
						t.Fatal(err)
					}
					if len(subjects) != 1 {
						t.Fatal("metadata fixture requires one selected subject")
					}
					var name string
					if err := json.Unmarshal(subjects[0]["name"], &name); err != nil {
						t.Fatal(err)
					}
					subjects[0]["name"] = nativeRecordJSON(t, name+"-not-in-state")
					group["selected_subjects"] = nativeRecordJSON(t, subjects)
				} else {
					group["exclusive_claims"] = json.RawMessage(`{"kind":"absent"}`)
				}
			})
			reader := &preparedNativeReadCounter{RootedReader: fixture.read.Filesystem}
			fixture.read.Filesystem = reader
			loaded, err := journalqualification.LoadDocuments(decoded, t.Context(), fixture.read)
			assertNoPreparedNativeDocuments(t, loaded, err)
			if reader.reads != 0 {
				t.Fatal("metadata refusal eagerly read private images")
			}
		})
	}
}

func TestPreparedNativeDocumentsDoNotReadUnavailablePrivateImages(t *testing.T) {
	fixture := nativeJournalFixture(t)
	session, err := fixture.journal.BeginLoadedDocuments(t.Context(), fixture.read.Limits, fixture.read.Codecs)
	if err != nil {
		t.Fatal(err)
	}
	demand, present, err := session.Next(t.Context())
	if err != nil || !present {
		t.Fatal("fixture requires an image demand", err)
	}
	reference, err := demand.Original().FileReference()
	if err != nil {
		t.Fatal(err)
	}
	root, err := fixture.read.Artifacts.Authority()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root.PhysicalRoot(), filepath.FromSlash(reference))
	if err := os.Rename(path, path+".unavailable"); err != nil {
		t.Fatal(err)
	}
	remaining := fixture.read.Budget.RemainingBytes()
	prepared, err := fixture.journal.PrepareDocuments(t.Context(), []journaldocument.Comparison{fixture.comparison}, fixture.read.Limits, fixture.read.Codecs)
	if err != nil {
		t.Fatal("pre-artifact preparation read private images", err)
	}
	comparisons, err := prepared.Comparisons()
	if err != nil || len(comparisons) != 1 || !comparisons[0].ExpectedDocument().Equal(fixture.comparison.ExpectedDocument()) {
		t.Fatal("pre-artifact preparation lost its complete comparison", err)
	}
	if fixture.read.Budget.RemainingBytes() != remaining {
		t.Fatal("pure preparation consumed image bytes")
	}
	loaded, err := journalqualification.LoadDocuments(fixture.journal, t.Context(), fixture.read)
	assertNoPreparedNativeDocuments(t, loaded, err)
}

type preparedNativeReadCounter struct {
	mutationfs.RootedReader
	reads int
}

func (reader *preparedNativeReadCounter) ReadRootedRegularFileUpTo(ctx context.Context, capability rootedpath.CommitCapability, maximumBytes int64) ([]byte, fs.FileMode, mutationfs.EntryIdentity, error) {
	reader.reads++
	return reader.RootedReader.ReadRootedRegularFileUpTo(ctx, capability, maximumBytes)
}

func TestQualifiedNativeRecordJoinsActualRootsImagesAndSemanticRollback(t *testing.T) {
	fixture := nativeJournalFixture(t)
	roots := fixture.roots
	reader := &nativeNamespaceReader{RootedReader: fixture.read.Filesystem}
	fixture.read.Filesystem = reader
	loaded, err := journalqualification.QualifyDocuments(fixture.journal, t.Context(), fixture.read, roots)
	recorded, present := loaded.Records().At(0)
	namespace, qualified := loaded.NamespaceAt(0)
	if err != nil || !qualified || loaded.Len() != 1 || !present || reader.imageReads != 2 {
		t.Fatalf("live namespace + complete image/record join: %v", err)
	}
	capability, err := namespace.AcquireReferent(t.Context(), fixture.read.Budget)
	if err != nil {
		t.Fatal(err)
	}
	host, mode, _, readErr := journalTestFilesystem().ReadRootedRegularFileUpTo(t.Context(), capability, 4096)
	closeErr := capability.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(host, recorded.Images().Original().Document().Content()) {
		t.Fatalf("qualified capability did not read the retained original host: %v, %v", readErr, closeErr)
	}
	restored, err := recorded.Group().Comparison().RestoreBefore(t.Context(), recorded.Images().Expected().Document(), recovery.NewPermissionMode(mode))
	if err != nil {
		t.Fatalf("qualified record lost semantic rollback: %v", err)
	}
	var values struct {
		Model   bool
		Outside int
	}
	if err := json.Unmarshal(restored.Document().Content(), &values); err != nil || !values.Model || values.Outside != 1 {
		t.Fatalf("selected rollback lost before values or protected foreign data: %+v, %v", values, err)
	}
	after, err := os.ReadFile(filepath.Join(namespacetest.LegacyRoot(t, roots, output.RootPiAgent).SelectedPath, "custom.json"))
	if err != nil || !bytes.Equal(host, after) {
		t.Fatalf("read/reconstruction mutated host bytes: %v", err)
	}
}

func TestQualifiedNativeWriterRefusesProtectedArtifactRoot(t *testing.T) {
	fixture := nativeJournalFixture(t)
	roots := fixture.roots
	read := fixture.read
	read.Artifacts = namespacetest.LegacyRoot(t, roots, output.RootProject).Root
	journal := fixture.journal
	namespaces, err := journalqualification.LoadNamespaces(journal, t.Context(), read, roots)
	if err == nil || namespaces != nil {
		t.Fatal("native writer inside the protected artifact root was admitted", err)
	}
}

func TestQualifiedNativeManifestRequiresRecordedLocatorWithTheSameProvenance(t *testing.T) {
	fixture := nativeJournalFixture(t)
	manifestReference, err := fixture.journal.ManifestRootReference()
	if err != nil {
		t.Fatal(err)
	}
	other, err := output.NewRootReference(manifestReference.Identity(), "fixture-other-project-revision")
	if err != nil {
		t.Fatal(err)
	}
	anchors, err := fixture.journal.RecordedRootAnchors()
	if err != nil {
		t.Fatal(err)
	}
	var roots journalnamespace.Roots
	for reference := range anchors {
		selected, supplied := fixture.roots.Selection(reference)
		if !supplied {
			t.Fatal("fixture omitted a recorded root")
		}
		if reference == manifestReference {
			reference = other
		}
		roots, err = roots.With(reference, selected)
		if err != nil {
			t.Fatal(err)
		}
	}
	grants, err := roots.Qualify(t.Context(), fixture.read.Budget)
	if err != nil {
		t.Fatal(err)
	}
	manifest, present := grants.Manifest()
	if !present || manifest.RootReference() != other || !fixture.journal.MatchesManifestRoot(manifest.Provenance()) {
		t.Fatal("fixture did not isolate the recorded locator from manifest provenance")
	}
	if namespaces, err := journalqualification.LoadNamespaces(fixture.journal, t.Context(), fixture.read, roots); err == nil || namespaces != nil {
		t.Fatal("matching provenance substituted for the recorded manifest locator", err)
	}
}

func TestRecordedNativeAliasLocationDoesNotSelectTheCurrentBinding(t *testing.T) {
	fixture := nativeJournalFixture(t)
	documents, err := fixture.journal.Documents()
	if err != nil || len(documents) != 1 || len(documents[0].Aliases()) != 1 {
		t.Fatal("fixture does not provide one historical alias", err)
	}
	location, err := documents[0].Aliases()[0].Location()
	if err != nil || location.RootReference().Identity() != "pi-agent" || location.RootReference().LocatorPolicy() != "pi-agent-location-v1" {
		t.Fatal("recorded alias followed the current locator selection", location, err)
	}
	manifestReference, err := fixture.journal.ManifestRootReference()
	if err != nil {
		t.Fatal(err)
	}
	var roots journalnamespace.Roots
	for reference, selected := range map[output.RootReference]journalnamespace.RootSelection{
		manifestReference:        namespacetest.LegacyRoot(t, fixture.roots, output.RootProject),
		location.RootReference(): namespacetest.LegacyRoot(t, fixture.roots, output.RootPiAgent),
	} {
		roots, err = roots.With(reference, selected)
		if err != nil {
			t.Fatal(err)
		}
	}
	namespaces, err := journalqualification.LoadNamespaces(fixture.journal, t.Context(), fixture.read, roots)
	if err != nil || len(namespaces) != 1 {
		t.Fatal("historical alias could not use its independently selected recorded root", err)
	}
	alias := namespaces[0].Aliases()[0]
	if alias.Location() != location {
		t.Fatal("qualified alias replaced its recorded root-relative location")
	}
	if _, known := alias.Binding(); known {
		t.Fatal("native16 qualification invented a document policy")
	}
	if _, _, err := namespaces[0].WriterBinding(t.Context(), fixture.read.Budget); err != nil {
		t.Fatal("qualified writer reinterpreted historical alias facts", err)
	}
}

func TestZeroNativeQualificationRefusesBeforeRootQualification(t *testing.T) {
	_, err := journalqualification.LoadNamespaces(journalrecord.Native{}, t.Context(), journalqualification.ReadOptions{
		Filesystem: journalTestFilesystem(), Budget: nativeFixtureBudget(t),
	}, journalnamespace.Roots{})
	if err == nil || err.Error() != "native journal record is required" {
		t.Fatal("root selection obscured the uninitialized record refusal", err)
	}
}
