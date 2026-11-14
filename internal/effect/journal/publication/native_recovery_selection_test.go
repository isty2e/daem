package publication

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

func TestCapturedRecoverySelectionRequiresOriginalAllocationAndManifestIncarnation(t *testing.T) {
	input := nativeCaptureInput(t, false, target.ScopeProject)
	captured, err := input.capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	budget, manifest := input.batch.budget, input.options.ManifestRoot
	if err := captured.RequireRecoverySelection(manifest, budget); err != nil {
		t.Fatal("original recovery selection was refused", err)
	}
	otherBudget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := captured.RequireAllocation(otherBudget); err == nil {
		t.Fatal("published recovery accepted a replacement allocation")
	}
	alias, err := rootedpath.CaptureRootNoFollow(input.paths.ManifestRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = alias.Close() })
	if err := captured.RequireRecoverySelection(alias, budget); err != nil {
		t.Fatal("independent handle to the same incarnation was refused", err)
	}
	if err := alias.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Authority(); err != nil {
		t.Fatal("borrowed-handle closure closed the original manifest", err)
	}
	original, err := manifest.Authority()
	if err != nil {
		t.Fatal(err)
	}
	want, err := original.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(input.paths.ManifestRoot, filepath.Join(t.TempDir(), "displaced")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(input.paths.ManifestRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	replacement, err := rootedpath.CaptureRootNoFollow(input.paths.ManifestRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = replacement.Close() })
	current, err := replacement.Authority()
	if err != nil {
		t.Fatal(err)
	}
	got, err := current.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	if got.PhysicalRoot() != want.PhysicalRoot() || got.MountFingerprint() != want.MountFingerprint() || got.ObjectFingerprint() == want.ObjectFingerprint() {
		t.Fatal("replacement control did not isolate the manifest incarnation")
	}
	anchors, err := captured.journal.RecordedRootAnchors()
	manifestReference, referenceErr := captured.journal.ManifestRootReference()
	if referenceErr != nil {
		t.Fatal(referenceErr)
	}
	if err != nil || anchors[manifestReference] != want {
		t.Fatal("stored publication provenance became unavailable before comparison", err)
	}
	if err := captured.RequireAllocation(budget); err != nil {
		t.Fatal("original allocation became unavailable before comparison", err)
	}
	if err := captured.RequireRecoverySelection(replacement, budget); err == nil {
		t.Fatal("same-path replacement manifest was admitted for captured recovery")
	}
}

func TestCaptureRefusesAnotherManifestLocatorBeforeJournalVisibility(t *testing.T) {
	input := nativeCaptureInput(t, false, target.ScopeProject)
	manifest, present := input.batch.roots.Manifest()
	if !present {
		t.Fatal("fixture has no selected manifest")
	}
	other, err := output.NewRootReference("project", "fixture-other-project-revision")
	if err != nil {
		t.Fatal(err)
	}
	input.batch.roots, err = (journalnamespace.Roots{}).With(other, manifest)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := input.capture(t.Context())
	var published *NativeJournalPublicationError
	if err == nil || captured.Validate() == nil || errors.As(err, &published) {
		t.Fatal("a mismatched manifest locator reached journal publication", err)
	}
	if _, err := os.Lstat(filepath.Join(input.paths.RecoveryDir, "native-record-apply")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a mismatched manifest locator left visible recovery evidence", err)
	}
	input.requireUnchanged(t)
}
