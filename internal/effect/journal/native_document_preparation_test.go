package journal

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestPreparedNativeDocumentsAdmitActualPlanBeforeArtifactsExist(t *testing.T) {
	fixture, decoded, comparison := preparedNativeFixture(t)
	if err := os.Rename(filepath.Join(fixture.record.ManifestRootProvenance.PhysicalRoot, "images"), filepath.Join(fixture.record.ManifestRootProvenance.PhysicalRoot, "unavailable-images")); err != nil {
		t.Fatal(err)
	}
	prepared, err := decoded.prepareDocuments(t.Context(), []DocumentComparison{comparison}, fixture.read.limits, fixture.read.codecs)
	if err != nil || len(prepared.groups) != 1 || len(prepared.claims) != 1 || len(prepared.images) != 1 {
		t.Fatalf("actual prepared plan cannot pass pre-artifact construction: %v", err)
	}
	if _, err := decoded.loadDocuments(t.Context(), fixture.read); err == nil {
		t.Fatal("unavailable artifact fixture unexpectedly remained readable")
	}
	mode := recovery.NewPermissionMode(0o600)
	for _, image := range []loadedDocumentImage{prepared.images[0].original, prepared.images[0].expected} {
		match, err := prepared.groups[0].comparison.Match(t.Context(), image.document, mode)
		if err != nil || !match.Recoverable() {
			t.Fatalf("prepared construction lost an actual comparison image: %+v, %v", match, err)
		}
	}
}

func TestPreparedNativeDocumentsRejectMismatchedImagesAndWholeOperation(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*testing.T, *nativeRecordFixture, *nativeJournalSnapshots, *[]DocumentComparison)
	}{
		{"missing comparison", func(_ *testing.T, _ *nativeRecordFixture, _ *nativeJournalSnapshots, comparisons *[]DocumentComparison) {
			*comparisons = nil
		}},
		{"extra comparison", func(_ *testing.T, _ *nativeRecordFixture, _ *nativeJournalSnapshots, comparisons *[]DocumentComparison) {
			*comparisons = append(*comparisons, (*comparisons)[0])
		}},
		{"zero comparison", func(_ *testing.T, _ *nativeRecordFixture, _ *nativeJournalSnapshots, comparisons *[]DocumentComparison) {
			(*comparisons)[0] = DocumentComparison{}
		}},
		{"wrong original digest", func(_ *testing.T, _ *nativeRecordFixture, journal *nativeJournalSnapshots, _ *[]DocumentComparison) {
			journal.record.DocumentGroups[0].Original.Artifact.Digest = artifact.HashFileContent([]byte("other original"))
		}},
		{"wrong expected digest", func(_ *testing.T, _ *nativeRecordFixture, journal *nativeJournalSnapshots, _ *[]DocumentComparison) {
			journal.record.DocumentGroups[0].Expected.Artifact.Digest = artifact.HashFileContent([]byte("other expected"))
		}},
		{"wrong expected size", func(_ *testing.T, _ *nativeRecordFixture, journal *nativeJournalSnapshots, _ *[]DocumentComparison) {
			size := *journal.record.DocumentGroups[0].Expected.Artifact.Size + 1
			journal.record.DocumentGroups[0].Expected.Artifact.Size = &size
		}},
		{"wrong expected mode", func(_ *testing.T, _ *nativeRecordFixture, journal *nativeJournalSnapshots, _ *[]DocumentComparison) {
			journal.record.DocumentGroups[0].Expected.Mode = recovery.NewPermissionMode(0o640)
		}},
		{"wrong expected presence", func(_ *testing.T, _ *nativeRecordFixture, journal *nativeJournalSnapshots, _ *[]DocumentComparison) {
			journal.record.DocumentGroups[0].Expected = recoveryDocumentImage{Kind: "absent"}
		}},
		{"different limits", func(_ *testing.T, _ *nativeRecordFixture, _ *nativeJournalSnapshots, comparisons *[]DocumentComparison) {
			(*comparisons)[0].limits.MaximumBytes--
		}},
		{"wrong extent", func(_ *testing.T, _ *nativeRecordFixture, _ *nativeJournalSnapshots, comparisons *[]DocumentComparison) {
			(*comparisons)[0].whole = true
		}},
		{"wrong selected frontier", func(_ *testing.T, _ *nativeRecordFixture, _ *nativeJournalSnapshots, comparisons *[]DocumentComparison) {
			(*comparisons)[0].participants = nil
		}},
		{"missing changed group", func(_ *testing.T, _ *nativeRecordFixture, journal *nativeJournalSnapshots, comparisons *[]DocumentComparison) {
			journal.record.DocumentGroups = nil
			*comparisons = nil
		}},
		{"wrong after baseline", func(t *testing.T, fixture *nativeRecordFixture, journal *nativeJournalSnapshots, _ *[]DocumentComparison) {
			row := fixture.after.ManagedAggregates()[0]
			wrong := selectionNativeRow(t, row.Contribution().Contract(), nativeRecordCanonical(t, `"wrong"`))
			journal.after = selectionState(t, wrong)
		}},
		{"wrong binding before", func(_ *testing.T, _ *nativeRecordFixture, journal *nativeJournalSnapshots, _ *[]DocumentComparison) {
			journal.record.DocumentGroups[0].BindingIntents[0].Before = recoveryDocumentBindingPhase{Kind: "absent"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, decoded, comparison := preparedNativeFixture(t)
			comparisons := []DocumentComparison{comparison}
			test.edit(t, &fixture, &decoded, &comparisons)
			prepared, err := decoded.prepareDocuments(t.Context(), comparisons, fixture.read.limits, fixture.read.codecs)
			assertNoPreparedNativeDocuments(t, prepared, err)
		})
	}
}

func TestPreparedNativeDocumentsDiscardEarlierGroupOnLaterFailure(t *testing.T) {
	fixture, decoded, comparison := preparedNativeFixture(t)
	late := decoded.record.DocumentGroups[0]
	late.Aliases = append([]recoveryDocumentAlias(nil), late.Aliases...)
	late.Aliases[0].Address.Destination = "@pi-agent/settings.json"
	late.SelectedSubjects = []persistedSubjectRef{}
	late.BindingIntents = []recoveryDocumentBindingIntent{}
	late.ExclusiveClaims = recoveryDocumentClaimSet{Kind: "absent"}
	late.Original, late.Expected = recoveryDocumentImage{Kind: "absent"}, recoveryDocumentImage{Kind: "absent"}
	decoded.record.DocumentGroups = append(decoded.record.DocumentGroups, late)
	prepared, err := decoded.prepareDocuments(t.Context(), []DocumentComparison{comparison, {}}, fixture.read.limits, fixture.read.codecs)
	assertNoPreparedNativeDocuments(t, prepared, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	prepared, err = decoded.prepareDocuments(ctx, []DocumentComparison{comparison, {}}, fixture.read.limits, fixture.read.codecs)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	assertNoPreparedNativeDocuments(t, prepared, err)
	prepared, err = decoded.prepareDocuments(nil, []DocumentComparison{comparison, {}}, fixture.read.limits, fixture.read.codecs)
	assertNoPreparedNativeDocuments(t, prepared, err)
}

func TestNativeDocumentReaderKeepsMetadataRefusalBeforeImageReads(t *testing.T) {
	for _, kind := range []string{"selection", "claim"} {
		t.Run(kind, func(t *testing.T) {
			fixture, decoded, _ := preparedNativeFixture(t)
			if kind == "selection" {
				decoded.record.DocumentGroups[0].SelectedSubjects[0].Name += "-not-in-state"
			} else {
				decoded.record.DocumentGroups[0].ExclusiveClaims = recoveryDocumentClaimSet{Kind: "absent"}
			}
			reader := &preparedNativeReadCounter{RootedReader: fixture.read.filesystem}
			fixture.read.filesystem = reader
			loaded, err := decoded.loadDocuments(t.Context(), fixture.read)
			assertNoPreparedNativeDocuments(t, loaded, err)
			if reader.reads != 0 {
				t.Fatal("metadata refusal eagerly read private images")
			}
		})
	}
}

func preparedNativeFixture(t *testing.T) (nativeRecordFixture, nativeJournalSnapshots, DocumentComparison) {
	t.Helper()
	fixture := nativeJournalFixture(t)
	row := fixture.after.ManagedAggregates()[0]
	contract := row.Contribution().Contract()
	codec, found := fixture.read.codecs.Lookup(contract.CodecContractID())
	if !found {
		t.Fatal("fixture codec missing")
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	before := aggregate.ExistingDocument([]byte(`{"model":true,"outside":1}`))
	baseline, failure := codec.Read(before, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	item, err := aggregate.NewSubjectContribution(row.Subject(), row.Contribution())
	if err != nil {
		t.Fatal(err)
	}
	set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := aggregate.NewProjectionIntent(baseline.States()[0], &set)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := aggregate.NewPlan(baseline, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	edit, err := compose.NewProjectionEdit(codec, plan, configdocument.JSON, []output.KeyPath{contract.Address().KeyPath()})
	if err != nil {
		t.Fatal(err)
	}
	mode := recovery.NewPermissionMode(0o600)
	candidate, err := PrepareSelectedDocument(t.Context(), SelectedDocumentPreparationInput{Before: before, BeforeMode: mode, ExpectedMode: mode, Limits: fixture.read.limits}, []compose.ProjectionEdit{edit}, nil)
	if err != nil {
		t.Fatal(err)
	}
	nativeRecordImages(t, &fixture, before, candidate.Document())
	content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, decoded, candidate.Comparison()
}

func assertNoPreparedNativeDocuments(t *testing.T, value nativeDocumentRecords, err error) {
	t.Helper()
	if err == nil || value.groups != nil || value.images != nil || value.claims != nil || value.provisional != nil {
		t.Fatalf("invalid preparation exposed a complete or partial document result: %v", err)
	}
}

type preparedNativeReadCounter struct {
	mutationfs.RootedReader
	reads int
}

func (reader *preparedNativeReadCounter) ReadRootedRegularFileUpTo(ctx context.Context, capability rootedpath.CommitCapability, maximumBytes int64) ([]byte, fs.FileMode, mutationfs.EntryIdentity, error) {
	reader.reads++
	return reader.RootedReader.ReadRootedRegularFileUpTo(ctx, capability, maximumBytes)
}
