package publication

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func aliasedMixedWriterFixture(t *testing.T) mixedJournalFixture {
	t.Helper()
	fixture := mixedStageFixture(t)
	root := fixture.whole.roots.Manifest
	ordinaryPath := filepath.Join(root.SelectedPath, "AGENTS.md")
	before := fixture.comparison.BeforeDocument().Content()
	requireWholeGroupWrite(t, ordinaryPath, before, 0o644)
	if err := os.Chmod(ordinaryPath, 0o644); err != nil {
		t.Fatal(err)
	}
	address := fixture.whole.namespace.Addresses()[0]
	aliasPath := filepath.Join(root.SelectedPath, filepath.FromSlash(address.AggregateRoot().RelativePath()))
	if err := os.Remove(aliasPath); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(filepath.Dir(aliasPath), ordinaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relative, aliasPath); err != nil {
		t.Fatal(err)
	}
	namespace, err := journalnamespace.Observe(t.Context(), fixture.whole.namespace.Addresses(), fixture.whole.roots, journalTestFilesystem(), fixture.whole.input.Budget())
	if err != nil {
		t.Fatal(err)
	}
	fixture.whole.namespace = namespace
	exact, known := namespace.Observation().Exact()
	if !known {
		t.Fatal("existing aliased file requires exact namespace evidence")
	}
	binding, err := ownership.NewDocumentBinding(fixture.whole.input.Owner(), address.AggregateRoot(), exact)
	if err != nil {
		t.Fatal(err)
	}
	present, err := ownership.PresentDocumentBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := ownership.NewExactDocumentBindingIntent(fixture.whole.input.Owner(), address.AggregateRoot(), present, present)
	if err != nil {
		t.Fatal(err)
	}
	fixture.whole.input.CaptureInput = reviseCaptureInput(fixture.whole.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) {
		spec.Bindings = []ownership.DocumentBindingIntent{intent}
	})
	group, comparison, err := prepareWholeDocumentGroup(t.Context(), namespace, fixture.whole.input)
	if err != nil {
		t.Fatal(err)
	}
	fixture.comparison = comparison
	hash := artifact.HashFileContent(before)
	base := fixture.capture.ManagedPathMutations[0]
	state, err := durable.NewManagedPathState(base.Subject(), base.ConsumerTargets(), base.Scope(), base.Destination(), hash, base.ContentKind(), realization.PathPermissionsExact, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := journalrecord.NewManagedPathRecordMutation(state.Subject(), state.ConsumerTargets(), state.Scope(), state.Destination(), hash, hash, state.ContentKind(), 0o644, &state)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(state.Subject(), state.Destination(), true, hash, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	withOrdinary := func(snapshot durable.Snapshot) durable.Snapshot {
		result, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: append(snapshot.ManagedPaths(), state), ManagedAggregates: snapshot.ManagedAggregates()})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	combined := fixture.whole
	combined.envelope.before, combined.envelope.after = withOrdinary(fixture.whole.envelope.before), withOrdinary(fixture.whole.envelope.after)
	fixture.journal = combined.recordForGroups(t, []journalrecord.SealedDocumentGroup{group})
	fixture.capture.ManagedPathMutations, fixture.capture.ManagedPathEvidence = []journalrecord.ManagedPathMutation{mutation}, []observe.ManagedPathEvidence{evidence}
	return fixture
}

func TestNativeMixedWriterPartitionRefusesActualAliasedOrdinaryWriter(t *testing.T) {
	fixture := aliasedMixedWriterFixture(t)
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	writer := &mixedNoWriteWriter{}
	fingerprint, err := stageMixedDocumentJournal(fixture.journal, t.Context(), fixture.paths, fixture.capture, []journaldocument.Comparison{fixture.comparison}, writer, fixture.stageOptions(imageBudget(t)))
	if err == nil || fingerprint != "" || (writer.directories+writer.writes) != 0 {
		t.Fatalf("aliased native and ordinary writers staged a usable result: fingerprint=%q calls=%d err=%v", fingerprint, (writer.directories + writer.writes), err)
	}
	if entries, err := os.ReadDir(temporary); err != nil || len(entries) != 0 {
		t.Fatalf("refused writer partition retained ordinary build artifacts: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(fixture.paths.ManifestRoot, "AGENTS.md"))
	if err != nil || !slices.Equal(got, fixture.comparison.BeforeDocument().Content()) {
		t.Fatalf("partition refusal changed host bytes: %v", err)
	}
}

func TestNativeWriterPartitionRefusesAtEveryRecordBoundary(t *testing.T) {
	fixture := aliasedMixedWriterFixture(t)
	source, _ := fixture.source(t, imageBudget(t))
	journal, err := fixture.journal.WithOrdinaryEvidence(source.record)
	if err != nil {
		t.Fatal(err)
	}
	if content, err := journal.Encode(t.Context(), fixture.whole.envelope.codec); err == nil || content != nil {
		t.Error("strict record construction admitted overlapping physical writers")
	}
	if prepared, err := journal.PrepareDocuments(t.Context(), []journaldocument.Comparison{fixture.comparison}, fixture.whole.input.Limits(), fixture.whole.envelope.read.Codecs); err == nil {
		t.Error("prepared comparison boundary admitted an overlapping group")
	} else if comparisons, err := prepared.Comparisons(); err == nil || comparisons != nil {
		t.Error("prepared comparison boundary returned an overlapping group prefix")
	}
	read := fixture.whole.envelope.read
	reader := &nativeNamespaceReader{RootedReader: read.Filesystem}
	read.Filesystem, read.Budget = reader, imageBudget(t)
	if loaded, err := journalqualification.LoadDocuments(journal, t.Context(), read); err == nil || loaded.Len() != 0 || reader.imageReads != 0 {
		t.Error("artifact boundary admitted overlap or read images before refusing it")
	}
}
