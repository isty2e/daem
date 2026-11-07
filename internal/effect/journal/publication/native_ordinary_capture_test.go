package publication

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"

	"github.com/isty2e/daem/internal/desired/entity"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	topologyprojection "github.com/isty2e/daem/internal/topology/projection"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"
	"github.com/isty2e/daem/internal/output"

	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeCaptureWithObservedEmptyDocumentsPreservesUnselectedNativeState(t *testing.T) {
	input := nativeCaptureInput(t, false, target.ScopeProject)
	before := input.batch.Evidence().BeforeState()
	hash := artifact.HashFileContentWithExecutable([]byte("ordinary instruction\n"), false)
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
	row, err := durable.NewManagedPathState(subject, []target.Target{target.TargetCodex}, target.ScopeProject, destination, hash, realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	input.after, err = before.WithManagedPaths([]durable.ManagedPathState{row})
	if err != nil {
		t.Fatal(err)
	}
	input.edits = nil
	reader, err := ownershipstore.NewRecoveryReader(input.registry)
	if err != nil {
		t.Fatal(err)
	}
	input.batch, err = ObserveNativeDocuments(t.Context(), journalobservation.Input{
		Owner: input.batch.owner, Before: before, Roots: input.batch.roots,
		Filesystem: input.options.Filesystem, Registry: reader, Limits: input.batch.Evidence().DocumentLimits(), Budget: input.batch.budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := journalrecord.NewManagedPathCreateMutation(row.Subject(), row.ConsumerTargets(), row.Scope(), row.Destination(), hash, row.ContentKind(), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(row.Subject(), row.Destination(), false, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	input.options.ManagedPathMutations = []journalrecord.ManagedPathMutation{mutation}
	input.options.ManagedPathEvidence = []observe.ManagedPathEvidence{evidence}
	input.options.Resolver = func(destination output.Destination) (string, error) {
		return filepath.Join(input.paths.ManifestRoot, filepath.FromSlash(destination.RelativePath())), nil
	}
	captured, err := input.capture(t.Context())
	if err != nil || captured.Validate() != nil {
		t.Fatalf("observed empty document batch could not capture ordinary work: %v", err)
	}
	entries, err := captured.journal.RecordedEntries()
	if err != nil {
		t.Fatal(err)
	}
	documents, err := captured.journal.Documents()
	if err != nil {
		t.Fatal(err)
	}
	if !captured.BeforeState().Equal(before) || !captured.AfterState().Equal(input.after) || len(entries) != 1 || len(documents) != 0 {
		t.Fatal("capture lost complete state or invented a native document group")
	}
	if captured.budget != input.batch.budget || namespacetest.LegacyRoot(t, captured.publication.roots, output.RootProject).Root != namespacetest.LegacyRoot(t, input.batch.roots, output.RootProject).Root {
		t.Fatal("empty census lost the containing root or original allocation")
	}
	if _, err := os.Stat(filepath.Join(captured.CaptureResult().Directory, "documents")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("empty document census staged a document image", err)
	}
	if _, err := os.Stat(filepath.Join(input.paths.ManifestRoot, "AGENTS.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("capture performed an ordinary host effect", err)
	}
	input.requireUnchanged(t)
}
