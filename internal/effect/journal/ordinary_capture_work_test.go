package journal

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

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
	entry := recoveryEntryFor("second", "CLAUDE.md", string(hash), string(hash), "unused")
	state := resourceStateWithPermissions(entry, string(hash), realization.PathPermissionsExact, 0o600)
	host := filepath.Join(fixture.paths.ManifestRoot, "CLAUDE.md")
	requireWholeGroupWrite(t, host, second, 0o600)
	mutation, err := NewManagedPathRecordMutation(state.Subject(), state.ConsumerTargets(), state.Scope(), state.Destination(), hash, hash, state.ContentKind(), 0o600, &state)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(state.Subject(), state.Destination(), true, hash, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	fixture.capture.ManagedPathMutations = append(fixture.capture.ManagedPathMutations, mutation)
	fixture.capture.ManagedPathEvidence = append(fixture.capture.ManagedPathEvidence, evidence)
	fixture.journal.before, err = fixture.journal.before.WithManagedPaths(append(fixture.journal.before.ManagedPaths(), state))
	if err != nil {
		t.Fatal(err)
	}
	fixture.journal.after, err = fixture.journal.after.WithManagedPaths(append(fixture.journal.after.ManagedPaths(), state))
	if err != nil {
		t.Fatal(err)
	}
	budget := imageBudget(t)
	remaining := int64(2*len(fixture.ordinary) + len(second))
	consume, err := workmeasure.NewArtifactWork(0, budget.RemainingBytes()-remaining)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitTree(consume); err != nil {
		t.Fatal(err)
	}
	basis, err := fixture.journal.ordinaryCaptureBasis()
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
	action := pathMutation{
		Kind: pathMutationReplace, Scope: target.ScopeProject, Destination: outputtest.Parse(t, "skills"),
		LiveExists: true, LiveHash: hash, LivePathExists: true, LivePathHash: hash,
	}
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
