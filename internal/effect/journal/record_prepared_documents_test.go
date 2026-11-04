package journal

import (
	"bytes"
	"testing"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
)

func TestPreparedDocumentRecordEncodesOnlyCompletedImageWitnesses(t *testing.T) {
	fixture, journal, comparison := preparedNativeFixture(t)
	before := nativeRecordJSON(t, journal.record)
	prepared, err := journal.prepareDocuments(t.Context(), []journaldocument.Comparison{comparison}, fixture.read.limits, fixture.read.codecs)
	if err != nil {
		t.Fatal(err)
	}
	content, err := prepared.encode(t.Context(), fixture.codec)
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

func TestDocumentReconstructionRefusesAnotherCompletionSource(t *testing.T) {
	for _, mode := range []documentReconstructionMode{documentReconstructionPrepared, documentReconstructionLoaded} {
		fixture, journal, comparison := preparedNativeFixture(t)
		var comparisons []journaldocument.Comparison
		if mode == documentReconstructionPrepared {
			comparisons = []journaldocument.Comparison{comparison}
		}
		session, err := newDocumentReconstruction(t.Context(), journal, fixture.read.limits, fixture.read.codecs, mode, comparisons)
		if err != nil {
			t.Fatal(err)
		}
		demand, present, err := session.next(t.Context())
		if err != nil || !present {
			t.Fatalf("no actual reconstruction demand: %v", err)
		}
		if mode == documentReconstructionPrepared {
			err = session.supplyPreparedImages(t.Context(), demand)
		} else {
			var images loadedDocumentImages
			images, err = loadDocumentImages(t.Context(), demand.original, demand.expected, fixture.read.artifacts, fixture.read.filesystem, fixture.read.budget, recovery.MaximumRecoveryBackupFileBytes)
			if err == nil {
				err = session.supplyLoadedImages(t.Context(), demand, images)
			}
		}
		if err != nil {
			t.Fatal(err)
		}

		if mode == documentReconstructionPrepared {
			if result, err := session.finishLoaded(t.Context()); err == nil || result.groups != nil {
				t.Fatal("prepared evidence completed through the loaded producer")
			}
		} else {
			result, err := session.finishPrepared(t.Context())
			if err == nil || result.documents.groups != nil {
				t.Fatal("loaded evidence completed through the prepared producer")
			}
			if content, err := result.encode(t.Context(), fixture.codec); err == nil || content != nil {
				t.Fatal("wrong-source completion produced encodable witnesses")
			}
		}
		if result, err := finishDocumentReconstruction(t.Context(), session, mode); err == nil || result.groups != nil {
			t.Fatal("wrong-source completion left a reusable session")
		}
	}
}
