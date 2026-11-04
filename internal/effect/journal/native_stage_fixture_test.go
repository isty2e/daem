package journal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func requireStagedDocumentJournal(t *testing.T, fixture nativeRecordFixture, journal journalrecord.Native, comparisons []journaldocument.Comparison) journalrecord.Native {
	t.Helper()
	destination, capability := imageStageDestination(t)
	defer capability.Close()
	budget := imageBudget(t)
	remainingBytes, remainingEntries := budget.RemainingBytes(), budget.RemainingEntries()
	var fingerprint string
	prepared, err := journalTestFilesystem().PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		var err error
		fingerprint, err = stageDocumentJournal(journal, t.Context(), comparisons, writer, documentStageOptions(fixture, budget))
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
	content, err := os.ReadFile(filepath.Join(destination, journalrecord.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint == "" || journalrecord.Fingerprint(content) != fingerprint || content[len(content)-1] != '\n' {
		t.Fatal("returned fingerprint does not identify the exact staged journal")
	}
	info, err := os.Stat(filepath.Join(destination, journalrecord.FileName))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("journal is not private0600: %v", err)
	}
	root, err := rootedpath.CaptureRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	snapshot, err := readNativeCaptureRecord(t.Context(), root, journalTestFilesystem(), imageBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := journalrecord.DecodeNativeSnapshot(t.Context(), snapshot, journalrecord.FileName, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	reconstructed, present := decoded.Native()
	if !present {
		t.Fatal("staged record changed native family")
	}
	fixture.read.artifacts, fixture.read.budget = root, imageBudget(t)
	loaded, err := loadDocuments(reconstructed, t.Context(), fixture.read)
	if err != nil || loaded.Len() != len(comparisons) {
		t.Fatalf("staged complete native record cannot reconstruct: %v", err)
	}
	workBytes, workEntries := int64(len(content)), 1
	for index, comparison := range comparisons {
		if !requireLoadedNativeDocument(t, loaded, index).Images().Original().Document().Equal(comparison.BeforeDocument()) || !requireLoadedNativeDocument(t, loaded, index).Images().Expected().Document().Equal(comparison.ExpectedDocument()) {
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
	if !journal.Before().Equal(reconstructed.Before()) || !journal.After().Equal(reconstructed.After()) {
		t.Fatal("private staging changed complete state evidence")
	}
	return reconstructed
}

func twoWholeGroupStageFixture(t *testing.T) (wholeGroupTestFixture, wholeGroupTestFixture, []journalrecord.SealedDocumentGroup, []journaldocument.Comparison) {
	t.Helper()
	first := wholeGroupFixture(t, target.TargetPi, target.ScopeProject, "replace", []byte("first before"), []byte("first after"))
	second := wholeGroupFixture(t, target.TargetCodex, target.ScopeProject, "replace", []byte("second before"), []byte("second after"))
	second.path = filepath.Join(first.roots.Manifest.SelectedPath, filepath.FromSlash(second.input.mutation.Destination().RelativePath()))
	requireWholeGroupWrite(t, second.path, second.original.Content(), 0o644)
	roots := second.roots
	roots.Manifest = first.roots.Manifest
	var err error
	second.namespace, err = journalnamespace.Observe(t.Context(), second.namespace.Addresses(), roots, second.reader, second.input.Budget())
	if err != nil {
		t.Fatal(err)
	}
	second.input.CaptureInput = reviseCaptureInput(second.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.Owner = first.input.Owner() })
	exact, known := second.namespace.Observation().Exact()
	if !known {
		t.Fatal("existing second file requires exact authority")
	}
	binding, err := ownership.NewDocumentBinding(first.input.Owner(), second.input.mutation.Destination(), exact)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ownership.PresentDocumentBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := ownership.NewExactDocumentBindingIntent(first.input.Owner(), binding.Destination(), value, value)
	if err != nil {
		t.Fatal(err)
	}
	second.input.CaptureInput = reviseCaptureInput(second.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) {
		spec.Bindings = []ownership.DocumentBindingIntent{intent}
	})
	first.input.CaptureInput = reviseCaptureInput(first.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.GroupIndex = 0 })
	second.input.CaptureInput = reviseCaptureInput(second.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.GroupIndex = 1 })
	var groups []journalrecord.SealedDocumentGroup
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
	first.envelope = envelope
	return first, second, groups, comparisons
}
