package nativeoperation

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/target"
)

func TestOperationRetainsOriginalAllocationAndOwnsOnlyItsCapture(t *testing.T) {
	input, _, _ := nativeOrderApplyInput(t, target.ScopeProject)
	operation, err := newOperation(input.Paths, input.ProjectRoot, input.Resolver, input.Filesystem, input.Budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := operation.close(); err != nil {
			t.Error(err)
		}
	})
	if err := operation.bindProjectStatefile(input.Paths.ManifestRoot, input.Paths.StatefilePath); err != nil {
		t.Fatal(err)
	}
	state, err := operation.projectStatefile.Read(t.Context(), input.StatefileCodec, input.Budget)
	if err != nil || !state.Equal(input.CurrentState) {
		t.Fatal("selected statefile did not retain the input allocation", err)
	}

	captured := operation.capturedRoot
	if err := operation.close(); err != nil {
		t.Fatal(err)
	}
	if err := captured.ValidateSelectionBounded(input.Paths.ManifestRoot, physicalwork.MaximumPhysicalPathDepth, input.Budget); err == nil {
		t.Fatal("closed native operation retained its owned root")
	}
	if err := input.ProjectRoot.ValidateSelectionBounded(input.Paths.ManifestRoot, physicalwork.MaximumPhysicalPathDepth, input.Budget); err != nil {
		t.Fatal("closing native operation invalidated its borrowed root", err)
	}
}

func TestOperationRejectsDifferentBorrowedRootWithoutTakingCustody(t *testing.T) {
	input, _, _ := nativeOrderApplyInput(t, target.ScopeProject)
	foreignPath := t.TempDir()
	foreign, err := rootedpath.CaptureRoot(foreignPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = foreign.Close() })
	operation, err := newOperation(input.Paths, foreign, input.Resolver, input.Filesystem, input.Budget)
	if operation != nil || err == nil {
		if operation != nil {
			_ = operation.close()
		}
		t.Fatal("native operation admitted a different borrowed project root")
	}
	if err := foreign.ValidateSelectionBounded(foreignPath, physicalwork.MaximumPhysicalPathDepth, input.Budget); err != nil {
		t.Fatal("failed setup took custody of the borrowed root", err)
	}
}
