package applyprogram

import (
	"testing"

	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/test/outputtest"
)

func TestFailureSettlementProfilesHaveZeroStateBarrierDemand(t *testing.T) {
	for _, transitions := range []bool{false, true} {
		structure, err := CompileFailureSettlement(transitions)
		if err != nil {
			t.Fatal(err)
		}
		demand, err := structure.LegacyDemand()
		if err != nil || !demand.Empty() {
			t.Fatalf("failure settlement demand = %v, error = %v; want zero", demand, err)
		}
	}
}

func TestPlanRefusesForwardAuthorityInsideFailurePolicy(t *testing.T) {
	var builder operationplan.EffectStructureBuilder
	segment := ForwardObligation(&builder, "apply/test", operationplan.EffectStepPersistence)
	structure, err := builder.Compile(builder.ForwardPhase("apply-test", segment))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newPlan(segment, structure, structure, nil, true); err == nil {
		t.Fatal("failure policy admitted forward State Barrier authority")
	}
}

func TestUnavailablePromotionScheduleCannotBindAPlan(t *testing.T) {
	var schedule *PromotionSchedule
	if _, err := schedule.BindPlan(operationplan.EffectNode{}, operationplan.EffectStructure{}, operationplan.EffectStructure{}); err == nil {
		t.Fatal("unavailable promotion schedule produced a plan")
	}
}

func TestPlanRetainsAnImmutablePromotionBinding(t *testing.T) {
	key := ownership.LogicalKey{Destination: outputtest.Parse(t, "~/config"), Footprint: outputtest.KeyFootprint(t, "model")}
	metadata := map[ownership.LogicalKey]int{key: 0}
	var builder operationplan.EffectStructureBuilder
	segment := ForwardObligation(&builder, "apply/test", operationplan.EffectStepPersistence)
	structure, err := builder.Compile(builder.ForwardPhase("apply-test", segment))
	if err != nil {
		t.Fatal(err)
	}
	failure, err := CompileFailureSettlement(false)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := newPlan(segment, structure, failure, metadata, true)
	if err != nil {
		t.Fatal(err)
	}
	metadata[key] = 9
	execution, err := Begin(&plan, plan)
	if err != nil {
		t.Fatal(err)
	}
	if reference, err := execution.PromotionReference(key); err != nil || reference != "apply/ownership-promotion/0" {
		t.Fatalf("promotion after caller mutation = %q, %v", reference, err)
	}
}
