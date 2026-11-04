package record

import (
	"context"
	"errors"
	"testing"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func reconstructionFixtureImage(t *testing.T, reference DocumentImage, document aggregate.Document) LoadedImage {
	t.Helper()
	image, err := loadRecordFixtureImage(reference, document)
	if err != nil {
		t.Fatal(err)
	}
	return image
}

func reconstructionFixtureImages(t *testing.T, demand ImageDemand, comparison journaldocument.Comparison) LoadedImages {
	t.Helper()
	images, err := loadedDocumentImagePair(reconstructionFixtureImage(t, demand.original, comparison.BeforeDocument()), reconstructionFixtureImage(t, demand.expected, comparison.ExpectedDocument()))
	if err != nil {
		t.Fatal(err)
	}
	return images
}

func TestNativeCreateRecordLoadsExplicitAbsenceAndItsRemovalEvidence(t *testing.T) {
	fixture, journal, comparison := preparedNativeFixtureFor(t, nativeCreateFixture(t), aggregate.AbsentDocument())
	if len(journal.Before().ManagedAggregates()) != 0 || len(journal.After().ManagedAggregates()) != 1 {
		t.Fatal("create fixture did not bind the before/after membership")
	}
	recorded, err := journal.DocumentAt(0)
	if err != nil {
		t.Fatal(err)
	}
	removal, present, err := recorded.Removal()
	if err != nil || !present || removal.Demands().Len() == 0 {
		t.Fatal("decoded create record lost its removal authority", err)
	}
	session, err := journal.BeginLoadedDocuments(t.Context(), fixture.limits, fixture.codecs)
	if err != nil {
		t.Fatal(err)
	}
	demand, present, err := session.Next(t.Context())
	if err != nil || !present || demand.Original().Exists() || !demand.Expected().Exists() {
		t.Fatal("create record has the wrong image demand", err)
	}
	original, err := demand.Original().Absent()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := demand.Expected().Load(comparison.ExpectedDocument().Content(), Mode)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SupplyLoaded(t.Context(), demand, original, expected); err != nil {
		t.Fatal(err)
	}
	if _, present, err := session.Next(t.Context()); err != nil || present {
		t.Fatal("create record has unfinished groups", err)
	}
	loaded, err := session.FinishLoaded(t.Context())
	if err != nil || loaded.Len() != 1 {
		t.Fatal("create record failed reconstruction", err)
	}
	value, present := loaded.At(0)
	if !present || value.Images().Original().Document().Exists() || value.Images().Original().Mode() != nil || !value.Images().Expected().Document().Equal(comparison.ExpectedDocument()) || len(value.Claims().Before()) != 0 || len(value.Claims().After()) != 1 {
		t.Fatal("create reconstruction lost explicit absence, expected bytes or claim membership")
	}
}

func TestLoadedDocumentReconstructionRejectsUnboundImages(t *testing.T) {
	for _, fault := range []string{"zero pair", "foreign original", "foreign expected"} {
		t.Run(fault, func(t *testing.T) {
			fixture, journal, comparison := preparedNativeFixture(t)
			session, err := newDocumentReconstruction(t.Context(), journal, fixture.limits, fixture.codecs, documentReconstructionLoaded, nil)
			if err != nil {
				t.Fatal(err)
			}
			demand, present, err := session.Next(t.Context())
			if err != nil || !present {
				t.Fatal("missing loaded image demand", err)
			}
			var images LoadedImages
			if fault != "zero pair" {
				original, expected := demand.original, demand.expected
				if !original.Exists() || !expected.Exists() {
					t.Fatal("foreign-witness fixture requires two files")
				}
				if fault == "foreign original" {
					original.reference += ".other"
				} else {
					expected.reference += ".other"
				}
				images, err = loadedDocumentImagePair(reconstructionFixtureImage(t, original, comparison.BeforeDocument()), reconstructionFixtureImage(t, expected, comparison.ExpectedDocument()))
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := session.SupplyLoaded(t.Context(), demand, images.Original(), images.Expected()); err == nil {
				t.Error("loaded reconstruction accepted unbound image evidence")
			}
			if session.phase != documentReconstructionFailed {
				t.Error("failed image input did not poison reconstruction")
			}
			if result, err := session.FinishLoaded(t.Context()); err == nil || len(result.groups)+len(result.images)+len(result.claims)+len(result.provisional) != 0 {
				t.Error("unbound images produced a reconstruction prefix")
			}
		})
	}
}

func TestLoadedDocumentsViewsPreserveIndexedFactsAndOwnership(t *testing.T) {
	if (LoadedDocuments{}).Len() != 0 {
		t.Fatal("zero loaded result has documents")
	}
	if _, present := (LoadedDocuments{}).At(0); present {
		t.Fatal("zero loaded result has a document view")
	}
	fixture, journal, comparison := preparedNativeFixture(t)
	session, err := journal.BeginLoadedDocuments(t.Context(), fixture.limits, fixture.codecs)
	if err != nil {
		t.Fatal(err)
	}
	demand, present, err := session.Next(t.Context())
	if err != nil || !present {
		t.Fatal("missing image demand", err)
	}
	images := reconstructionFixtureImages(t, demand, comparison)
	if err := session.SupplyLoaded(t.Context(), demand, images.Original(), images.Expected()); err != nil {
		t.Fatal(err)
	}
	records, err := session.FinishLoaded(t.Context())
	if err != nil || records.Len() != 1 {
		t.Fatal("missing complete loaded record", err)
	}
	for _, index := range []int{-1, 1} {
		if _, present := records.At(index); present {
			t.Fatal("out-of-range loaded document view")
		}
	}
	recorded, err := journal.DocumentAt(0)
	if err != nil {
		t.Fatal(err)
	}
	aliases, err := recorded.Addresses(t.Context())
	if err != nil || len(aliases) == 0 {
		t.Fatal("missing canonical alias", err)
	}
	records.provisional[0] = []journaldocument.ProvisionalAcquire{journaldocument.NewProvisionalAcquire(aliases[0], ownership.ProvisionalAcquireIntent{})}
	view, present := records.At(0)
	if !present || !view.Group().Comparison().ExpectedDocument().Equal(comparison.ExpectedDocument()) || !view.Images().Original().Document().Equal(comparison.BeforeDocument()) {
		t.Fatal("indexed view changed complete document facts")
	}
	provisional := view.Provisional()
	provisional[0] = journaldocument.ProvisionalAcquire{}
	if view.Provisional()[0].Alias() != aliases[0] {
		t.Fatal("provisional view changed its source")
	}
}

func finishDocumentReconstruction(ctx context.Context, session *LoadedReconstruction, source documentReconstructionMode) (LoadedDocuments, error) {
	if source == documentReconstructionPrepared {
		prepared, err := session.finishPrepared(ctx)
		return prepared.documents, err
	}
	return session.FinishLoaded(ctx)
}

func TestDocumentReconstructionRejectsIncompleteOrForeignImageSupply(t *testing.T) {
	for _, fault := range []string{"zero", "finish early", "supply early", "double request", "foreign demand", "skipped group", "double supply", "failed images", "wrong prepared producer", "wrong loaded producer", "canceled"} {
		t.Run(fault, func(t *testing.T) {
			fixture, journal, comparison := preparedNativeFixture(t)
			mode := documentReconstructionLoaded
			var comparisons []journaldocument.Comparison
			if fault == "wrong loaded producer" {
				mode, comparisons = documentReconstructionPrepared, []journaldocument.Comparison{comparison}
			}
			session, err := newDocumentReconstruction(t.Context(), journal, fixture.limits, fixture.codecs, mode, comparisons)
			if err != nil {
				t.Fatal(err)
			}

			switch fault {
			case "zero":
				session = &LoadedReconstruction{}
				_, _, err = session.Next(t.Context())
			case "finish early":
				_, err = finishDocumentReconstruction(t.Context(), session, mode)
			case "supply early":
				err = session.SupplyLoaded(t.Context(), ImageDemand{}, LoadedImage{}, LoadedImage{})
			case "canceled":
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				_, _, err = session.Next(ctx)
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("lost reconstruction cancellation: %v", err)
				}
			default:
				demand, present, nextErr := session.Next(t.Context())
				if nextErr != nil || !present {
					t.Fatalf("missing first group demand: %v", nextErr)
				}
				if fault == "double request" {
					_, _, err = session.Next(t.Context())
					break
				}
				if fault == "wrong prepared producer" {
					err = session.supplyPreparedImages(t.Context(), demand)
					break
				}

				images := reconstructionFixtureImages(t, demand, comparison)
				switch fault {
				case "foreign demand":
					demand.session = &LoadedReconstruction{}
				case "skipped group":
					demand.index++
				case "failed images":
					images.original = LoadedImage{}
				case "double supply":
					if err := session.SupplyLoaded(t.Context(), demand, images.Original(), images.Expected()); err != nil {
						t.Fatal(err)
					}
				}
				err = session.SupplyLoaded(t.Context(), demand, images.Original(), images.Expected())
			}

			if err == nil {
				t.Fatal("invalid reconstruction protocol was accepted")
			}
			if result, err := finishDocumentReconstruction(t.Context(), session, mode); err == nil || len(result.groups)+len(result.images)+len(result.claims)+len(result.provisional) != 0 {
				t.Fatal("failed reconstruction returned a usable prefix")
			}
			if _, _, err := session.Next(t.Context()); err == nil {
				t.Fatal("failed reconstruction requested more image work")
			}
		})
	}
}

func TestDocumentReconstructionCompletesBothImageSourcesWithoutReplay(t *testing.T) {
	for _, source := range []struct {
		name string
		mode documentReconstructionMode
	}{{"prepared", documentReconstructionPrepared}, {"loaded", documentReconstructionLoaded}} {
		t.Run(source.name, func(t *testing.T) {
			fixture, journal, comparison := preparedNativeFixture(t)
			comparisons := []journaldocument.Comparison{comparison}
			if source.mode == documentReconstructionLoaded {
				comparisons = nil
			}
			session, err := newDocumentReconstruction(t.Context(), journal, fixture.limits, fixture.codecs, source.mode, comparisons)
			if err != nil {
				t.Fatal(err)
			}
			demand, present, err := session.Next(t.Context())
			if err != nil || !present {
				t.Fatalf("first reconstruction demand: %v", err)
			}

			if source.mode == documentReconstructionPrepared {
				err = session.supplyPreparedImages(t.Context(), demand)
			} else {
				images := reconstructionFixtureImages(t, demand, comparison)
				err = session.SupplyLoaded(t.Context(), demand, images.Original(), images.Expected())
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, present, err := session.Next(t.Context()); err != nil || present {
				t.Fatalf("completed reconstruction requested extra work: %v", err)
			}

			result, err := finishDocumentReconstruction(t.Context(), session, source.mode)
			if err != nil || len(result.groups) != 1 || len(result.images) != 1 || len(result.claims) != 1 {
				t.Fatalf("complete reconstruction: %v", err)
			}
			actual := result.groups[0].Comparison()
			if !actual.BeforeDocument().Equal(comparison.BeforeDocument()) || !actual.ExpectedDocument().Equal(comparison.ExpectedDocument()) {
				t.Fatal("reconstruction changed the captured image pair")
			}
			if err := actual.RequireCanonicalRecoveryPolicy(t.Context(), comparison); err != nil {
				t.Fatal("reconstruction changed canonical recovery policy", err)
			}
			if value, err := finishDocumentReconstruction(t.Context(), session, source.mode); err == nil || len(value.groups)+len(value.images) != 0 {
				t.Fatal("reconstruction completion was replayed")
			}
		})
	}
}
