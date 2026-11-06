package hostmutation

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/execute/controlwitness"
	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"
	"github.com/isty2e/daem/internal/effect/execute/removaloperation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func projectMutationOperation(t *testing.T, root string, filesystem mutationfs.Store) *Operation {
	t.Helper()
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	phase, err := physicalwork.NewTraversalPhase(budget)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := mutationnamespace.New(func(destination output.Destination) (string, error) {
		return filepath.Join(root, filepath.FromSlash(destination.RelativePath())), nil
	}, phase)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := rootedpath.CaptureRootBounded(root, physicalwork.MaximumPhysicalPathDepth, phase)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := errors.Join(namespace.Close(), captured.Close()); err != nil {
			t.Error(err)
		}
	})
	if err := namespace.BindProject(captured, nil); err != nil {
		t.Fatal(err)
	}
	removals := removaloperation.New(namespace, filesystem, controlwitness.New(filesystem), budget, phase)
	return New(namespace, filesystem, removals)
}

func testAggregateCodecs() aggregate.CodecCatalog { return aggregatecodec.Catalog() }

func testSelectedTargets(t *testing.T, values ...target.Target) reconcile.SelectedTargets {
	t.Helper()
	selected, err := reconcile.NewSelectedTargets(values)
	if err != nil {
		t.Fatalf("NewSelectedTargets returned error: %v", err)
	}
	return selected
}
