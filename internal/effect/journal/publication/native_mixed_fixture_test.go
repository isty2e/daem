package publication

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/entity"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type mixedJournalFixture struct {
	whole      wholeGroupTestFixture
	journal    journalrecord.Native
	groups     []journalrecord.SealedDocumentGroup
	comparison journaldocument.Comparison
	paths      Paths
	capture    CaptureOptions
	ordinary   []byte
}

func (fixture mixedJournalFixture) stageOptions(budget *physicalwork.Budget) nativeMixedStageOptions {
	return nativeMixedStageOptions{StageOptions: documentStageOptions(fixture.whole.envelope, budget), roots: fixture.whole.roots}
}

func mixedStageFixture(t *testing.T) mixedJournalFixture {
	t.Helper()
	whole := wholeGroupFixture(t, target.TargetPi, target.ScopeProject, "replace", []byte("native before\x00"), []byte("native after\xff"))
	group, comparison, err := prepareWholeDocumentGroup(t.Context(), whole.namespace, whole.input)
	if err != nil {
		t.Fatal(err)
	}
	root := whole.roots.Manifest
	ordinary := []byte("ordinary executable\n")
	hash := artifact.HashFileContentWithExecutable(ordinary, true)
	id, err := entity.New(entity.KindInstructions, "ordinary")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(id, "instructions.project.agents")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.NewManagedPathState(subject, []target.Target{target.TargetCodex}, target.ScopeProject, destination, hash, realization.PathProjectionFile, realization.PathPermissionsExact, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	requireWholeGroupWrite(t, filepath.Join(root.SelectedPath, "AGENTS.md"), ordinary, 0o700)
	mutation, err := journalrecord.NewManagedPathRecordMutation(state.Subject(), state.ConsumerTargets(), state.Scope(), state.Destination(), hash, hash, state.ContentKind(), 0o700, &state)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(state.Subject(), state.Destination(), true, hash, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	appendState := func(value durable.Snapshot) durable.Snapshot {
		result, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: append(value.ManagedPaths(), state), ManagedAggregates: value.ManagedAggregates()})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	combined := whole
	combined.envelope.before, combined.envelope.after = appendState(whole.envelope.before), appendState(whole.envelope.after)
	journal := combined.recordForGroups(t, []journalrecord.SealedDocumentGroup{group})
	paths := Paths{ManifestRoot: root.SelectedPath, StatefilePath: filepath.Join(root.SelectedPath, "state.json"), RecoveryDir: filepath.Join(root.SelectedPath, "recovery")}
	capture := CaptureOptions{ManagedPathMutations: []journalrecord.ManagedPathMutation{mutation}, ManagedPathEvidence: []observe.ManagedPathEvidence{evidence}, ManifestRoot: root.Root, Filesystem: journalTestFilesystem(), StateCodec: whole.envelope.codec, Codecs: whole.envelope.read.Codecs, Resolver: func(destination output.Destination) (string, error) {
		return filepath.Join(root.SelectedPath, filepath.FromSlash(destination.RelativePath())), nil
	}}
	return mixedJournalFixture{whole: whole, journal: journal, groups: []journalrecord.SealedDocumentGroup{group}, comparison: comparison, paths: paths, capture: capture, ordinary: ordinary}
}

func (fixture mixedJournalFixture) source(t *testing.T, budget *physicalwork.Budget) (ordinaryCaptureSource, string) {
	t.Helper()
	directory := t.TempDir()
	basis, err := fixture.journal.OrdinaryCaptureBasis()
	if err != nil {
		t.Fatal(err)
	}
	source, err := captureOrdinarySource(t.Context(), fixture.paths, directory, basis, fixture.capture, budget)
	if err != nil {
		t.Fatal(err)
	}
	return source, directory
}

func documentStageOptions(fixture nativeRecordFixture, budget *physicalwork.Budget) StageOptions {
	return StageOptions{StateCodec: fixture.codec, Codecs: fixture.read.Codecs, Limits: fixture.read.Limits, Budget: budget}
}
