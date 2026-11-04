package journal

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/durable"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
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
				fixture.envelope.record.DocumentGroups = []recoveryDocumentGroup{group}
				authority, err := fixture.namespace.roots.Manifest.Root.Authority()
				if err != nil {
					t.Fatal(err)
				}
				provenance, err := authority.Provenance()
				if err != nil {
					t.Fatal(err)
				}
				fixture.envelope.record.ManifestRootProvenance = persistedNativeRoot(provenance)
				content, err := marshalNativeJournalRecord(t.Context(), fixture.envelope.record, fixture.envelope.before, fixture.envelope.after, fixture.envelope.codec)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.envelope.codec)
				if err != nil {
					t.Fatal(err)
				}
				requireStagedDocumentJournal(t, fixture.envelope, decoded, []journaldocument.Comparison{comparison})
				fixture.requireUnchangedHost(t)
			})
		}
	}
}

func TestNativeDocumentJournalStagePreservesMultipleActualGroups(t *testing.T) {
	first := wholeGroupFixture(t, target.TargetPi, target.ScopeProject, "replace", []byte("first before"), []byte("first after"))
	second := wholeGroupFixture(t, target.TargetCodex, target.ScopeProject, "replace", []byte("second before"), []byte("second after"))
	second.path = filepath.Join(first.namespace.roots.Manifest.SelectedPath, filepath.FromSlash(second.input.mutation.facts().destination.RelativePath()))
	requireWholeGroupWrite(t, second.path, second.original.Content(), 0o644)
	roots := second.namespace.roots
	roots.Manifest = first.namespace.roots.Manifest
	var err error
	second.namespace, err = ObserveNativeDocumentNamespace(t.Context(), second.namespace.Addresses(), roots, second.reader, second.input.budget)
	if err != nil {
		t.Fatal(err)
	}
	second.input.owner = first.input.owner
	exact, known := second.namespace.observation.Exact()
	if !known {
		t.Fatal("existing second file requires exact authority")
	}
	binding, err := ownership.NewDocumentBinding(first.input.owner, second.input.mutation.facts().destination, exact)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ownership.PresentDocumentBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := ownership.NewExactDocumentBindingIntent(first.input.owner, binding.Destination(), value, value)
	if err != nil {
		t.Fatal(err)
	}
	second.input.bindings = []ownership.DocumentBindingIntent{intent}
	first.input.groupIndex, second.input.groupIndex = 0, 1
	var groups []recoveryDocumentGroup
	var comparisons []journaldocument.Comparison
	for _, fixture := range []wholeGroupTestFixture{first, second} {
		group, comparison, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
		if err != nil {
			t.Fatal(err)
		}
		groups, comparisons = append(groups, group), append(comparisons, comparison)
	}
	envelope := first.envelope
	envelope.before, err = durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: append(first.envelope.before.ManagedPaths(), second.envelope.before.ManagedPaths()...)})
	if err != nil {
		t.Fatal(err)
	}
	envelope.after, err = durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: append(first.envelope.after.ManagedPaths(), second.envelope.after.ManagedPaths()...)})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := roots.Manifest.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	envelope.record.ManifestRootProvenance, envelope.record.DocumentGroups = persistedNativeRoot(provenance), groups
	content, err := marshalNativeJournalRecord(t.Context(), envelope.record, envelope.before, envelope.after, envelope.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, envelope.codec)
	if err != nil {
		t.Fatal(err)
	}
	requireStagedDocumentJournal(t, envelope, decoded, comparisons)
	first.requireUnchangedHost(t)
	second.requireUnchangedHost(t)
}

func TestNativeDocumentJournalStageRejectsAllInvalidMetadataBeforeWrites(t *testing.T) {
	for _, change := range []string{"missing comparison", "wrong image", "wrong state", "invalid envelope", "ordinary entries", "later group", "missing codec", "pre-cancel"} {
		t.Run(change, func(t *testing.T) {
			fixture, decoded, comparison := preparedNativeFixture(t)
			options := documentStageOptions(fixture, imageBudget(t))
			comparisons := []journaldocument.Comparison{comparison}
			ctx := t.Context()
			switch change {
			case "missing comparison":
				comparisons = nil
			case "wrong image":
				decoded.record.DocumentGroups[0].Expected.Artifact.Digest = decoded.record.DocumentGroups[0].Original.Artifact.Digest
			case "wrong state":
				decoded.after = decoded.before
			case "invalid envelope":
				decoded.record.OperationID = "../unsafe"
			case "ordinary entries":
				decoded.record.Entries = []recoveryEntry{{}}
			case "later group":
				late := decoded.record.DocumentGroups[0]
				late.Aliases = slices.Clone(late.Aliases)
				late.Aliases[0].Address.Destination = "@pi-agent/other.json"
				late.BindingIntents, late.SelectedSubjects = []recoveryDocumentBindingIntent{}, []persistedSubjectRef{}
				late.ExclusiveClaims = recoveryDocumentClaimSet{Kind: "absent"}
				decoded.record.DocumentGroups = append(decoded.record.DocumentGroups, late)
				comparisons = append(comparisons, journaldocument.Comparison{})
			case "missing codec":
				options.stateCodec = nil
			case "pre-cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			writer := &imageStageWriter{}
			fingerprint, err := decoded.stageDocumentJournal(ctx, comparisons, writer, options)
			if err == nil || fingerprint != "" || writer.directories != 0 || writer.writes != 0 {
				t.Fatalf("invalid whole envelope caused private I/O or returned a fingerprint: %v", err)
			}
		})
	}
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
			fingerprint, err := decoded.stageDocumentJournal(t.Context(), []journaldocument.Comparison{comparison}, writer, documentStageOptions(fixture, budget))
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
				fingerprint, err = decoded.stageDocumentJournal(ctx, []journaldocument.Comparison{comparison}, wrapped, documentStageOptions(fixture, budget))
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
		fingerprint, stageErr = decoded.stageDocumentJournal(ctx, []journaldocument.Comparison{comparison}, wrapped, documentStageOptions(fixture, imageBudget(t)))
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
	if err == nil && path.Path() == recoveryJournalFileName {
		writer.context.armed.Store(true)
	}
	return err
}

func documentStageOptions(fixture nativeRecordFixture, budget *physicalwork.Budget) nativeDocumentStageOptions {
	return nativeDocumentStageOptions{stateCodec: fixture.codec, codecs: fixture.read.codecs, limits: fixture.read.limits, budget: budget}
}

func requireStagedDocumentJournal(t *testing.T, fixture nativeRecordFixture, decoded nativeJournalSnapshots, comparisons []journaldocument.Comparison) {
	t.Helper()
	destination, capability := imageStageDestination(t)
	budget := imageBudget(t)
	remainingBytes, remainingEntries := budget.RemainingBytes(), budget.RemainingEntries()
	var fingerprint string
	prepared, err := journalTestFilesystem().PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		var err error
		fingerprint, err = decoded.stageDocumentJournal(t.Context(), comparisons, writer, documentStageOptions(fixture, budget))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort(context.Background())
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private staging made journal visible: %v", err)
	}
	if err := prepared.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(destination, recoveryJournalFileName))
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint == "" || recoveryJournalRecordFingerprint(content) != fingerprint || content[len(content)-1] != '\n' {
		t.Fatal("returned fingerprint does not identify the exact staged journal")
	}
	info, err := os.Stat(filepath.Join(destination, recoveryJournalFileName))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("journal is not private0600: %v", err)
	}
	reconstructed, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	root, err := rootedpath.CaptureRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	fixture.read.artifacts, fixture.read.budget = root, imageBudget(t)
	loaded, err := reconstructed.loadDocuments(t.Context(), fixture.read)
	if err != nil || len(loaded.groups) != len(comparisons) {
		t.Fatalf("staged complete native record cannot reconstruct: %v", err)
	}
	workBytes, workEntries := int64(len(content)), 1
	for index, comparison := range comparisons {
		if !loaded.images[index].original.document.Equal(comparison.BeforeDocument()) || !loaded.images[index].expected.document.Equal(comparison.ExpectedDocument()) {
			t.Fatal("staged record lost comparison bytes/presence")
		}
		files := 0
		for _, image := range []aggregate.Document{comparison.BeforeDocument(), comparison.ExpectedDocument()} {
			if image.Exists() {
				files++
				workBytes += image.ContentSize()
			}
		}
		if files != 0 {
			workEntries += files + 1
		}
	}
	if remainingBytes-budget.RemainingBytes() != workBytes || remainingEntries-budget.RemainingEntries() != workEntries {
		t.Fatal("complete stage reset budget or omitted journal/image charges")
	}
	if !decoded.before.Equal(reconstructed.before) || !decoded.after.Equal(reconstructed.after) {
		t.Fatal("private staging changed complete state evidence")
	}
}
