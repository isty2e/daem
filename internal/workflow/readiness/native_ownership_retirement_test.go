package readiness

import (
	"os"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeMissingHistoryRetiresThroughActualGlobalPlanner(t *testing.T) {
	fixture := newNativePlanningFixture(t, target.ScopeGlobal, "extension-settings/Café.json")
	if err := os.Remove(fixture.hostPath); err != nil {
		t.Fatal(err)
	}
	fixture.observe(t)
	claims, err := fixture.input.Documents.Evidence().Ownership(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	row := fixture.observation.Before.ManagedAggregates()[0]
	owned, err := claims.Global(t.Context(), fixture.observation.Aliases[0], row.Contribution().Footprint())
	if err != nil || !owned.CorrelatesClaimAddress() {
		t.Fatal("missing baseline has no matching actual retained claim", err)
	}
	catalog, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{row.Contribution().CodecContractID()}, narrowingLimits, narrowingLimits)
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := catalog.Lookup(row.Contribution().CodecContractID())
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{row.Contribution().Contract()})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, failure := codec.Read(aggregate.AbsentDocument(), selection)
	if failure != nil {
		t.Fatal(failure)
	}
	evidence, err := observe.NewAggregateEvidence(aggregate.AbsentDocument(), snapshot, 0)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := reconcile.NewSelectedTargets(fixture.input.Selection.Targets())
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := projection.BuildAggregateDecisions(projection.AggregateInput{
		States: fixture.observation.Before.ManagedAggregates(), Evidence: []observe.AggregateEvidence{evidence},
		SelectedTargets: selected, Owner: fixture.observation.Owner, Ownership: []observe.OwnershipObservation{owned}, Codecs: catalog,
	})
	if err != nil || len(decisions) != 1 || decisions[0].Kind() != reconcile.AggregateRecord {
		t.Fatal("actual retained missing ownership was rejected by native planning", err)
	}
	state, err := execute.PrepareNativeApplyState(t.Context(), execute.NativeApplyStateInput{Before: fixture.observation.Before, Aggregates: decisions, StateCodec: fixture.codec})
	if err != nil || len(state.AfterState().ManagedAggregates()) != 0 || len(state.AggregateEffects()) != 1 {
		t.Fatal("missing historical key did not retire its actual baseline", err)
	}
	effect := state.AggregateEffects()[0]
	if effect.Kind() != execute.AggregateEffectRecord || effect.BeforeDocument().Exists() || effect.Rendered().Document().Exists() || len(effect.ProjectionEffects()) != 1 || effect.ProjectionEffects()[0].MutatesHost() {
		t.Fatal("absent retirement became a host mutation or invented document presence")
	}
	if _, err := os.Lstat(fixture.hostPath); !os.IsNotExist(err) {
		t.Fatal("planning recreated an already missing file", err)
	}
}
