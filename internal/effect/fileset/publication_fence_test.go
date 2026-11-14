package fileset

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestPublicationFenceRetainsLegacyMarkerSupport(t *testing.T) {
	root := t.TempDir()
	evidenceRoot := mustCanonicalStateDir(t, filepath.Join(root, "metadata"))
	target := mustWriteTarget(t, filepath.Join(root, "state.json"), "after")
	if _, err := prepareMarker(t.Context(), evidenceRoot, []FileTarget{target}); err != nil {
		t.Fatal(err)
	}
	requirePublicationFenceWithinReservation(t, evidenceRoot)
	assertMissing(t, target.Path())
}

func TestPublicationFenceObservesOnlyHeaderWithinLegacyReservation(t *testing.T) {
	root := t.TempDir()
	evidenceRoot := mustCanonicalStateDir(t, filepath.Join(root, "metadata"))
	absent := AbsentPublicationImage()
	after := publicationImageForTest(t, []byte("after"))
	var targets []PublicationTarget
	for index := range 9 {
		targets = append(targets, publicationTargetForTest(t, filepath.Join(root, fmt.Sprintf("state-%d.json", index)), absent, after))
	}
	intent, err := NewPublicationIntent([]byte("original selection"), targets)
	if err != nil {
		t.Fatal(err)
	}
	body, err := encodePublicationIntent(intent)
	if err != nil {
		t.Fatal(err)
	}
	header, err := encodePublicationMarker(body)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, markerPath(evidenceRoot), string(header), transactionEvidenceMode)
	assertMissing(t, filepath.Join(transactionDir(evidenceRoot), publicationIntentFile))
	requirePublicationFenceWithinReservation(t, evidenceRoot)
	if err := ObserveClearFenceAt(t.Context(), evidenceRoot, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{}); !errors.Is(err, ErrFileSetEvidenceInvalid) {
		t.Fatal("static generation-3 observer accepted containing-intent marker", err)
	}
	for _, target := range targets {
		assertMissing(t, target.Path())
	}

	if err := os.Remove(markerPath(evidenceRoot)); err != nil {
		t.Fatal(err)
	}
	err = ObserveClearPublicationFenceAt(t.Context(), evidenceRoot, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{})
	if !errors.Is(err, ErrFileSetEvidenceInvalid) || errors.Is(err, ErrInterruptedFileSetTransaction) {
		t.Fatal("marker-missing evidence was not classified as incomplete", err)
	}
}

func TestPublicationFenceRejectsOtherMarkerGenerations(t *testing.T) {
	for _, version := range []int{contractversion.MetadataTransactionReference, contractversion.MetadataTransactionIntent + 1} {
		t.Run(fmt.Sprintf("generation-%d", version), func(t *testing.T) {
			evidenceRoot := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), "metadata"))
			if version == contractversion.MetadataTransactionReference {
				target := mustWriteTarget(t, filepath.Join(filepath.Dir(evidenceRoot), "state.json"), "after")
				if _, err := prepareMarkerVersion(t.Context(), evidenceRoot, []FileTarget{target}, version, false); err != nil {
					t.Fatal(err)
				}
				if err := ObserveClearFenceAt4(t.Context(), evidenceRoot, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{}); !errors.Is(err, ErrInterruptedFileSetTransaction) {
					t.Fatal("legacy reference fixture was not a valid generation-4 marker", err)
				}
			} else {
				size, digest := int64(1), hashBytes([]byte("body"))
				content, err := json.Marshal(publicationMarkerRecord{
					Version: version, Intent: &publicationIntentDescriptorRecord{Size: &size, Hash: &digest},
				})
				if err != nil {
					t.Fatal(err)
				}
				writeFixture(t, markerPath(evidenceRoot), string(content), transactionEvidenceMode)
			}
			err := ObserveClearPublicationFenceAt(t.Context(), evidenceRoot, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{})
			if !errors.Is(err, ErrFileSetEvidenceInvalid) || errors.Is(err, ErrInterruptedFileSetTransaction) {
				t.Fatal("closed publication observer accepted another marker generation", err)
			}
		})
	}
}

func requirePublicationFenceWithinReservation(t *testing.T, evidenceRoot string) {
	t.Helper()
	work, err := MaximumFenceCensusWork(evidenceRoot, mutationfs.MaximumPhysicalPathDepth)
	if err != nil {
		t.Fatal(err)
	}
	budget := &limitedPhysicalWorkBudget{maximum: work}
	err = ObserveClearPublicationFenceAt(t.Context(), evidenceRoot, mutationfs.MaximumPhysicalPathDepth, budget)
	if !errors.Is(err, ErrInterruptedFileSetTransaction) || FileSetFenceKindOf(err) != FileSetFencePublishedTransaction {
		t.Fatal("published marker exceeded its existing reservation or lost classification", err)
	}
	if budget.paths > work.PathComponents || budget.entries > work.Entries || budget.bytes > work.Bytes {
		t.Fatal("publication marker observation exceeded ordinary census work")
	}
}
