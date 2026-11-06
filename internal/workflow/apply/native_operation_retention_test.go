package apply

import (
	"context"
	"strings"
	"testing"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	projectionbuild "github.com/isty2e/daem/internal/reconcile/build/projection"
)

func TestNativeOperationPreparationRetainsNoOpAdmission(t *testing.T) {
	planning, before := nativeAggregatePlanningInput(t, reconcile.AggregateNoOp)
	selection, err := planning.Evidence[0].Snapshot().Selection()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	limits := configdocument.Limits{MaximumBytes: 32, MaximumDepth: 8}
	planning.Codecs, err = aggregatecodec.ForOperation(ctx, []aggregate.CodecContractID{selection.CodecContractID()}, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := projectionbuild.BuildAggregateDecisions(planning)
	if err != nil || len(decisions) != 1 || decisions[0].Kind() != reconcile.AggregateNoOp {
		t.Fatal("fixture did not produce the actual no-op", err)
	}
	input := effectpreparation.NativeApplyStateInput{Before: before, Aggregates: decisions, StateCodec: nativePreparationStateCodec(t)}
	prepared, err := effectpreparation.PrepareNativeApplyState(t.Context(), input)
	if err != nil || prepared.Validate() != nil {
		t.Fatal(err)
	}
	if len(prepared.AggregateEffects()) != 0 || !prepared.BeforeState().Equal(prepared.AfterState()) {
		t.Fatal("retaining static admission manufactured a no-op mutation")
	}
	operations := prepared.NativeAggregateOperations()
	if len(operations) != 1 || operations[0].ValidateSelection(selection) != nil || len(operations[0].RequiredObservations()) == 0 {
		t.Fatal("no-op preparation lost its selected static contract")
	}
	operation := operations[0]
	operations[0] = profile.NativeAggregateOperationContract{}
	if prepared.NativeAggregateOperations()[0].ValidateSelection(selection) != nil {
		t.Fatal("caller modified preparation's retained contract")
	}
	if _, failure := operation.Codec().Read(planning.Evidence[0].Document(), selection); failure != nil {
		t.Fatal("retained concrete codec could not read the actual document", failure)
	}
	if _, failure := operation.Codec().Read(aggregate.ExistingDocument([]byte(strings.Repeat(" ", 40)+"{}")), selection); failure == nil {
		t.Fatal("preparation replaced the selected codec's actual document limit")
	}
	cancel()
	if err := operation.Validate(); err != nil {
		t.Fatal("static admission was confused with a live observation", err)
	}
	if _, failure := operation.Codec().Read(planning.Evidence[0].Document(), selection); failure == nil {
		t.Fatal("preparation lost the actual codec context")
	}

	input.ManagedPaths = []reconcile.ManagedPathDecision{{}}
	failed, err := effectpreparation.PrepareNativeApplyState(t.Context(), input)
	if err == nil || failed.Validate() == nil || len(failed.NativeAggregateOperations()) != 0 {
		t.Fatal("failed preparation exposed a prefix of native admission")
	}
	empty, err := effectpreparation.PrepareNativeApplyState(t.Context(), effectpreparation.NativeApplyStateInput{StateCodec: nativePreparationStateCodec(t)})
	if err != nil || len(empty.NativeAggregateOperations()) != 0 {
		t.Fatal("preparation invented native contracts without native decisions", err)
	}
}
