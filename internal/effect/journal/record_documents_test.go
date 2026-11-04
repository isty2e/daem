package journal

import (
	"context"
	"errors"
	"testing"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
)

func finishDocumentReconstruction(ctx context.Context, session *documentReconstructionSession, source documentReconstructionMode) (nativeDocumentRecords, error) {
	if source == documentReconstructionPrepared {
		prepared, err := session.finishPrepared(ctx)
		return prepared.documents, err
	}
	return session.finishLoaded(ctx)
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
			session, err := newDocumentReconstruction(t.Context(), journal, fixture.read.limits, fixture.read.codecs, mode, comparisons)
			if err != nil {
				t.Fatal(err)
			}

			switch fault {
			case "zero":
				session = &documentReconstructionSession{}
				_, _, err = session.next(t.Context())
			case "finish early":
				_, err = finishDocumentReconstruction(t.Context(), session, mode)
			case "supply early":
				err = session.supplyLoadedImages(t.Context(), documentImageDemand{}, loadedDocumentImages{})
			case "canceled":
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				_, _, err = session.next(ctx)
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("lost reconstruction cancellation: %v", err)
				}
			default:
				demand, present, nextErr := session.next(t.Context())
				if nextErr != nil || !present {
					t.Fatalf("missing first group demand: %v", nextErr)
				}
				if fault == "double request" {
					_, _, err = session.next(t.Context())
					break
				}
				if fault == "wrong prepared producer" {
					err = session.supplyPreparedImages(t.Context(), demand)
					break
				}

				images, imageErr := loadDocumentImages(t.Context(), demand.original, demand.expected, fixture.read.artifacts, fixture.read.filesystem, fixture.read.budget, recovery.MaximumRecoveryBackupFileBytes)
				if imageErr != nil {
					t.Fatal(imageErr)
				}
				switch fault {
				case "foreign demand":
					demand.session = &documentReconstructionSession{}
				case "skipped group":
					demand.index++
				case "failed images":
					images.original = loadedDocumentImage{}
				case "double supply":
					if err := session.supplyLoadedImages(t.Context(), demand, images); err != nil {
						t.Fatal(err)
					}
				}
				err = session.supplyLoadedImages(t.Context(), demand, images)
			}

			if err == nil {
				t.Fatal("invalid reconstruction protocol was accepted")
			}
			if result, err := finishDocumentReconstruction(t.Context(), session, mode); err == nil || len(result.groups)+len(result.images)+len(result.claims)+len(result.provisional) != 0 {
				t.Fatal("failed reconstruction returned a usable prefix")
			}
			if _, _, err := session.next(t.Context()); err == nil {
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
			session, err := newDocumentReconstruction(t.Context(), journal, fixture.read.limits, fixture.read.codecs, source.mode, comparisons)
			if err != nil {
				t.Fatal(err)
			}
			demand, present, err := session.next(t.Context())
			if err != nil || !present {
				t.Fatalf("first reconstruction demand: %v", err)
			}

			if source.mode == documentReconstructionPrepared {
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
			if _, present, err := session.next(t.Context()); err != nil || present {
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
