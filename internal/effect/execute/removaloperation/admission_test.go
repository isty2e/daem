//go:build darwin || linux

package removaloperation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/target"
)

func TestForwardCandidatesAreValidatedBeforeObservationOrReservation(t *testing.T) {
	for _, fault := range []string{"missing measurement", "empty state", "invalid relation"} {
		t.Run(fault, func(t *testing.T) {
			root := newProjectRootForMutationTest(t)
			path := filepath.Join(root, ".agent", "config")
			writeMutationTestFile(t, path, "inside")
			operation, destination := projectMutationDestinationForTest(t, root, ".agent/config")
			bindTestFileRemovalIntent(t, operation, destination, []byte("inside"))
			demand := operation.removalDemands.Demands()[0]
			measurements := 0
			valid := ForwardCandidate{
				Relation: Relation{Scope: demand.Scope(), Destination: demand.Destination()},
				State:    demand.States()[0],
				Measure: func(context.Context, workmeasure.ArtifactWork) (workmeasure.ArtifactWork, error) {
					measurements++
					return workmeasure.NewArtifactWork(0, int64(len("inside")))
				},
			}
			invalid := valid
			switch fault {
			case "missing measurement":
				invalid.Measure = nil
			case "empty state":
				invalid.State = recovery.RemovalState{}
			case "invalid relation":
				invalid.Relation.Scope = target.Scope("")
			}
			remaining := operation.physicalWorkBudget.RemainingTreeWork()

			if err := operation.PrepareForward(t.Context(), []ForwardCandidate{valid, invalid}); err == nil {
				t.Fatal("invalid later candidate was admitted")
			}
			if measurements != 0 || operation.physicalWorkBudget.RemainingTreeWork() != remaining {
				t.Fatal("invalid later candidate allowed prefix observation or measurement")
			}
			if operation.forwardRemovalPrepared || operation.forwardRemovalExecution != nil || operation.forwardRemovalReservations != nil {
				t.Fatal("invalid candidate published partial removal capacity")
			}
			if err := operation.PrepareForward(t.Context(), nil); err != nil {
				t.Fatalf("prepare the unchanged valid current state after refusal: %v", err)
			}
			assertMutationTestFile(t, path, "inside")
		})
	}
}

func TestCleanupFollowsTheSharedTraversalPhase(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		name := "without reserved cleanup"
		if prepared {
			name = "with reserved cleanup"
		}
		t.Run(name, func(t *testing.T) {
			root := newProjectRootForMutationTest(t)
			path := filepath.Join(root, ".agent", "config")
			writeMutationTestFile(t, path, "inside")
			operation, destination := projectMutationDestinationForTest(t, root, ".agent/config")
			bindTestFileRemovalIntent(t, operation, destination, []byte("inside"))
			intent, err := operation.IntentFor(destination.Scope(), destination.Logical())
			if err != nil {
				t.Fatal(err)
			}
			intents := []recovery.RemovalIntent{intent}
			if prepared {
				if err := operation.PrepareCleanup(intents); err != nil {
					t.Fatal(err)
				}
			}
			execution, err := physicalwork.NewBudget(1)
			if err != nil {
				t.Fatal(err)
			}
			if err := operation.traversal.Advance(execution, execution); err != nil {
				t.Fatal(err)
			}

			obligations, err := operation.CleanupIntents(t.Context(), intents, false, func(obligations []recovery.RemovalCleanupObligation) bool {
				return len(obligations) == 1 && obligations[0].Readiness() == recovery.RemovalCleanupDischarged
			})
			assertMutationTestFile(t, path, "inside")
			if !prepared {
				if err == nil || obligations != nil {
					t.Fatalf("cleanup without execution allocation = %v, %v; want refusal", obligations, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("cleanup using its reserved allocation: %v", err)
			}
			if len(obligations) != 1 || obligations[0].Readiness() != recovery.RemovalCleanupDischarged {
				t.Fatalf("completed cleanup obligations = %v", obligations)
			}
			for _, name := range []string{intent.Namespace().Names().Residue(), intent.Namespace().Names().Cleanup()} {
				if _, err := os.Lstat(filepath.Join(filepath.Dir(path), name)); !os.IsNotExist(err) {
					t.Fatalf("removal slot after complete cleanup: %v", err)
				}
			}
		})
	}
}

func TestNativeSourceKeepsWriterSelectionAndOriginalAllocation(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		name := "selected native writer"
		if duplicate {
			name = "duplicate native writer"
		}
		t.Run(name, func(t *testing.T) {
			root := newProjectRootForMutationTest(t)
			path := filepath.Join(root, ".agent", "config")
			writeMutationTestFile(t, path, "inside")
			fixture, ordinary := projectMutationDestinationForTest(t, root, ".agent/config")
			bindTestFileRemovalIntent(t, fixture, ordinary, []byte("inside"))
			intent, err := fixture.IntentFor(ordinary.Scope(), ordinary.Logical())
			if err != nil {
				t.Fatal(err)
			}
			allocation, err := physicalwork.NewBudget(1)
			if err != nil {
				t.Fatal(err)
			}
			if err := allocation.AdmitPhysicalWork(0, 0, 7); err != nil {
				t.Fatal(err)
			}
			writerRoot, writerPath, err := rootedpath.CaptureDestinationBounded(path, physicalwork.MaximumPhysicalPathDepth, allocation)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = writerRoot.Close() })
			writer, err := fixture.destinations.BorrowDestination(ordinary.Scope(), ordinary.Logical(), writerRoot, writerPath)
			if err != nil {
				t.Fatal(err)
			}
			if writer.RootedPath().Equal(ordinary.RootedPath()) {
				t.Fatal("fixture did not distinguish the writer binding from the ordinary project binding")
			}
			writers := []mutationnamespace.Destination{writer}
			if duplicate {
				writers = append(writers, writer)
			}
			operation := New(fixture.destinations, fixture.filesystem, fixture.control, fixture.physicalWorkBudget, fixture.traversal)
			remaining := allocation.RemainingTreeWork()

			err = operation.PrepareNativeSource(fixture.removalDemands, []mutationnamespace.Destination{ordinary}, writers, allocation, []recovery.RemovalIntent{intent})
			if duplicate {
				if err == nil {
					t.Fatal("duplicate native writer was admitted")
				}
				if _, err := operation.DestinationFor(ordinary.Scope(), ordinary.Logical()); err == nil {
					t.Fatal("refused source exposed a usable removal destination")
				}
				assertMutationTestFile(t, path, "inside")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			selected, err := operation.DestinationFor(ordinary.Scope(), ordinary.Logical())
			if err != nil {
				t.Fatal(err)
			}
			if !selected.RootedPath().Equal(writer.RootedPath()) {
				t.Fatal("ordinary binding displaced the selected native writer")
			}
			if operation.physicalWorkBudget != allocation || allocation.RemainingTreeWork() != remaining {
				t.Fatal("native source replaced or replenished its previously charged allocation")
			}
		})
	}
}
