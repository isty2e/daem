package journal

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
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
	entry := recoveryEntryFor("ordinary", "AGENTS.md", "", string(hash), "")
	row := resourceStateWithPermissions(entry, string(hash), realization.PathPermissionsExact, 0o600)
	var err error
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
	mutation, err := NewManagedPathCreateMutation(row.Subject(), row.ConsumerTargets(), row.Scope(), row.Destination(), hash, row.ContentKind(), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(row.Subject(), row.Destination(), false, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	input.options.ManagedPathMutations = []ManagedPathMutation{mutation}
	input.options.ManagedPathEvidence = []observe.ManagedPathEvidence{evidence}
	input.options.Resolver = func(destination output.Destination) (string, error) {
		return filepath.Join(input.paths.ManifestRoot, filepath.FromSlash(destination.RelativePath())), nil
	}
	captured, err := input.capture(t.Context())
	if err != nil || captured.Validate() != nil {
		t.Fatalf("observed empty document batch could not capture ordinary work: %v", err)
	}
	if !captured.BeforeState().Equal(before) || !captured.AfterState().Equal(input.after) || len(captured.journal.record.Entries) != 1 || len(captured.journal.record.DocumentGroups) != 0 {
		t.Fatal("capture lost complete state or invented a native document group")
	}
	if captured.budget != input.batch.budget || captured.publication.roots.Manifest.Root != input.batch.roots.Manifest.Root {
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
