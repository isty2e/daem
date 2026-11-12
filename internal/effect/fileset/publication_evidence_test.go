package fileset

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestPublicationEvidenceReconstructsBoundImmutableIntent(t *testing.T) {
	root, intent, _, header := publicationEvidenceFixture(t)
	view, err := ReadPublicationIntentEvidence5(t.Context(), root, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{})
	if err != nil {
		t.Fatal(err)
	}
	if view.MarkerRevision() != hashBytes(header) || !bytes.Equal(view.Intent().Selection(), intent.Selection()) ||
		!slices.Equal(view.Intent().Targets(), intent.Targets()) {
		t.Fatal("evidence lost its raw marker revision or containing intent")
	}
	selection := view.Intent().Selection()
	selection[0] ^= 1
	targets := view.Intent().Targets()
	targets[0] = PublicationTarget{}
	if !bytes.Equal(view.Intent().Selection(), intent.Selection()) || !slices.Equal(view.Intent().Targets(), intent.Targets()) {
		t.Fatal("evidence view exposed mutable owned facts")
	}
	for _, target := range intent.Targets() {
		assertMissing(t, target.Path())
	}
	var reformatted bytes.Buffer
	if err := json.Indent(&reformatted, header, "", "  "); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, markerPath(root), reformatted.String(), transactionEvidenceMode)
	updated, err := ReadPublicationIntentEvidence5(t.Context(), root, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{})
	if err != nil || updated.MarkerRevision() != hashBytes(reformatted.Bytes()) || updated.MarkerRevision() == view.MarkerRevision() {
		t.Fatal("marker revision was computed from reserialization instead of original bytes", err)
	}
}

func TestPublicationEvidenceRefusesSameSizeBodySubstitution(t *testing.T) {
	root, _, body, _ := publicationEvidenceFixture(t)
	var substituted publicationIntentRecord
	if err := json.Unmarshal(body, &substituted); err != nil {
		t.Fatal(err)
	}
	substituted.Selection[0] ^= 1
	changed, err := json.Marshal(substituted)
	if err != nil || len(changed) != len(body) {
		t.Fatal("substituted fixture changed size", err)
	}
	writeFixture(t, filepath.Join(transactionDir(root), publicationIntentFile), string(changed), transactionEvidenceMode)
	if err := ObserveClearPublicationFenceAt(t.Context(), root, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{}); !errors.Is(err, ErrInterruptedFileSetTransaction) {
		t.Fatal("coarse observer unexpectedly consumed body integrity", err)
	}
	view, err := ReadPublicationIntentEvidence5(t.Context(), root, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{})
	if !errors.Is(err, ErrFileSetEvidenceInvalid) || view.MarkerRevision() != "" || view.Intent().Validate() == nil {
		t.Fatal("owning inspection accepted an unbound body or returned partial evidence", err)
	}
}

func TestPublicationEvidenceRefusesIncompleteBudgetOrCanceledInspection(t *testing.T) {
	root, _, _, _ := publicationEvidenceFixture(t)
	if err := os.Remove(filepath.Join(transactionDir(root), publicationIntentFile)); err != nil {
		t.Fatal(err)
	}
	view, err := ReadPublicationIntentEvidence5(t.Context(), root, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{})
	if !errors.Is(err, ErrFileSetEvidenceInvalid) || view.MarkerRevision() != "" {
		t.Fatal("missing containing intent became usable evidence", err)
	}
	budget := &limitedPhysicalWorkBudget{}
	if view, err := ReadPublicationIntentEvidence5(t.Context(), root, mutationfs.MaximumPhysicalPathDepth, budget); err == nil || view.MarkerRevision() != "" {
		t.Fatal("inspection bypassed shared work admission", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ReadPublicationIntentEvidence5(ctx, root, mutationfs.MaximumPhysicalPathDepth, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation was displaced by inspection prerequisites", err)
	}
	absentRoot := filepath.Join(t.TempDir(), "not-created")
	if _, err := ReadPublicationIntentEvidence5(t.Context(), absentRoot, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{}); err == nil {
		t.Fatal("absent evidence was accepted")
	}
	assertMissing(t, absentRoot)
	if body, header, err := encodePublicationEvidence(PublicationIntent{}); err == nil || body != nil || header != nil {
		t.Fatal("invalid intent returned a partial encoded pair", err)
	}
}

func TestPublicationEvidenceAdmitsMarkerAndBodyBeforeTheirReads(t *testing.T) {
	for _, phase := range []string{"marker", "body entry", "body payload"} {
		t.Run(phase, func(t *testing.T) {
			root, _, body, _ := publicationEvidenceFixture(t)
			budget := &publicationReadAdmissionBudget{}
			mutated := false
			budget.onWork = func(entries int, payloadBytes int64) error {
				matches := phase == "marker" && payloadBytes == maximumMarkerBytes ||
					phase == "body entry" && entries == 1 && budget.entries == 2 ||
					phase == "body payload" && payloadBytes == int64(len(body))
				if mutated || !matches {
					return nil
				}
				mutated = true
				path := markerPath(root)
				if phase != "marker" {
					path = filepath.Join(transactionDir(root), publicationIntentFile)
				}
				return os.Remove(path)
			}
			view, err := ReadPublicationIntentEvidence5(t.Context(), root, mutationfs.MaximumPhysicalPathDepth, budget)
			if !mutated || !errors.Is(err, os.ErrNotExist) || view.Intent().Validate() == nil {
				t.Fatal("evidence read skipped or preceded its own work admission", mutated, err)
			}
		})
	}
}

type publicationReadAdmissionBudget struct {
	fenceObservationBudget
	onWork func(entries int, payloadBytes int64) error
}

func (budget *publicationReadAdmissionBudget) AdmitPathComponents(count int) error {
	return budget.AdmitPhysicalWork(count, 0, 0)
}

func (budget *publicationReadAdmissionBudget) AdmitPhysicalWork(paths, entries int, payloadBytes int64) error {
	if err := budget.fenceObservationBudget.AdmitPhysicalWork(paths, entries, payloadBytes); err != nil {
		return err
	}
	return budget.onWork(entries, payloadBytes)
}

func publicationEvidenceFixture(t *testing.T) (string, PublicationIntent, []byte, []byte) {
	t.Helper()
	root := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), "metadata"))
	absent := AbsentPublicationImage()
	intent, err := NewPublicationIntent([]byte("original qualified selection"), []PublicationTarget{
		publicationTargetForTest(t, filepath.Join(filepath.Dir(root), "unopened-state.json"), absent, publicationImageForTest(t, []byte("after"))),
	})
	if err != nil {
		t.Fatal(err)
	}
	body, header, err := encodePublicationEvidence(intent)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, markerPath(root), string(header), transactionEvidenceMode)
	writeFixture(t, filepath.Join(transactionDir(root), publicationIntentFile), string(body), transactionEvidenceMode)
	return root, intent, body, header
}
