package publication

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeDocumentJournalStageUsesActualWholeProducer(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, action := range []string{"create", "replace", "record", "remove"} {
			t.Run(string(scope)+"/"+action, func(t *testing.T) {
				fixture := wholeGroupFixture(t, target.TargetPi, scope, action, []byte("original\x00\xff\r\n"), []byte("expected\x00\xff\n"))
				group, comparison, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
				if err != nil {
					t.Fatal(err)
				}
				journal := fixture.recordForGroups(t, []journalrecord.SealedDocumentGroup{group})
				requireStagedDocumentJournal(t, fixture.envelope, journal, []journaldocument.Comparison{comparison})
				fixture.requireUnchangedHost(t)
			})
		}
	}
}

func TestNativeDocumentJournalStageRejectsAllInvalidMetadataBeforeWrites(t *testing.T) {
	for _, change := range []string{"missing comparison", "wrong image", "wrong state", "invalid envelope", "ordinary entries", "overlapping later writer", "invalid later comparison", "missing codec", "pre-cancel"} {
		t.Run(change, func(t *testing.T) {
			whole := wholeGroupFixture(t, target.TargetPi, target.ScopeProject, "replace", []byte("original"), []byte("expected"))
			group, comparison, err := prepareWholeDocumentGroup(t.Context(), whole.namespace, whole.input)
			if err != nil {
				t.Fatal(err)
			}
			journal := whole.recordForGroups(t, []journalrecord.SealedDocumentGroup{group})
			options := documentStageOptions(whole.envelope, imageBudget(t))
			comparisons := []journaldocument.Comparison{comparison}
			ctx := t.Context()
			switch change {
			case "missing comparison":
				comparisons = nil
			case "wrong image":
				decoded, artifacts := publishNativeFixture(t, journal, comparison, options)
				variant := whole.envelope
				variant.journal, variant.read.Artifacts = decoded, artifacts
				journal = publishedNativeGroupVariant(t, variant, func(group map[string]json.RawMessage) {
					var original, expected map[string]json.RawMessage
					if err := json.Unmarshal(group["original"], &original); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(group["expected"], &expected); err != nil {
						t.Fatal(err)
					}
					var originalArtifact, expectedArtifact map[string]json.RawMessage
					if err := json.Unmarshal(original["artifact"], &originalArtifact); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(expected["artifact"], &expectedArtifact); err != nil {
						t.Fatal(err)
					}
					expectedArtifact["digest"] = originalArtifact["digest"]
					expected["artifact"] = nativeRecordJSON(t, expectedArtifact)
					group["expected"] = nativeRecordJSON(t, expected)
				})
				options.Budget = imageBudget(t)
			case "wrong state":
				invalid := whole
				invalid.envelope.after = invalid.envelope.before
				journal, err = invalid.tryRecordForGroups(t, []journalrecord.SealedDocumentGroup{group})
			case "invalid envelope":
				invalid := whole
				invalid.input.CaptureInput = reviseCaptureInput(invalid.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.OperationID = "../unsafe" })
				journal, err = invalid.tryRecordForGroups(t, []journalrecord.SealedDocumentGroup{group})
			case "ordinary entries":
				mixed := mixedStageFixture(t)
				source, _ := mixed.source(t, imageBudget(t))
				journal, err = mixed.journal.WithOrdinaryEvidence(source.record)
				if err != nil {
					t.Fatal(err)
				}
				comparisons = []journaldocument.Comparison{mixed.comparison}
				options = documentStageOptions(mixed.whole.envelope, imageBudget(t))
			case "overlapping later writer":
				journal, err = whole.tryRecordForGroups(t, []journalrecord.SealedDocumentGroup{group, group})
				comparisons = append(comparisons, comparison)
			case "invalid later comparison":
				first, _, groups, validComparisons := twoWholeGroupStageFixture(t)
				journal = first.recordForGroups(t, groups)
				options = documentStageOptions(first.envelope, imageBudget(t))
				prepared, err := journal.PrepareDocuments(ctx, validComparisons, options.Limits, options.Codecs)
				if err != nil {
					t.Fatal("non-overlapping groups failed before their negative control", err)
				}
				if _, err := prepared.Encode(ctx, options.StateCodec); err != nil {
					t.Fatal("valid multi-group preparation could not encode", err)
				}
				comparisons = append([]journaldocument.Comparison(nil), validComparisons...)
				comparisons[1] = journaldocument.Comparison{}
			case "missing codec":
				options.StateCodec = nil
			case "pre-cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err != nil {
				if change != "wrong state" && change != "invalid envelope" && change != "overlapping later writer" {
					t.Fatal(err)
				}
				if !reflect.ValueOf(journal).IsZero() {
					t.Fatal("constructor refusal returned partial native facts")
				}
				whole.requireUnchangedHost(t)
				return
			}
			writer := &imageStageWriter{}
			fingerprint, err := StageDocuments(journal, ctx, comparisons, writer, options)
			if err == nil || fingerprint != "" || writer.directories != 0 || writer.writes != 0 {
				t.Fatalf("invalid whole envelope caused private I/O or returned a fingerprint: %v", err)
			}
		})
	}
}

func TestNativeDocumentJournalStagePreservesMultipleActualGroups(t *testing.T) {
	first, second, groups, comparisons := twoWholeGroupStageFixture(t)
	journal := first.recordForGroups(t, groups)
	requireStagedDocumentJournal(t, first.envelope, journal, comparisons)
	first.requireUnchangedHost(t)
	second.requireUnchangedHost(t)
}

func TestNativeDocumentJournalStagePreflightsJournalAndAllImageWork(t *testing.T) {
	for _, shortage := range []string{"journal bytes", "journal entry", "image bytes"} {
		t.Run(shortage, func(t *testing.T) {
			fixture, decoded, comparison := preparedNativeFixture(t)
			budget := imageBudget(t)
			imageBytes := comparison.BeforeDocument().ContentSize() + comparison.ExpectedDocument().ContentSize()
			remainingBytes, remainingEntries := budget.RemainingBytes(), budget.RemainingEntries()
			consumeEntries, consumeBytes := 0, int64(0)
			switch shortage {
			case "journal bytes":
				consumeBytes = remainingBytes - imageBytes
			case "image bytes":
				consumeBytes = remainingBytes - imageBytes + 1
			case "journal entry":
				consumeEntries = remainingEntries - 3
			}
			work, err := workmeasure.NewArtifactWork(consumeEntries, consumeBytes)
			if err != nil {
				t.Fatal(err)
			}
			if err := budget.AdmitTree(work); err != nil {
				t.Fatal(err)
			}
			remainingBytes, remainingEntries = budget.RemainingBytes(), budget.RemainingEntries()
			writer := &imageStageWriter{}
			fingerprint, err := StageDocuments(decoded, t.Context(), []journaldocument.Comparison{comparison}, writer, documentStageOptions(fixture, budget))
			if err == nil || fingerprint != "" || writer.writes != 0 || writer.directories != 0 || budget.RemainingBytes() != remainingBytes || budget.RemainingEntries() != remainingEntries {
				t.Fatalf("budget refusal wrote or charged unattempted artifacts: %v", err)
			}
		})
	}
}

func TestNativeDocumentJournalStageLateFailureAbortsCompletePrivateTree(t *testing.T) {
	for _, test := range []struct {
		name   string
		at     int
		cancel bool
	}{
		{"second image", 2, false}, {"journal", 3, false}, {"journal canceled", 3, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, decoded, comparison := preparedNativeFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			failure := errors.New("injected private journal write failure")
			destination, capability := imageStageDestination(t)
			budget := imageBudget(t)
			remainingBytes, remainingEntries := budget.RemainingBytes(), budget.RemainingEntries()
			var fingerprint string
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
				fingerprint, err = StageDocuments(decoded, ctx, []journaldocument.Comparison{comparison}, wrapped, documentStageOptions(fixture, budget))
				return err
			})
			cause := failure
			if test.cancel {
				cause = context.Canceled
			}
			if !errors.Is(err, cause) || fingerprint != "" || remainingEntries-budget.RemainingEntries() != 1+test.at {
				t.Fatalf("failed complete stage exposed evidence or lost entry charges: %v", err)
			}
			imageBytes := comparison.BeforeDocument().ContentSize() + comparison.ExpectedDocument().ContentSize()
			if remainingBytes-budget.RemainingBytes() < imageBytes || (test.at == 3 && remainingBytes-budget.RemainingBytes() == imageBytes) {
				t.Fatal("failed journal write erased its conservative byte charge")
			}
			if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed staging published destination: %v", err)
			}
			entries, err := os.ReadDir(filepath.Dir(destination))
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed callback left partial private data: %v", err)
			}
		})
	}
}

func TestNativeDocumentJournalStageDiscardsFingerprintAfterFinalCheckCancellation(t *testing.T) {
	fixture, decoded, comparison := preparedNativeFixture(t)
	base, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := &journalCompletionCancellation{Context: base, cancel: cancel}
	destination, capability := imageStageDestination(t)
	var fingerprint string
	var stageErr error
	_, err := journalTestFilesystem().PrepareRootedTree(ctx, capability, func(writer mutationfs.RootedTreeWriter) error {
		wrapped := journalCompletionWriter{RootedTreeWriter: writer, context: ctx}
		fingerprint, stageErr = StageDocuments(decoded, ctx, []journaldocument.Comparison{comparison}, wrapped, documentStageOptions(fixture, imageBudget(t)))
		return stageErr
	})
	if !errors.Is(stageErr, context.Canceled) || fingerprint != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("terminal cancellation escaped staging: stage=%v outer=%v fingerprint=%q", stageErr, err, fingerprint)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("terminal cancellation published destination: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(destination))
	if err != nil || len(entries) != 0 {
		t.Fatalf("terminal cancellation retained partial private data: %v", err)
	}
}

type journalCompletionCancellation struct {
	context.Context
	cancel context.CancelFunc
	armed  atomic.Bool
}

func (ctx *journalCompletionCancellation) Err() error {
	err := ctx.Context.Err()
	if ctx.armed.Swap(false) {
		ctx.cancel()
	}
	return err
}

type journalCompletionWriter struct {
	mutationfs.RootedTreeWriter
	context *journalCompletionCancellation
}

func (writer journalCompletionWriter) WriteFile(path mutationfs.TreeRelativePath, mode fs.FileMode, content io.Reader) error {
	err := writer.RootedTreeWriter.WriteFile(path, mode, content)
	if err == nil && path.Path() == journalrecord.FileName {
		writer.context.armed.Store(true)
	}
	return err
}
