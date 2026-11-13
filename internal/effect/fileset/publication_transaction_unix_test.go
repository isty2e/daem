//go:build darwin || linux

package fileset

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestPublication5PublishesNineLazyNamespaceBoundImages(t *testing.T) {
	input, calls := publicationInputForTest(t)
	if err := CommitFreshFileSet5(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	for _, target := range input.Intent.Targets() {
		image, _, _, err := readPublicationTarget(t.Context(), target.path, maximumTargetBytes)
		if err != nil || !image.Equal(target.after) || calls[target.path] != 1 {
			t.Fatal("whole publication lost an After image or namespace binding", target.path, image, err, calls)
		}
	}
	assertMissing(t, transactionDir(input.EvidenceRoot))
	if err := ObserveClearPublicationFenceAt(t.Context(), input.EvidenceRoot, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{}); err != nil {
		t.Fatal("completed publication retained a fence", err)
	}
}

func TestPublication5RestoresNineTargetsAfterLazyFailureOrCancellation(t *testing.T) {
	for _, failure := range []string{"second materializer", "third output", "third cancellation"} {
		t.Run(failure, func(t *testing.T) {
			input, _ := publicationInputForTest(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			stopAt := 1
			if failure != "second materializer" {
				stopAt = 2
			}
			path := input.Intent.targets[stopAt].path
			primary := errors.New("injected lazy failure")
			input.Materializers[path] = func(context.Context, []byte) ([]byte, error) {
				if failure == "third output" {
					return []byte("unselected output"), nil
				}
				if failure == "third cancellation" {
					cancel()
					return nil, ctx.Err()
				}
				return nil, primary
			}
			err := CommitFreshFileSet5(ctx, input)
			if err == nil || failure == "second materializer" && !errors.Is(err, primary) ||
				failure == "third cancellation" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost forward failure", err)
			}
			assertPublicationBeforeForTest(t, input.Intent)
			assertMissing(t, transactionDir(input.EvidenceRoot))
		})
	}
}

func TestPublication5RejectsWholeWorkBeforeCreatingEvidence(t *testing.T) {
	input, calls := publicationInputForTest(t)
	denied := errors.New("publication work denied")
	budget := &publicationReadAdmissionBudget{onWork: func(_ int, bytes int64) error {
		if bytes > maximumMarkerBytes {
			return denied
		}
		return nil
	}}
	input.PhysicalWorkBudget = budget
	if err := CommitFreshFileSet5(t.Context(), input); !errors.Is(err, denied) {
		t.Fatal("publication skipped complete pre-effect admission", err)
	}
	if len(calls) != 0 {
		t.Fatal("materialized before work admission", calls)
	}
	assertMissing(t, filepath.Dir(input.EvidenceRoot))
	assertPublicationBeforeForTest(t, input.Intent)
}

func TestPublication5NoopHasNoRootOrMaterializerFootprint(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	absent := AbsentPublicationImage()
	intent, err := NewPublicationIntent([]byte("qualified selection"), []PublicationTarget{
		publicationTargetForTest(t, filepath.Join(root, "retained"), absent, absent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitFreshFileSet5(t.Context(), PublicationInput{EvidenceRoot: root, Intent: intent}); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, root)
}

func TestPublication5RejectsAggregateAfterWorkWithoutRetainingPayloads(t *testing.T) {
	root := mustCanonicalStateDir(t, t.TempDir())
	count := int(maximumFileSetOperationBytes/maximumTargetBytes) + 1
	largeImage, err := publicationImageFromFacts(true, maximumTargetBytes, hashBytes([]byte("bounded compiler facts")), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	smallImage := publicationImageForTest(t, []byte("x"))
	var large, small []PublicationTarget
	materializers := make(map[string]PublicationMaterializer)
	called := false
	for index := range count {
		path := filepath.Join(root, fmt.Sprintf("state-%04d", index))
		large = append(large, publicationTargetForTest(t, path, AbsentPublicationImage(), largeImage))
		small = append(small, publicationTargetForTest(t, path, AbsentPublicationImage(), smallImage))
		materializers[path] = func(context.Context, []byte) ([]byte, error) {
			called = true
			return nil, errors.New("unexpected materialization")
		}
	}
	evidenceRoot := filepath.Join(root, "data", "metadata")
	smallIntent, err := NewPublicationIntent([]byte("selection"), small)
	if err != nil {
		t.Fatal(err)
	}
	body, header, err := encodePublicationEvidence(smallIntent)
	if err != nil {
		t.Fatal(err)
	}
	smallBudget := &fenceObservationBudget{}
	plan, err := bindPublicationPlan(t.Context(), evidenceRoot, smallIntent, int64(len(header)), int64(len(body)), smallBudget)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.admitWork(smallBudget, publicationForwardWork); err != nil {
		t.Fatal("target cardinality alone prevented admission", err)
	}
	if err := plan.rootWitness.Close(); err != nil {
		t.Fatal(err)
	}
	intent, err := NewPublicationIntent([]byte("selection"), large)
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitFreshFileSet5(t.Context(), PublicationInput{
		EvidenceRoot: evidenceRoot, Intent: intent,
		Materializers: materializers, PhysicalWorkBudget: &fenceObservationBudget{},
	}); err == nil || called {
		t.Fatal("aggregate output work was not refused before materialization", called, err)
	}
	assertMissing(t, filepath.Dir(evidenceRoot))
}

func TestPublication5RestartRestoresMixtureAndFinalizesCompleteAfter(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprint(complete), func(t *testing.T) {
			input, revision := interruptedPublicationForTest(t, complete)
			if err := ObserveClearPublicationFenceAt(t.Context(), input.EvidenceRoot, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{}); !errors.Is(err, ErrInterruptedFileSetTransaction) {
				t.Fatal("coarse observer did not recognize the actual producer's marker", err)
			}
			before := publicationDiskImagesForTest(t, input)
			disposition, err := InspectFileSetRecovery5(t.Context(), input.EvidenceRoot, input.Intent, revision, &fenceObservationBudget{})
			want := RecoveryRollback
			if complete {
				want = RecoveryFinalize
			}
			if err != nil || disposition != want || !maps.Equal(before, publicationDiskImagesForTest(t, input)) {
				t.Fatal("read-only recovery preview changed files or lost disposition", disposition, err)
			}
			selected := input.Intent.targets[2]
			view, err := ReadPublicationBeforeEvidence5(t.Context(), input.EvidenceRoot, selected.path, revision,
				mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{})
			if err != nil || !view.Image().Equal(selected.before) || hashBytes(view.Content()) != selected.before.hash {
				t.Fatal("real publisher and selected Before reader disagree on canonical backup position", err)
			}
			if err := RecoverFileSet5(t.Context(), input.EvidenceRoot, input.Intent, revision, &fenceObservationBudget{}); err != nil {
				t.Fatal(err)
			}
			if !complete {
				assertPublicationBeforeForTest(t, input.Intent)
			} else {
				for _, target := range input.Intent.targets {
					image, _, _, err := readPublicationTarget(t.Context(), target.path, maximumTargetBytes)
					if err != nil || !image.Equal(target.after) {
						t.Fatal("finalization rewrote a completed After image", image, err)
					}
				}
			}
			assertMissing(t, transactionDir(input.EvidenceRoot))
		})
	}
}

func TestPublication5RecoveryRejectsChangedSelectionRevisionOrBackupWithoutEffects(t *testing.T) {
	input, revision := interruptedPublicationForTest(t, true)
	before := publicationDiskImagesForTest(t, input)
	changed, err := NewPublicationIntent([]byte("different selection"), input.Intent.Targets())
	if err != nil {
		t.Fatal(err)
	}
	subset, err := NewPublicationIntent(input.Intent.Selection(), input.Intent.Targets()[:8])
	if err != nil {
		t.Fatal(err)
	}
	for _, selection := range []struct {
		intent   PublicationIntent
		revision string
	}{{changed, revision}, {subset, revision}, {input.Intent, hashBytes([]byte("different marker"))}} {
		if err := RecoverFileSet5(t.Context(), input.EvidenceRoot, selection.intent, selection.revision, &fenceObservationBudget{}); !errors.Is(err, ErrRecoveryIntentMismatch) {
			t.Fatal("recovery accepted a substituted operation", err)
		}
		if !maps.Equal(before, publicationDiskImagesForTest(t, input)) {
			t.Fatal("selection refusal changed transaction files")
		}
	}
	backup := filepath.Join(transactionDir(input.EvidenceRoot), publicationBackupName(2))
	writeFixture(t, backup, string(bytes.Repeat([]byte("x"), int(input.Intent.targets[2].before.size))), transactionEvidenceMode)
	before = publicationDiskImagesForTest(t, input)
	if err := RecoverFileSet5(t.Context(), input.EvidenceRoot, input.Intent, revision, &fenceObservationBudget{}); err == nil {
		t.Fatal("all-After recovery silently retired a damaged backup")
	}
	if !maps.Equal(before, publicationDiskImagesForTest(t, input)) {
		t.Fatal("damaged-evidence refusal had effects")
	}
}

func TestPublication5RejectsInvalidMaterializerSelectionBeforeEffects(t *testing.T) {
	for _, invalid := range []string{"missing", "nil", "unselected", "retained"} {
		t.Run(invalid, func(t *testing.T) {
			input, calls := publicationInputForTest(t)
			switch invalid {
			case "missing":
				delete(input.Materializers, input.Intent.targets[0].path)
			case "nil":
				input.Materializers[input.Intent.targets[0].path] = nil
			case "unselected":
				input.Materializers[filepath.Join(filepath.Dir(input.EvidenceRoot), "unselected")] = func(context.Context, []byte) ([]byte, error) {
					t.Fatal("unselected materializer was called")
					return nil, nil
				}
			case "retained":
				targets := input.Intent.Targets()
				targets[2] = publicationTargetForTest(t, targets[2].path, targets[2].before, targets[2].before)
				intent, err := NewPublicationIntent(input.Intent.Selection(), targets)
				if err != nil {
					t.Fatal(err)
				}
				input.Intent = intent
			}
			if err := CommitFreshFileSet5(t.Context(), input); err == nil || len(calls) != 0 {
				t.Fatal("invalid materializer selection reached effects", invalid, calls, err)
			}
			assertPublicationBeforeForTest(t, input.Intent)
			assertMissing(t, filepath.Dir(input.EvidenceRoot))
		})
	}
}

func TestPublication5RecoveryWorkRefusalPreservesEvidenceAndTargets(t *testing.T) {
	input, revision := interruptedPublicationForTest(t, false)
	before := publicationDiskImagesForTest(t, input)
	denied := errors.New("recovery work denied")
	budget := &publicationReadAdmissionBudget{onWork: func(_ int, bytes int64) error {
		if bytes > maximumMarkerBytes {
			return denied
		}
		return nil
	}}
	if err := RecoverFileSet5(t.Context(), input.EvidenceRoot, input.Intent, revision, budget); !errors.Is(err, denied) {
		t.Fatal("recovery bypassed complete pre-effect admission", err)
	}
	if !maps.Equal(before, publicationDiskImagesForTest(t, input)) {
		t.Fatal("work refusal changed a target or transaction evidence")
	}
}

func TestPublication5OwningRecoveryPreservesInactiveResidueWithoutBlocking(t *testing.T) {
	input, revision := interruptedPublicationForTest(t, true)
	residue := filepath.Join(input.EvidenceRoot, ".daem-tmp-unowned")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectFileSetRecovery5(t.Context(), input.EvidenceRoot, input.Intent, revision, &fenceObservationBudget{}); err != nil {
		t.Fatal("inactive residue blocked recovery preview", err)
	}
	if err := RecoverFileSet5(t.Context(), input.EvidenceRoot, input.Intent, revision, &fenceObservationBudget{}); err != nil {
		t.Fatal("inactive residue blocked owning recovery", err)
	}
	assertMissing(t, transactionDir(input.EvidenceRoot))
	if info, err := os.Stat(residue); err != nil || !info.IsDir() {
		t.Fatal("recovery deleted anonymous evidence by its name", err)
	}
}

func TestPublication5ThirdStateRefusesSettlementAndRecovery(t *testing.T) {
	input, _ := publicationInputForTest(t)
	first := input.Intent.targets[0].path
	input.Materializers[input.Intent.targets[1].path] = func(context.Context, []byte) ([]byte, error) {
		if err := os.WriteFile(first, []byte("external third state"), 0o600); err != nil {
			return nil, err
		}
		return nil, errors.New("injected failure after external edit")
	}
	if err := CommitFreshFileSet5(t.Context(), input); err == nil {
		t.Fatal("third state was reported as successful publication")
	}
	view, err := ReadPublicationIntentEvidence5(t.Context(), input.EvidenceRoot, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{})
	if err != nil {
		t.Fatal(err)
	}
	before := publicationDiskImagesForTest(t, input)
	if _, err := InspectFileSetRecovery5(t.Context(), input.EvidenceRoot, input.Intent, view.MarkerRevision(), &fenceObservationBudget{}); err == nil {
		t.Fatal("third state was reported as recoverable")
	}
	if err := RecoverFileSet5(t.Context(), input.EvidenceRoot, input.Intent, view.MarkerRevision(), &fenceObservationBudget{}); err == nil {
		t.Fatal("third state was overwritten by recovery")
	}
	if !maps.Equal(before, publicationDiskImagesForTest(t, input)) {
		t.Fatal("third-state refusal changed files or retired evidence")
	}
}

func TestPublication5WholeTreeRetirementPrecedesOptionalCleanup(t *testing.T) {
	input, revision := interruptedPublicationForTest(t, true)
	unselected := filepath.Join(transactionDir(input.EvidenceRoot), "unselected")
	writeFixture(t, unselected, "", transactionEvidenceMode)
	if err := RecoverFileSet5(t.Context(), input.EvidenceRoot, input.Intent, revision, &fenceObservationBudget{}); err != nil {
		t.Fatal("optional cleanup invalidated terminal recovery", err)
	}
	assertMissing(t, transactionDir(input.EvidenceRoot))
	retired, err := filepath.Glob(filepath.Join(input.EvidenceRoot, publicationRetiredPrefix+"*"))
	if err != nil || len(retired) != 1 {
		t.Fatal("whole evidence tree was not retired", retired, err)
	}
	for _, name := range []string{"unselected", transactionMarkerFile, publicationIntentFile} {
		if _, err := os.Stat(filepath.Join(retired[0], name)); err != nil {
			t.Fatal("retirement lost evidence or removed an unknown file", name, err)
		}
	}
	if err := ObserveClearPublicationFenceAt(t.Context(), input.EvidenceRoot, mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{}); err != nil {
		t.Fatal("retired data blocked normal admission", err)
	}
}

func TestPublication5RechecksTerminalTargetsBeforeRetiringEvidence(t *testing.T) {
	input, revision := interruptedPublicationForTest(t, true)
	plan, err := loadQualifiedPublication(t.Context(), input.EvidenceRoot, input.Intent, revision, &fenceObservationBudget{})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.rootWitness.Close()
	if err := plan.admitWork(&fenceObservationBudget{}, publicationRecoveryWork); err != nil {
		t.Fatal(err)
	}
	disposition, err := plan.inspect(t.Context(), revision)
	if err != nil || disposition != RecoveryFinalize {
		t.Fatal("fixture did not reach all-After finalization", disposition, err)
	}
	writeFixture(t, input.Intent.targets[0].path, "changed after classification", 0o600)
	before := publicationDiskImagesForTest(t, input)
	if err := plan.finish(t.Context(), disposition, revision); err == nil {
		t.Fatal("finalization discarded evidence for a changed terminal target")
	}
	if !maps.Equal(before, publicationDiskImagesForTest(t, input)) {
		t.Fatal("failed terminal confirmation changed targets or retired evidence")
	}
}

func TestPublicationTerminalConfirmationDistinguishesAbsentAndEmpty(t *testing.T) {
	root := mustCanonicalStateDir(t, t.TempDir())
	absentPath := filepath.Join(root, "missing", "state.json")
	if err := confirmPublicationTarget(t.Context(), absentPath, AbsentPublicationImage()); err != nil {
		t.Fatal("rooted absence could not be confirmed", err)
	}
	assertMissing(t, filepath.Dir(absentPath))
	emptyPath := filepath.Join(root, "empty.json")
	writeFixture(t, emptyPath, "", 0o600)
	empty := publicationImageForTest(t, nil)
	if err := confirmPublicationTarget(t.Context(), emptyPath, empty); err != nil {
		t.Fatal("present empty image could not be confirmed", err)
	}
	if err := confirmPublicationTarget(t.Context(), emptyPath, AbsentPublicationImage()); err == nil {
		t.Fatal("present empty target was treated as absent")
	}
	if err := confirmPublicationTarget(t.Context(), absentPath, empty); err == nil {
		t.Fatal("absent target was treated as a present empty file")
	}
}

func publicationInputForTest(t *testing.T) (PublicationInput, map[string]int) {
	t.Helper()
	root := mustCanonicalStateDir(t, t.TempDir())
	input := PublicationInput{
		EvidenceRoot:  filepath.Join(root, "data", "metadata"),
		Materializers: make(map[string]PublicationMaterializer), PhysicalWorkBudget: &fenceObservationBudget{},
	}
	calls := make(map[string]int)
	var targets []PublicationTarget
	for index := range 9 {
		path := filepath.Join(root, fmt.Sprintf("state-%02d.json", index))
		beforeBytes := []byte(fmt.Sprintf("before-%d", index))
		before := AbsentPublicationImage()
		if index != 0 {
			if index == 1 {
				beforeBytes = nil
			}
			writeFixture(t, path, string(beforeBytes), 0o600)
			before = publicationImageForTest(t, beforeBytes)
		} else {
			beforeBytes = nil
		}
		afterBytes := []byte(fmt.Sprintf("after-%d", index))
		if index == 0 {
			afterBytes = nil
		}
		targets = append(targets, publicationTargetForTest(t, path, before, publicationImageForTest(t, afterBytes)))
		input.Materializers[path] = func(_ context.Context, content []byte) ([]byte, error) {
			calls[path]++
			if !bytes.Equal(content, beforeBytes) {
				return nil, fmt.Errorf("materializer received another namespace's Before bytes")
			}
			return slices.Clone(afterBytes), nil
		}
	}
	slices.Reverse(targets)
	intent, err := NewPublicationIntent([]byte("qualified original selection"), targets)
	if err != nil {
		t.Fatal(err)
	}
	input.Intent = intent
	return input, calls
}

func interruptedPublicationForTest(t *testing.T, allAfter bool) (PublicationInput, string) {
	t.Helper()
	input, _ := publicationInputForTest(t)
	body, header, err := encodePublicationEvidence(input.Intent)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := bindPublicationPlan(t.Context(), input.EvidenceRoot, input.Intent, int64(len(header)), int64(len(body)), input.PhysicalWorkBudget)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.rootWitness.Close()
	if err := plan.admitWork(input.PhysicalWorkBudget, publicationForwardWork); err != nil {
		t.Fatal(err)
	}
	if err := plan.publishEvidence(t.Context(), body, header); err != nil {
		t.Fatal(err)
	}
	for index, target := range input.Intent.targets {
		if !allAfter && index >= 2 {
			break
		}
		if err := plan.publishTarget(t.Context(), index, input.Materializers[target.path]); err != nil {
			t.Fatal(err)
		}
	}
	return input, hashBytes(header)
}

func assertPublicationBeforeForTest(t *testing.T, intent PublicationIntent) {
	t.Helper()
	for _, target := range intent.targets {
		image, _, _, err := readPublicationTarget(t.Context(), target.path, maximumTargetBytes)
		if err != nil || !image.Equal(target.before) {
			t.Fatal("publication did not restore exact Before image", target.path, image, err)
		}
	}
}

func publicationDiskImagesForTest(t *testing.T, input PublicationInput) map[string]PublicationImage {
	t.Helper()
	var paths []string
	for _, target := range input.Intent.targets {
		paths = append(paths, target.path)
	}
	entries, err := os.ReadDir(transactionDir(input.EvidenceRoot))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		paths = append(paths, filepath.Join(transactionDir(input.EvidenceRoot), entry.Name()))
	}
	images := make(map[string]PublicationImage)
	for _, path := range paths {
		image, _, _, err := readPublicationTarget(t.Context(), path, maximumTargetBytes)
		if err != nil {
			t.Fatal(err)
		}
		images[path] = image
	}
	return images
}
