package journal

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestOrdinaryCaptureStopsBeforeSecondBackupWhenSharedBytesAreExhausted(t *testing.T) {
	fixture := mixedStageFixture(t)
	second := []byte("second ordinary file\n")
	hash := artifact.HashFileContentWithExecutable(second, false)
	id, err := entity.New(entity.KindInstructions, "second")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(id, "instructions.project.claude")
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.NewManagedPathState(subject, []target.Target{target.TargetClaudeCode}, target.ScopeProject, outputtest.Parse(t, "CLAUDE.md"), hash, realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(fixture.paths.ManifestRoot, "CLAUDE.md")
	requireWholeGroupWrite(t, host, second, 0o600)
	mutation, err := journalrecord.NewManagedPathRecordMutation(state.Subject(), state.ConsumerTargets(), state.Scope(), state.Destination(), hash, hash, state.ContentKind(), 0o600, &state)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(state.Subject(), state.Destination(), true, hash, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	fixture.capture.ManagedPathMutations = append(fixture.capture.ManagedPathMutations, mutation)
	fixture.capture.ManagedPathEvidence = append(fixture.capture.ManagedPathEvidence, evidence)
	combined := fixture.whole
	combined.envelope.before, err = fixture.journal.Before().WithManagedPaths(append(fixture.journal.Before().ManagedPaths(), state))
	if err != nil {
		t.Fatal(err)
	}
	combined.envelope.after, err = fixture.journal.After().WithManagedPaths(append(fixture.journal.After().ManagedPaths(), state))
	if err != nil {
		t.Fatal(err)
	}
	fixture.journal = combined.recordForGroups(t, fixture.groups)
	budget := imageBudget(t)
	remaining := int64(2*len(fixture.ordinary) + len(second))
	consume, err := workmeasure.NewArtifactWork(0, budget.RemainingBytes()-remaining)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitTree(consume); err != nil {
		t.Fatal(err)
	}
	basis, err := fixture.journal.OrdinaryCaptureBasis()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	_, err = captureOrdinarySource(t.Context(), fixture.paths, directory, basis, fixture.capture, budget)
	if err == nil {
		t.Fatal("capture copied past the containing allocation")
	}
	first, err := os.ReadFile(filepath.Join(directory, "files", "000001"))
	if err != nil || !bytes.Equal(first, fixture.ordinary) {
		t.Fatalf("admitted first backup was not captured: %q %v", first, err)
	}
	if _, err := os.Lstat(filepath.Join(directory, "files", "000002")); !os.IsNotExist(err) {
		t.Fatalf("unadmitted second backup was created: %v", err)
	}
	actual, err := os.ReadFile(host)
	if err != nil || !bytes.Equal(actual, second) || budget.RemainingBytes() != 0 {
		t.Fatalf("capture changed the host or reset its allocation: %v", err)
	}
}

func TestUninitializedCaptureInputIsRejectedBeforeEffects(t *testing.T) {
	valid := normalizedPathCapture(t, journalrecord.CaptureCreate, target.ScopeGlobal, outputtest.Parse(t, "~/.agents/skills/review"), "", realization.PathProjectionDirectory)
	for _, bounded := range []bool{false, true} {
		work := &ordinaryCaptureWork{}
		if bounded {
			work.budget = imageBudget(t)
		}
		calls := 0
		_, err := work.globalBindings([]journalrecord.CaptureMutation{valid, {}}, func(output.Destination) (string, error) {
			calls++
			return "", nil
		}, nil)
		if err == nil || calls != 0 {
			t.Fatalf("bounded=%t: invalid input reached global observation: %d %v", bounded, calls, err)
		}
	}
	zeroError := (journalrecord.CaptureMutation{}).Validate()
	before, index, err := captureRecoveryBeforePath(t.Context(), t.TempDir(), 7, journalrecord.CaptureMutation{}, nil, nil, nil, nil, nil, nil)
	if !errors.Is(err, zeroError) || index != 7 || before.Existed {
		t.Error("invalid input reached backup capture or returned before evidence", err)
	}
	if err := validateNativePublicationPartition(t.Context(), "", NativeDocumentObservationBatch{}, []journalrecord.CaptureMutation{{}}, nil); !errors.Is(err, zeroError) {
		t.Error("invalid input reached publication observation", err)
	}
}

func TestOrdinaryDirectoryCaptureBoundsStreamingBeforeAdditionalCopies(t *testing.T) {
	root := t.TempDir()
	host := filepath.Join(root, "skills")
	requireWholeGroupWrite(t, filepath.Join(host, "a.md"), []byte("first"), 0o640)
	requireWholeGroupWrite(t, filepath.Join(host, "nested", "b.md"), []byte("second"), 0o600)
	requireWholeGroupWrite(t, filepath.Join(host, "nested", "c.md"), []byte("third"), 0o600)
	hash, kind, err := access.HashPath(t.Context(), host)
	if err != nil || kind != artifact.ArtifactKindDirectory {
		t.Fatal(err)
	}
	budget := imageBudget(t)
	consume, err := workmeasure.NewArtifactWork(budget.RemainingEntries()-9, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitTree(consume); err != nil {
		t.Fatal(err)
	}
	work := &ordinaryCaptureWork{budget: budget}
	session, err := work.manifest(Paths{ManifestRoot: root}, nativeNamespaceRoot(t, root).Root)
	if err != nil {
		t.Fatal(err)
	}
	action := normalizedPathCapture(t, journalrecord.CaptureReplace, target.ScopeProject, outputtest.Parse(t, "skills"), hash, realization.PathProjectionDirectory)
	directory := t.TempDir()
	_, _, err = captureProjectExistingRecoveryBeforePath(t.Context(), ordinaryCaptureReader{Reader: journalTestFilesystem(), work: work}, directory, 0, action, session, work)
	if err == nil {
		t.Fatal("oversized source tree escaped the remaining shared entry limit")
	}
	first, err := os.ReadFile(filepath.Join(directory, "files", "000001", "a.md"))
	if err != nil || string(first) != "first" {
		t.Fatalf("admitted prefix was not captured: %q %v", first, err)
	}
	if _, err := os.Lstat(filepath.Join(directory, "files", "000001", "nested", "b.md")); !os.IsNotExist(err) {
		t.Fatalf("directory capture copied past its admitted prefix: %v", err)
	}
	for _, destination := range []output.Destination{outputtest.Parse(t, "skills/a.md"), outputtest.Parse(t, "skills/nested/b.md"), outputtest.Parse(t, "skills/nested/c.md")} {
		if _, err := os.Stat(filepath.Join(root, destination.RelativePath())); err != nil {
			t.Fatal("failed backup capture changed a source file", err)
		}
	}
}
