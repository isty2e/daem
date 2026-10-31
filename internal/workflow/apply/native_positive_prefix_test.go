package apply

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/reconcile"
	projectionbuild "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/target"
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

func TestPositiveNativePrefixAndFreshSuffixPreserveDistinctRetirements(t *testing.T) {
	root := t.TempDir()
	previous := nativeProjectStateSource(t, root, "old", `{"a":1,"z":1}`, hostconfig.OwnershipKeys)
	desired := nativeProjectStateSource(t, root, "new", `{"a":2}`, hostconfig.OwnershipKeys)
	states := nativeStateFromObservedKeys(t, previous.observed)
	before, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths: []durable.ManagedPathState{nativePreparationOtherWholeState(t)}, ManagedAggregates: states,
	})
	if err != nil {
		t.Fatal(err)
	}
	codecID, _ := aggregate.HostConfigCodecForSyntax("json")
	catalog, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{codecID}, narrowingLimits, narrowingLimits)
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := catalog.Lookup(codecID)
	contracts := make([]aggregate.ProjectionContract, 0, len(states))
	for _, state := range states {
		contracts = append(contracts, state.Contribution().Contract())
	}
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	document := aggregate.ExistingDocument([]byte(`{"a":1,"z":1,"foreign":true}`))
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	evidence, err := observe.NewAggregateEvidence(document, snapshot, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := reconcile.NewSelectedTargets([]target.Target{target.TargetPi})
	if err != nil {
		t.Fatal(err)
	}
	var items []aggregate.SubjectContribution
	for _, contract := range desired.observed.Projections() {
		item, present, err := contract.ManagedAggregateContribution()
		if err != nil || !present {
			t.Fatal("source did not produce a selected key", err)
		}
		items = append(items, item)
	}
	input := projectionbuild.AggregateInput{
		Locked: desired.file.Locked, Expected: desired.observed.Projections(), Desired: items,
		States: states, Evidence: []observe.AggregateEvidence{evidence}, SelectedTargets: selected, Codecs: catalog,
	}
	complete, err := projectionbuild.BuildAggregateDecisions(input)
	if err != nil {
		t.Fatal(err)
	}
	prefix, err := projectionbuild.BuildPositiveNativePrefix(t.Context(), complete)
	if err != nil || len(prefix) != 1 {
		t.Fatal("prefix was not prepared from the complete reconciliation", err)
	}
	stateCodec := nativePreparationStateCodec(t)
	prepared, err := execute.PrepareNativeApplyState(t.Context(), execute.NativeApplyStateInput{
		Before: before, Aggregates: prefix, StateCodec: stateCodec,
	})
	if err != nil {
		t.Fatal(err)
	}
	afterPrefix := prepared.AfterState()
	if len(afterPrefix.ManagedAggregates()) != 2 {
		t.Fatal("prefix retired a different physical key or retained a replaced subject")
	}
	for _, state := range afterPrefix.ManagedAggregates() {
		if state.Contribution().Address().KeyPath().String() == "/a" {
			if state.Subject() != items[0].SubjectID() || !state.Contribution().Equal(items[0].Contribution()) {
				t.Fatal("prefix did not transfer the selected footprint to its new subject")
			}
			continue
		}
		if state.Contribution().Address().KeyPath().String() != "/z" {
			t.Fatal("prefix introduced unrelated aggregate state")
		}
		found := false
		for _, old := range states {
			found = found || state.Equal(old)
		}
		if !found {
			t.Fatal("prefix changed the retiring key's recorded baseline")
		}
	}

	current := prefix[0].Rendered().Document()
	fresh, failure := codec.Read(current, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	for _, state := range fresh.States() {
		if state.Contract().Address().KeyPath().String() == "/a" && state.CanonicalProjection() != items[0].Contribution().CanonicalContribution() {
			t.Fatal("prefix content does not contain the selected desired value")
		}
		if state.Contract().Address().KeyPath().String() == "/z" && !state.Present() {
			t.Fatal("prefix removed retiring content before the invocation")
		}
	}
	evidence, err = observe.NewAggregateEvidence(current, fresh, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	input.States, input.Evidence = afterPrefix.ManagedAggregates(), []observe.AggregateEvidence{evidence}
	suffix, err := projectionbuild.BuildAggregateDecisions(input)
	if err != nil {
		t.Fatal(err)
	}
	settled, err := execute.PrepareNativeApplyState(t.Context(), execute.NativeApplyStateInput{
		Before: afterPrefix, Aggregates: suffix, StateCodec: stateCodec,
	})
	if err != nil {
		t.Fatal(err)
	}
	whole, err := execute.PrepareNativeApplyState(t.Context(), execute.NativeApplyStateInput{
		Before: before, Aggregates: complete, StateCodec: stateCodec,
	})
	if err != nil || !settled.AfterState().Equal(whole.AfterState()) || !suffix[0].Rendered().Expected().Equal(complete[0].Rendered().Expected()) {
		t.Fatal("fresh suffix diverged from the complete selected result", err)
	}
}
