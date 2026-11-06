package hostmutation

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/effect/execute/controlwitness"
	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"
	"github.com/isty2e/daem/internal/effect/execute/removaloperation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/hostpath"
)

type restorationTestOperation struct {
	operation *Operation
	control   *controlwitness.Guard
	original  *physicalwork.Budget
	phase     *physicalwork.TraversalPhase
	sources   *Backups
}

func newRestorationTestOperation(t *testing.T, root string, filesystem mutationfs.Store) restorationTestOperation {
	t.Helper()
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	phase, err := physicalwork.NewTraversalPhase(budget)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := mutationnamespace.New(hostpath.NewResolver(root).Resolve, phase)
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
	control := controlwitness.New(filesystem)
	removals := removaloperation.New(namespace, filesystem, control, budget, phase)
	operation := New(namespace, filesystem, removals)
	return restorationTestOperation{
		operation: operation, control: control, original: budget, phase: phase,
		sources: NewBackups(operation, budget),
	}
}

func beginRestorationExecutionForTest(t *testing.T, fixture restorationTestOperation) *physicalwork.Budget {
	t.Helper()
	if err := fixture.original.ConcludeRetirementNotApplicable(); err != nil {
		t.Fatal(err)
	}
	if err := fixture.operation.removals.PrepareCleanup(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.original.BeginReservedSemanticExecution(); err != nil {
		t.Fatal(err)
	}
	host, control, err := fixture.original.BeginGeneralExecution()
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.phase.Advance(control, host); err != nil {
		t.Fatal(err)
	}
	return host
}
