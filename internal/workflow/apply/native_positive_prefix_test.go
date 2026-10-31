package apply

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/reconcile"
	projectionbuild "github.com/isty2e/daem/internal/reconcile/build/projection"
)

func TestPositiveNativePrefixPreparesOnlyItsActualStateProgress(t *testing.T) {
	for _, kind := range []reconcile.AggregateDecisionKind{
		reconcile.AggregateCreate, reconcile.AggregateReplace, reconcile.AggregateRecord,
		reconcile.AggregateNoOp, reconcile.AggregateRemove,
	} {
		t.Run(string(kind), func(t *testing.T) {
			input, before := nativeAggregatePlanningInput(t, kind)
			other := nativePreparationOtherWholeState(t)
			before, err := before.WithManagedPaths([]durable.ManagedPathState{other})
			if err != nil {
				t.Fatal(err)
			}
			decisions, err := projectionbuild.BuildAggregateDecisions(input)
			if err != nil {
				t.Fatal(err)
			}
			prefix, err := projectionbuild.BuildPositiveNativePrefix(t.Context(), decisions)
			if err != nil {
				t.Fatal(err)
			}
			codec := nativePreparationStateCodec(t)
			prepared, err := execute.PrepareNativeApplyState(t.Context(), execute.NativeApplyStateInput{
				Before: before, Aggregates: prefix, StateCodec: codec,
			})
			if err != nil {
				t.Fatal(err)
			}
			after := prepared.AfterState()
			if len(after.ManagedPaths()) != 1 || !after.ManagedPaths()[0].Equal(other) {
				t.Fatal("prefix discarded unrelated complete-snapshot state")
			}
			if kind == reconcile.AggregateNoOp || kind == reconcile.AggregateRemove {
				if !after.Equal(before) || len(prepared.AggregateEffects()) != 0 {
					t.Fatal("unchanged or retiring-only keys manufactured prefix progress")
				}
			} else {
				if len(after.ManagedAggregates()) != 1 || !after.ManagedAggregates()[0].Contribution().Equal(input.Desired[0].Contribution()) {
					t.Fatal("prefix state does not contain the selected desired contribution")
				}
			}
			encoded, err := codec.Encode(after)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := codec.Decode(encoded)
			if err != nil || !decoded.Equal(after) {
				t.Fatal("prefix state did not retain the native complete-snapshot codec", err)
			}
		})
	}
}
