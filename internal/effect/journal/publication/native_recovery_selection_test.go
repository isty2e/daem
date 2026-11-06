package publication

import (
	"os"
	"path/filepath"
	"testing"

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
	if err != nil || anchors[output.RootProject] != want {
		t.Fatal("stored publication provenance became unavailable before comparison", err)
	}
	if err := captured.RequireAllocation(budget); err != nil {
		t.Fatal("original allocation became unavailable before comparison", err)
	}
	if err := captured.RequireRecoverySelection(replacement, budget); err == nil {
		t.Fatal("same-path replacement manifest was admitted for captured recovery")
	}
}
