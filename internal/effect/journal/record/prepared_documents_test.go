package record

import (
	"bytes"
	"strconv"
	"testing"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
)

func TestPreparedDocumentRecordEncodesOnlyCompletedImageWitnesses(t *testing.T) {
	for _, version := range []int{native16Version, native17Version} {
		t.Run(strconv.Itoa(version), func(t *testing.T) { testPreparedDocumentImageWitnesses(t, version) })
	}
}

func testPreparedDocumentImageWitnesses(t *testing.T, version int) {
	fixture, journal, comparison := preparedNativeFixture(t)
	if version == native17Version {
		wire := native17RecordFixture(t, fixture)
		var err error
		journal, err = wire.reconstruct(t.Context(), fixture.codec)
		if err != nil {
			t.Fatal(err)
		}
	}
	before := nativeRecordJSON(t, journal.record)
	prepared, err := journal.PrepareDocuments(t.Context(), []journaldocument.Comparison{comparison}, fixture.limits, fixture.codecs)
	if err != nil {
		t.Fatal(err)
	}
	comparisons, err := prepared.Comparisons()
	if err != nil || len(comparisons) != 1 {
		t.Fatal("prepared record lost its completed comparison", err)
	}
	comparisons[0] = journaldocument.Comparison{}
	comparisons, err = prepared.Comparisons()
	if err != nil || comparisons[0].Validate() != nil {
		t.Fatal("comparison projection changed its source", err)
	}
	content, err := prepared.Encode(t.Context(), fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	_, original, expected, err := documentImageReferences(prepared.documents.groups[0].Comparison(), 0)
	if err != nil {
		t.Fatal(err)
	}
	actual := decoded.record.DocumentGroups[0]
	if original.Artifact.Path == journal.record.DocumentGroups[0].Original.Artifact.Path || expected.Artifact.Path == journal.record.DocumentGroups[0].Expected.Artifact.Path {
		t.Fatal("fixture does not distinguish captured and prepared image references")
	}
	if !bytes.Equal(nativeRecordJSON(t, original), nativeRecordJSON(t, actual.Original)) || !bytes.Equal(nativeRecordJSON(t, expected), nativeRecordJSON(t, actual.Expected)) {
		t.Fatal("encoded image witnesses differ from complete reconstruction")
	}
	if !bytes.Equal(before, nativeRecordJSON(t, journal.record)) {
		t.Fatal("prepared completion changed its source record")
	}
}

func TestPreparedDocumentRecordZeroHasNoStagingComparisons(t *testing.T) {
	if comparisons, err := (PreparedDocuments{}).Comparisons(); err == nil || comparisons != nil {
		t.Fatal("zero prepared record supplied staging comparisons")
	}
}

func TestDocumentReconstructionRefusesAnotherCompletionSource(t *testing.T) {
	for _, mode := range []documentReconstructionMode{documentReconstructionPrepared, documentReconstructionLoaded} {
		fixture, journal, comparison := preparedNativeFixture(t)
		var comparisons []journaldocument.Comparison
		if mode == documentReconstructionPrepared {
			comparisons = []journaldocument.Comparison{comparison}
		}
		session, err := newDocumentReconstruction(t.Context(), journal, fixture.limits, fixture.codecs, mode, comparisons)
		if err != nil {
			t.Fatal(err)
		}
		demand, present, err := session.Next(t.Context())
		if err != nil || !present {
			t.Fatalf("no actual reconstruction demand: %v", err)
		}
		if mode == documentReconstructionPrepared {
			err = session.supplyPreparedImages(t.Context(), demand)
		} else {
			images := reconstructionFixtureImages(t, demand, comparison)
			err = session.SupplyLoaded(t.Context(), demand, images.Original(), images.Expected())
		}
		if err != nil {
			t.Fatal(err)
		}

		if mode == documentReconstructionPrepared {
			if result, err := session.FinishLoaded(t.Context()); err == nil || result.groups != nil {
				t.Fatal("prepared evidence completed through the loaded producer")
			}
		} else {
			result, err := session.finishPrepared(t.Context())
			if err == nil || result.documents.groups != nil {
				t.Fatal("loaded evidence completed through the prepared producer")
			}
			if content, err := result.Encode(t.Context(), fixture.codec); err == nil || content != nil {
				t.Fatal("wrong-source completion produced encodable witnesses")
			}
		}
		if result, err := finishDocumentReconstruction(t.Context(), session, mode); err == nil || result.groups != nil {
			t.Fatal("wrong-source completion left a reusable session")
		}
	}
}
