package journal

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestDocumentImageStagePublishesExactPrivateImages(t *testing.T) {
	for _, test := range []struct {
		name                     string
		before, expected         aggregate.Document
		beforeMode, expectedMode *recovery.PermissionMode
	}{
		{"create empty", aggregate.AbsentDocument(), aggregate.ExistingDocument(nil), nil, recovery.NewPermissionMode(0o600)},
		{"remove empty", aggregate.ExistingDocument(nil), aggregate.AbsentDocument(), recovery.NewPermissionMode(0), nil},
		{"opaque", aggregate.ExistingDocument([]byte("before\x00\xff\r\n")), aggregate.ExistingDocument([]byte("after\xff\n")), recovery.NewPermissionMode(0o744), recovery.NewPermissionMode(0o600)},
		{"mode only", aggregate.ExistingDocument([]byte("same")), aggregate.ExistingDocument([]byte("same")), recovery.NewPermissionMode(0o644), recovery.NewPermissionMode(0o400)},
		{"absent pair", aggregate.AbsentDocument(), aggregate.AbsentDocument(), nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			comparison := imageStageComparison(t, journaldocument.ComparisonInput{Before: test.before, Expected: test.expected, BeforeMode: test.beforeMode, ExpectedMode: test.expectedMode})
			destination, capability := imageStageDestination(t)
			budget := imageBudget(t)
			remainingBytes, remainingEntries := budget.RemainingBytes(), budget.RemainingEntries()
			var original, expected journalrecord.DocumentImage
			prepared, err := journalTestFilesystem().PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
				var err error
				original, expected, err = stageDocumentImages(t.Context(), comparison, 0, writer, budget)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Abort(context.Background())
			if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("image staging published before commit: %v", err)
			}
			if err := prepared.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			root, err := rootedpath.CaptureRoot(destination)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			loadedOriginal, loadedExpected, err := loadDocumentImages(t.Context(), original, expected, root, journalTestFilesystem(), imageBudget(t), 4096)
			if err != nil || !loadedOriginal.Document().Equal(test.before) || !loadedExpected.Document().Equal(test.expected) {
				t.Fatalf("staged pair lost exact content or presence: %v", err)
			}
			fileCount := 0
			for index, record := range []journalrecord.DocumentImage{original, expected} {
				wantMode := []*recovery.PermissionMode{test.beforeMode, test.expectedMode}[index]
				loadedMode := []*recovery.PermissionMode{loadedOriginal.Mode(), loadedExpected.Mode()}[index]
				if (loadedMode == nil) != (wantMode == nil) || (loadedMode != nil && *loadedMode != *wantMode) {
					t.Fatal("host permission metadata was replaced by private witness mode")
				}
				plan, err := record.AdmitRead(4096)
				if err != nil {
					t.Fatal(err)
				}
				if !plan.Exists() {
					continue
				}
				fileCount++
				path := filepath.Join(destination, filepath.FromSlash(plan.Reference()))
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("witness file is not private0600: %v", err)
				}
				info, err = os.Stat(filepath.Dir(path))
				if err != nil || info.Mode().Perm() != 0o700 {
					t.Fatalf("witness directory is not private0700: %v", err)
				}
			}
			entries := fileCount
			if entries != 0 {
				entries++
			}
			if remainingBytes-budget.RemainingBytes() != test.before.ContentSize()+test.expected.ContentSize() || remainingEntries-budget.RemainingEntries() != entries {
				t.Fatal("staging did not charge the shared image work exactly once")
			}
		})
	}
}

func TestDocumentImageStageFeedsStrictNativeRecordReconstruction(t *testing.T) {
	fixture := nativeJournalFixture(t)
	loaded, err := loadDocuments(fixture.journal, t.Context(), fixture.read)
	if err != nil {
		t.Fatal(err)
	}
	comparison := requireLoadedNativeDocument(t, loaded, 0).Group().Comparison()
	decoded, root := publishNativeFixture(t, fixture.journal, comparison, nativeDocumentStageOptions{stateCodec: fixture.codec, codecs: fixture.read.codecs, limits: fixture.read.limits, budget: imageBudget(t)})
	fixture.read.artifacts, fixture.read.budget = root, imageBudget(t)
	reconstructed, err := loadDocuments(decoded, t.Context(), fixture.read)
	if err != nil || reconstructed.Len() != 1 {
		t.Fatalf("actual staged native16 images cannot reconstruct: %v", err)
	}
	if !requireLoadedNativeDocument(t, reconstructed, 0).Images().Original().Document().Equal(requireLoadedNativeDocument(t, loaded, 0).Images().Original().Document()) || !requireLoadedNativeDocument(t, reconstructed, 0).Images().Expected().Document().Equal(requireLoadedNativeDocument(t, loaded, 0).Images().Expected().Document()) {
		t.Fatal("native record reconstruction lost staged comparison witnesses")
	}
}

func TestDocumentImageStageFailureAndCancellationAbortActualPartialTree(t *testing.T) {
	for _, test := range []struct {
		name   string
		at     int
		cancel bool
	}{
		{"first write failure", 1, false}, {"second write failure", 2, false}, {"first write cancellation", 1, true}, {"second write cancellation", 2, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			comparison := imageStageComparison(t, journaldocument.ComparisonInput{Before: aggregate.ExistingDocument([]byte("before")), Expected: aggregate.ExistingDocument([]byte("expected")), BeforeMode: recovery.NewPermissionMode(0o640), ExpectedMode: recovery.NewPermissionMode(0o600)})
			destination, capability := imageStageDestination(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			failure := errors.New("injected witness write failure")
			budget := imageBudget(t)
			remainingBytes, remainingEntries := budget.RemainingBytes(), budget.RemainingEntries()
			var original, expected journalrecord.DocumentImage
			_, err := journalTestFilesystem().PrepareRootedTree(ctx, capability, func(writer mutationfs.RootedTreeWriter) error {
				wrapped := &imageStageWriter{RootedTreeWriter: writer, afterWrite: func(count int) error {
					if count != test.at {
						return nil
					}
					if test.cancel {
						cancel()
						return nil
					}
					return failure
				}}
				var err error
				original, expected, err = stageDocumentImages(ctx, comparison, 0, wrapped, budget)
				return err
			})
			cause := failure
			if test.cancel {
				cause = context.Canceled
			}
			if !errors.Is(err, cause) || original != (journalrecord.DocumentImage{}) || expected != (journalrecord.DocumentImage{}) {
				t.Fatalf("failed staging exposed a partial usable pair or stage: %v", err)
			}
			wantBytes := int64(len("before"))
			if test.at == 2 {
				wantBytes += int64(len("expected"))
			}
			if remainingBytes-budget.RemainingBytes() != wantBytes || remainingEntries-budget.RemainingEntries() != 1+test.at {
				t.Fatal("failed/canceled write erased attempted work charges")
			}
			if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed staging exposed destination: %v", err)
			}
			entries, err := os.ReadDir(filepath.Dir(destination))
			if err != nil || len(entries) != 0 {
				t.Fatalf("containing tree did not clean partial private artifacts: %v", err)
			}
		})
	}
}

func TestDocumentImageStagePreflightsWholePairAgainstSharedWork(t *testing.T) {
	comparison := imageStageComparison(t, journaldocument.ComparisonInput{Before: aggregate.ExistingDocument([]byte("before")), Expected: aggregate.ExistingDocument([]byte("expected")), BeforeMode: recovery.NewPermissionMode(0o600), ExpectedMode: recovery.NewPermissionMode(0o600)})
	for _, exhaustion := range []string{"bytes", "entries"} {
		t.Run(exhaustion, func(t *testing.T) {
			budget := imageBudget(t)
			entries, contentBytes := 0, int64(0)
			if exhaustion == "bytes" {
				contentBytes = budget.RemainingBytes() - int64(len("before")+len("expected")) + 1
			} else {
				entries = budget.RemainingEntries() - 2
			}
			work, err := workmeasure.NewArtifactWork(entries, contentBytes)
			if err != nil {
				t.Fatal(err)
			}
			if err := budget.AdmitTree(work); err != nil {
				t.Fatal(err)
			}
			writer := &imageStageWriter{}
			original, expected, err := stageDocumentImages(t.Context(), comparison, 0, writer, budget)
			if err == nil || original != (journalrecord.DocumentImage{}) || expected != (journalrecord.DocumentImage{}) || writer.directories != 0 || writer.writes != 0 {
				t.Fatal("insufficient pair budget reached writer or exposed an image")
			}
		})
	}
}

func TestDocumentImageStageDoesNotOverwriteAnotherGroup(t *testing.T) {
	comparison := imageStageComparison(t, journaldocument.ComparisonInput{Before: aggregate.AbsentDocument(), Expected: aggregate.ExistingDocument([]byte("expected")), ExpectedMode: recovery.NewPermissionMode(0o600)})
	destination, capability := imageStageDestination(t)
	budget := imageBudget(t)
	var original, expected journalrecord.DocumentImage
	_, err := journalTestFilesystem().PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		if _, _, err := stageDocumentImages(t.Context(), comparison, 0, writer, budget); err != nil {
			return err
		}
		var err error
		original, expected, err = stageDocumentImages(t.Context(), comparison, 0, writer, budget)
		return err
	})
	if err == nil || original != (journalrecord.DocumentImage{}) || expected != (journalrecord.DocumentImage{}) {
		t.Fatal("reused ordinal overwrote an image or returned a pair")
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("collision published partial groups")
	}
	entries, err := os.ReadDir(filepath.Dir(destination))
	if err != nil || len(entries) != 0 {
		t.Fatalf("collision left unpublished private artifacts: %v", err)
	}
}

func TestDocumentImageStageKeepsDistinctGroupsOnOneBudget(t *testing.T) {
	comparisons := []journaldocument.Comparison{
		imageStageComparison(t, journaldocument.ComparisonInput{Before: aggregate.AbsentDocument(), Expected: aggregate.ExistingDocument([]byte("first")), ExpectedMode: recovery.NewPermissionMode(0o600)}),
		imageStageComparison(t, journaldocument.ComparisonInput{Before: aggregate.ExistingDocument([]byte("second")), Expected: aggregate.AbsentDocument(), BeforeMode: recovery.NewPermissionMode(0o640)}),
	}
	destination, capability := imageStageDestination(t)
	budget := imageBudget(t)
	remainingBytes, remainingEntries := budget.RemainingBytes(), budget.RemainingEntries()
	var records [2][2]journalrecord.DocumentImage
	prepared, err := journalTestFilesystem().PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		for index, comparison := range comparisons {
			var err error
			records[index][0], records[index][1], err = stageDocumentImages(t.Context(), comparison, index, writer, budget)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort(context.Background())
	if err := prepared.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	root, err := rootedpath.CaptureRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	readBudget := imageBudget(t)
	for index, comparison := range comparisons {
		loadedOriginal, loadedExpected, err := loadDocumentImages(t.Context(), records[index][0], records[index][1], root, journalTestFilesystem(), readBudget, 4096)
		if err != nil || !loadedOriginal.Document().Equal(comparison.BeforeDocument()) || !loadedExpected.Document().Equal(comparison.ExpectedDocument()) {
			t.Fatalf("group images were confused: %v", err)
		}
	}
	if remainingBytes-budget.RemainingBytes() != int64(len("first")+len("second")) || remainingEntries-budget.RemainingEntries() != 4 {
		t.Fatal("multiple groups reset or lost operation work")
	}
}

func TestDocumentImageStageRejectsMissingLifecycleInputs(t *testing.T) {
	comparison := imageStageComparison(t, journaldocument.ComparisonInput{Before: aggregate.AbsentDocument(), Expected: aggregate.AbsentDocument()})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, test := range []struct {
		name                         string
		ctx                          context.Context
		comparison                   journaldocument.Comparison
		index                        int
		missingWriter, missingBudget bool
	}{
		{"context", nil, comparison, 0, false, false},
		{"canceled", ctx, comparison, 0, false, false},
		{"comparison", t.Context(), journaldocument.Comparison{}, 0, false, false},
		{"ordinal", t.Context(), comparison, -1, false, false},
		{"writer", t.Context(), comparison, 0, true, false},
		{"budget", t.Context(), comparison, 0, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer := &imageStageWriter{}
			var port mutationfs.RootedTreeWriter = writer
			budget := imageBudget(t)
			if test.missingWriter {
				port = nil
			}
			if test.missingBudget {
				budget = nil
			}
			original, expected, err := stageDocumentImages(test.ctx, test.comparison, test.index, port, budget)
			if err == nil || original != (journalrecord.DocumentImage{}) || expected != (journalrecord.DocumentImage{}) || writer.directories != 0 || writer.writes != 0 {
				t.Fatal("missing lifecycle input produced usable records or storage work")
			}
			if test.name == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("staging lost cancellation classification")
			}
		})
	}
}

func imageStageComparison(t *testing.T, input journaldocument.ComparisonInput) journaldocument.Comparison {
	t.Helper()
	input.Limits = configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	comparison, err := journaldocument.NewWholeComparison(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	return comparison
}

func imageStageDestination(t *testing.T) (string, rootedpath.CommitCapability) {
	t.Helper()
	destination := filepath.Join(t.TempDir(), "private-journal")
	root, path, err := rootedpath.CaptureDestination(destination)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	capability, err := root.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	return destination, capability
}

type imageStageWriter struct {
	mutationfs.RootedTreeWriter
	directories, writes int
	afterWrite          func(int) error
}

func (writer *imageStageWriter) CreateDirectory(path mutationfs.TreeRelativePath, mode fs.FileMode) error {
	writer.directories++
	if writer.RootedTreeWriter == nil {
		return errors.New("unexpected directory write")
	}
	return writer.RootedTreeWriter.CreateDirectory(path, mode)
}

func (writer *imageStageWriter) WriteFile(path mutationfs.TreeRelativePath, mode fs.FileMode, content io.Reader) error {
	writer.writes++
	if writer.RootedTreeWriter == nil {
		return errors.New("unexpected file write")
	}
	err := writer.RootedTreeWriter.WriteFile(path, mode, content)
	if writer.afterWrite != nil {
		err = errors.Join(err, writer.afterWrite(writer.writes))
	}
	return err
}
