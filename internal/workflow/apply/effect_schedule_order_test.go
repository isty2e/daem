package apply

import (
	"testing"

	"github.com/isty2e/daem/internal/operationplan"
)

func TestApplyScheduleReferencesAreOrdinalAndByteNeutral(t *testing.T) {
	t.Parallel()
	if got := applyOrdinalScheduleReference("apply/final/route", 12); got != "apply/final/route/000012" {
		t.Fatalf("schedule reference = %q", got)
	}
}

func TestApplyScheduleSuccessfulSettlementOrder(t *testing.T) {
	t.Parallel()
	input := syntheticApplyScheduleInput(t, 1)
	input.hasGlobalRetirement = true
	input.carrierRemovals = []applyCarrierScheduleFact{{
		ref:  "carrier-removal",
		work: operationplan.CarrierWork{InvokesHost: true},
	}}
	input.finalRoutes = []applyRouteScheduleFact{{
		ref:  "final-route",
		work: operationplan.RouteWork{InvokesHost: true},
	}}
	input.orderClasses = []applyOrderScheduleFact{{
		ref:              "relation-order",
		requiresMutation: true,
	}}
	input.mayReclassifyOrder = true
	input.delegates = []applyDelegateScheduleFact{{
		ref:  "delegate",
		work: operationplan.DelegateWork{SchedulesAttempt: true},
	}}
	input.hasGlobalAdoption = true

	schedule := mustCompileSyntheticApplySchedule(t, input)
	cursor := applyScheduleTestCursor{t: t, cursor: schedule.full.Begin()}
	cursor.consume("apply/effect-segment", operationplan.EffectStepNoOp)
	cursor.consume("apply/effect-segment/forward", operationplan.EffectStepForwardEffect)
	cursor.consume("apply/effect-segment/settlement", operationplan.EffectStepPersistence)
	cursor.failFastSuccess("apply/global-claim-retirements")
	cursor.consume("carrier-removal/forward", operationplan.EffectStepForwardEffect)
	cursor.bindInitialStatefileAuthority()
	cursor.consume("carrier-removal/statefile/pending/publish", operationplan.EffectStepPublishDescendant)
	cursor.consume("carrier-removal/statefile/pre-effect/validate", operationplan.EffectStepValidateDescendant)
	cursor.consume("carrier-removal/effect", operationplan.EffectStepExternal)
	cursor.consumeRepeated("carrier-removal/statefile/post-effect/validate", operationplan.EffectStepValidateDescendant, 6)
	cursor.consumeRepeated("carrier-removal/statefile/settlement/publish", operationplan.EffectStepPublishDescendant, 2)
	cursor.failFastChoice("carrier-removal/outcome")
	cursor.consume("final-route/forward", operationplan.EffectStepForwardEffect)
	cursor.consume("final-route/statefile/ensure-validate", operationplan.EffectStepValidateDescendant)
	cursor.consume("final-route/statefile/pending/publish", operationplan.EffectStepPublishDescendant)
	cursor.consume("final-route/statefile/pre-host/validate", operationplan.EffectStepValidateDescendant)
	cursor.consume("final-route/host", operationplan.EffectStepExternal)
	cursor.consumeRepeated("final-route/statefile/post-host/validate", operationplan.EffectStepValidateDescendant, 5)
	cursor.consumeRepeated("final-route/statefile/settlement/publish", operationplan.EffectStepPublishDescendant, 3)
	cursor.selectAlternative("final-route/outcome", 0)
	cursor.consume("final-route/success", operationplan.EffectStepNoOp)
	cursor.consume("apply/relation-order/reobserve", operationplan.EffectStepObservation)
	cursor.failFastChoice("apply/relation-order/admission")
	cursor.selectAlternative("relation-order/choice", 1)
	cursor.consume("relation-order/forward", operationplan.EffectStepForwardEffect)
	cursor.consume("relation-order/external", operationplan.EffectStepExternal)
	cursor.consume("relation-order/observation", operationplan.EffectStepObservation)
	cursor.failFastChoice("relation-order/outcome")
	cursor.consume("apply/delegates/forward", operationplan.EffectStepForwardEffect)
	cursor.consume("apply/delegates/statefile/ensure-validate", operationplan.EffectStepValidateDescendant)
	cursor.consume("delegate/statefile/pre-attempt/validate", operationplan.EffectStepValidateDescendant)
	cursor.consume("delegate/attempt", operationplan.EffectStepExternal)
	cursor.consume("delegate/statefile/post-attempt/validate", operationplan.EffectStepValidateDescendant)
	cursor.selectAlternative("delegate/outcome", 0)
	cursor.consume("delegate/success", operationplan.EffectStepNoOp)
	cursor.consume("apply/delegates/statefile/pre-persistence/validate", operationplan.EffectStepValidateDescendant)
	cursor.consume("apply/delegates/statefile/persistence/publish", operationplan.EffectStepPublishDescendant)
	cursor.consume("apply/delegates/statefile/post-persistence/validate", operationplan.EffectStepValidateDescendant)
	cursor.failFastChoice("apply/delegates/persistence-outcome")
	cursor.failFastSuccess("apply/global-claim-adoptions")
	cursor.finish()
}

func TestApplyScheduleProviderBarriersPrecedeFinalSuffix(t *testing.T) {
	t.Parallel()
	input := syntheticApplyScheduleInput(t, 0)
	input.providerRoutes = []applyRouteScheduleFact{
		{ref: "provider-route", work: operationplan.RouteWork{InvokesHost: true}},
	}
	schedule := mustCompileSyntheticApplySchedule(t, input)
	cursor := applyScheduleTestCursor{t: t, cursor: schedule.full.Begin()}
	cursor.consume("apply/provider/pre-barrier", operationplan.EffectStepValidateBarrier)
	cursor.consume("provider-route/forward", operationplan.EffectStepForwardEffect)
	cursor.bindInitialStatefileAuthority()
	cursor.consume("provider-route/statefile/pending/publish", operationplan.EffectStepPublishDescendant)
	cursor.consume("provider-route/statefile/pre-host/validate", operationplan.EffectStepValidateDescendant)
	cursor.consume("provider-route/host", operationplan.EffectStepExternal)
	cursor.consumeRepeated("provider-route/statefile/post-host/validate", operationplan.EffectStepValidateDescendant, 5)
	cursor.consumeRepeated("provider-route/statefile/settlement/publish", operationplan.EffectStepPublishDescendant, 3)
	cursor.selectAlternative("provider-route/outcome", 0)
	cursor.consume("provider-route/outcome/success", operationplan.EffectStepNoOp)
	cursor.consume("apply/provider/post-barrier", operationplan.EffectStepValidateBarrier)
	cursor.consume("apply/provider/replan-barrier", operationplan.EffectStepValidateBarrier)
	cursor.consume("apply/effect-segment", operationplan.EffectStepNoOp)
	cursor.consume("apply/effect-segment/no-change", operationplan.EffectStepNoOp)
	cursor.finish()

	finalCursor := applyScheduleTestCursor{t: t, cursor: schedule.final.Begin()}
	finalCursor.consume("apply/effect-segment", operationplan.EffectStepNoOp)
	finalCursor.consume("apply/effect-segment/no-change", operationplan.EffectStepNoOp)
	finalCursor.finish()
}

func TestApplyScheduleNoChangeContinuesWithoutForwardDemand(t *testing.T) {
	t.Parallel()
	input := syntheticApplyScheduleInput(t, 0)
	schedule := mustCompileSyntheticApplySchedule(t, input)
	demand, err := schedule.full.LegacyDemand()
	if err != nil {
		t.Fatal(err)
	}
	if !demand.Empty() {
		t.Fatalf("no-change demand = %#v, want empty", demand)
	}
	cursor := applyScheduleTestCursor{t: t, cursor: schedule.full.Begin()}
	cursor.consume("apply/effect-segment", operationplan.EffectStepNoOp)
	cursor.consume("apply/effect-segment/no-change", operationplan.EffectStepNoOp)
	cursor.finish()
}

func TestApplyScheduleCarrierFailureStopsLaterSettlement(t *testing.T) {
	t.Parallel()
	input := syntheticApplyScheduleInput(t, 0)
	input.carrierRemovals = []applyCarrierScheduleFact{{
		ref:  "carrier-removal",
		work: operationplan.CarrierWork{MutatesDirect: true},
	}}
	input.hasGlobalAdoption = true
	schedule := mustCompileSyntheticApplySchedule(t, input)
	cursor := applyScheduleTestCursor{t: t, cursor: schedule.full.Begin()}
	cursor.consume("apply/effect-segment", operationplan.EffectStepNoOp)
	cursor.consume("apply/effect-segment/no-change", operationplan.EffectStepNoOp)
	cursor.consume("carrier-removal/forward", operationplan.EffectStepForwardEffect)
	cursor.bindInitialStatefileAuthority()
	cursor.consume("carrier-removal/statefile/pending/publish", operationplan.EffectStepPublishDescendant)
	cursor.consume("carrier-removal/statefile/pre-effect/validate", operationplan.EffectStepValidateDescendant)
	cursor.consume("carrier-removal/effect", operationplan.EffectStepPersistence)
	cursor.consumeRepeated("carrier-removal/statefile/post-effect/validate", operationplan.EffectStepValidateDescendant, 6)
	cursor.consumeRepeated("carrier-removal/statefile/settlement/publish", operationplan.EffectStepPublishDescendant, 2)
	cursor.selectAlternative("carrier-removal/outcome", 1)
	cursor.consume("carrier-removal/outcome/failure", operationplan.EffectStepTerminal)
	cursor.finish()
	if err := cursor.cursor.Consume(
		"apply/global-claim-adoptions/persistence",
		operationplan.EffectStepPersistence,
	); err == nil {
		t.Fatal("carrier failure admitted final global adoption")
	}
}

func TestApplyScheduleFinalRouteOrdinaryFailureContinuesBatch(t *testing.T) {
	t.Parallel()
	input := syntheticApplyScheduleInput(t, 0)
	input.finalRoutes = []applyRouteScheduleFact{
		{ref: "route-1", work: operationplan.RouteWork{InvokesHost: true}},
		{ref: "route-2", work: operationplan.RouteWork{InvokesHost: true}},
	}
	input.hasGlobalAdoption = true
	schedule := mustCompileSyntheticApplySchedule(t, input)
	cursor := applyScheduleTestCursor{t: t, cursor: schedule.full.Begin()}
	cursor.consume("apply/effect-segment", operationplan.EffectStepNoOp)
	cursor.consume("apply/effect-segment/no-change", operationplan.EffectStepNoOp)
	cursor.consume("route-1/forward", operationplan.EffectStepForwardEffect)
	cursor.bindInitialStatefileAuthority()
	cursor.consume("route-1/statefile/pending/publish", operationplan.EffectStepPublishDescendant)
	cursor.consume("route-1/statefile/pre-host/validate", operationplan.EffectStepValidateDescendant)
	cursor.consume("route-1/host", operationplan.EffectStepExternal)
	cursor.consumeRepeated("route-1/statefile/post-host/validate", operationplan.EffectStepValidateDescendant, 5)
	cursor.consumeRepeated("route-1/statefile/settlement/publish", operationplan.EffectStepPublishDescendant, 3)
	cursor.selectAlternative("route-1/outcome", 1)
	cursor.consume("route-1/ordinary", operationplan.EffectStepNoOp)
	cursor.consume("route-2/forward", operationplan.EffectStepForwardEffect)
	cursor.consume("route-2/statefile/ensure-validate", operationplan.EffectStepValidateDescendant)
	cursor.consume("route-2/statefile/pending/publish", operationplan.EffectStepPublishDescendant)
	cursor.consume("route-2/statefile/pre-host/validate", operationplan.EffectStepValidateDescendant)
	cursor.consume("route-2/host", operationplan.EffectStepExternal)
	cursor.consumeRepeated("route-2/statefile/post-host/validate", operationplan.EffectStepValidateDescendant, 5)
	cursor.consumeRepeated("route-2/statefile/settlement/publish", operationplan.EffectStepPublishDescendant, 3)
	cursor.selectAlternative("route-2/outcome", 0)
	cursor.consume("route-2/success", operationplan.EffectStepNoOp)
	cursor.consume("route-1/ordinary-failure/terminal", operationplan.EffectStepTerminal)
	cursor.finish()
	if err := cursor.cursor.Consume(
		"apply/global-claim-adoptions/persistence",
		operationplan.EffectStepPersistence,
	); err == nil {
		t.Fatal("failed host-route batch admitted final global adoption")
	}
}

func TestApplyScheduleFinalRouteStructuralFailureStopsBatch(t *testing.T) {
	t.Parallel()
	input := syntheticApplyScheduleInput(t, 0)
	input.finalRoutes = []applyRouteScheduleFact{
		{ref: "route-1", work: operationplan.RouteWork{InvokesHost: true}},
		{ref: "route-2", work: operationplan.RouteWork{InvokesHost: true}},
	}
	input.hasGlobalAdoption = true
	schedule := mustCompileSyntheticApplySchedule(t, input)
	cursor := applyScheduleTestCursor{t: t, cursor: schedule.full.Begin()}
	cursor.consume("apply/effect-segment", operationplan.EffectStepNoOp)
	cursor.consume("apply/effect-segment/no-change", operationplan.EffectStepNoOp)
	cursor.consume("route-1/forward", operationplan.EffectStepForwardEffect)
	cursor.bindInitialStatefileAuthority()
	cursor.consume("route-1/statefile/pending/publish", operationplan.EffectStepPublishDescendant)
	cursor.consume("route-1/statefile/pre-host/validate", operationplan.EffectStepValidateDescendant)
	cursor.consume("route-1/host", operationplan.EffectStepExternal)
	cursor.consumeRepeated("route-1/statefile/post-host/validate", operationplan.EffectStepValidateDescendant, 5)
	cursor.consumeRepeated("route-1/statefile/settlement/publish", operationplan.EffectStepPublishDescendant, 3)
	cursor.selectAlternative("route-1/outcome", 2)
	cursor.consume("route-1/terminal", operationplan.EffectStepTerminal)
	cursor.finish()
	if err := cursor.cursor.Consume("route-2/forward", operationplan.EffectStepForwardEffect); err == nil {
		t.Fatal("structural host-route failure admitted a later route")
	}
}

func TestApplyScheduleDelegateOrdinaryFailurePersistsBeforeTerminal(t *testing.T) {
	t.Parallel()
	input := syntheticApplyScheduleInput(t, 0)
	input.delegates = []applyDelegateScheduleFact{
		{ref: "delegate-1", work: operationplan.DelegateWork{SchedulesAttempt: true}},
		{ref: "delegate-2", work: operationplan.DelegateWork{SchedulesAttempt: true}},
	}
	input.hasGlobalAdoption = true
	schedule := mustCompileSyntheticApplySchedule(t, input)
	cursor := applyScheduleTestCursor{t: t, cursor: schedule.full.Begin()}
	cursor.consume("apply/effect-segment", operationplan.EffectStepNoOp)
	cursor.consume("apply/effect-segment/no-change", operationplan.EffectStepNoOp)
	cursor.consume("apply/delegates/forward", operationplan.EffectStepForwardEffect)
	cursor.bindInitialStatefileAuthority()
	cursor.consume("delegate-1/statefile/pre-attempt/validate", operationplan.EffectStepValidateDescendant)
	cursor.consume("delegate-1/attempt", operationplan.EffectStepExternal)
	cursor.consume("delegate-1/statefile/post-attempt/validate", operationplan.EffectStepValidateDescendant)
	cursor.selectAlternative("delegate-1/outcome", 1)
	cursor.consume("delegate-1/ordinary", operationplan.EffectStepNoOp)
	cursor.consume("delegate-2/statefile/pre-attempt/validate", operationplan.EffectStepValidateDescendant)
	cursor.consume("delegate-2/attempt", operationplan.EffectStepExternal)
	cursor.consume("delegate-2/statefile/post-attempt/validate", operationplan.EffectStepValidateDescendant)
	cursor.selectAlternative("delegate-2/outcome", 0)
	cursor.consume("delegate-2/success", operationplan.EffectStepNoOp)
	cursor.consume("apply/delegates/statefile/pre-persistence/validate", operationplan.EffectStepValidateDescendant)
	cursor.consume("apply/delegates/statefile/persistence/publish", operationplan.EffectStepPublishDescendant)
	cursor.consume("apply/delegates/statefile/post-persistence/validate", operationplan.EffectStepValidateDescendant)
	cursor.failFastChoice("apply/delegates/persistence-outcome")
	cursor.consume("delegate-1/ordinary-failure/terminal", operationplan.EffectStepTerminal)
	cursor.finish()
}

func TestApplyScheduleDelegateStructuralFailureStopsBatch(t *testing.T) {
	t.Parallel()
	input := syntheticApplyScheduleInput(t, 0)
	input.delegates = []applyDelegateScheduleFact{
		{ref: "delegate-1", work: operationplan.DelegateWork{SchedulesAttempt: true}},
		{ref: "delegate-2", work: operationplan.DelegateWork{SchedulesAttempt: true}},
	}
	schedule := mustCompileSyntheticApplySchedule(t, input)
	cursor := applyScheduleTestCursor{t: t, cursor: schedule.full.Begin()}
	cursor.consume("apply/effect-segment", operationplan.EffectStepNoOp)
	cursor.consume("apply/effect-segment/no-change", operationplan.EffectStepNoOp)
	cursor.consume("apply/delegates/forward", operationplan.EffectStepForwardEffect)
	cursor.bindInitialStatefileAuthority()
	cursor.consume("delegate-1/statefile/pre-attempt/validate", operationplan.EffectStepValidateDescendant)
	cursor.consume("delegate-1/attempt", operationplan.EffectStepExternal)
	cursor.consume("delegate-1/statefile/post-attempt/validate", operationplan.EffectStepValidateDescendant)
	cursor.selectAlternative("delegate-1/outcome", 2)
	cursor.consume("delegate-1/terminal", operationplan.EffectStepTerminal)
	cursor.finish()
	if err := cursor.cursor.Consume("delegate-2/attempt", operationplan.EffectStepExternal); err == nil {
		t.Fatal("structural delegate failure admitted a later delegate")
	}
}

func TestApplyScheduleRelationOrderExactClassIsNoOp(t *testing.T) {
	t.Parallel()
	input := syntheticApplyScheduleInput(t, 0)
	input.orderClasses = []applyOrderScheduleFact{{ref: "relation-order"}}
	schedule := mustCompileSyntheticApplySchedule(t, input)
	cursor := applyScheduleTestCursor{t: t, cursor: schedule.full.Begin()}
	cursor.consume("apply/effect-segment", operationplan.EffectStepNoOp)
	cursor.consume("apply/effect-segment/no-change", operationplan.EffectStepNoOp)
	cursor.consume("apply/relation-order/reobserve", operationplan.EffectStepObservation)
	cursor.failFastChoice("apply/relation-order/admission")
	cursor.consume("relation-order/noop", operationplan.EffectStepNoOp)
	cursor.finish()
}

func TestApplyScheduleUsesOneDescendantBindingAcrossProviderAndFinal(t *testing.T) {
	t.Parallel()
	input := syntheticApplyScheduleInput(t, 0)
	input.providerRoutes = []applyRouteScheduleFact{{
		ref:  "provider-route",
		work: operationplan.RouteWork{InvokesHost: true},
	}}
	input.finalRoutes = []applyRouteScheduleFact{{
		ref:  "final-route",
		work: operationplan.RouteWork{InvokesHost: true},
	}}
	schedule := mustCompileSyntheticApplySchedule(t, input)
	fullDemand, err := schedule.full.LegacyDemand()
	if err != nil {
		t.Fatal(err)
	}
	if fullDemand.DescendantBindings() != 1 {
		t.Fatalf("full descendant bindings = %d, want 1", fullDemand.DescendantBindings())
	}
	finalDemand, err := schedule.final.LegacyDemand()
	if err != nil {
		t.Fatal(err)
	}
	if finalDemand.DescendantBindings() != 0 {
		t.Fatalf("reserved final descendant bindings = %d, want provider-bound authority", finalDemand.DescendantBindings())
	}
	alternatives, err := schedule.full.DemandAlternatives()
	if err != nil {
		t.Fatal(err)
	}
	for index, alternative := range alternatives {
		if alternative.DescendantBindings() > 1 {
			t.Fatalf(
				"alternative[%d] descendant bindings = %d, want at most 1",
				index,
				alternative.DescendantBindings(),
			)
		}
	}
}

func syntheticApplyScheduleInput(t *testing.T, executeGates int) applyScheduleInput {
	t.Helper()
	var builder operationplan.EffectStructureBuilder
	input := applyScheduleInput{coreChanged: executeGates != 0}
	if executeGates != 0 {
		input.core = operationplan.EffectSequence(
			builder.Step("apply/effect-segment/forward", operationplan.EffectStepForwardEffect),
			builder.Step("apply/effect-segment/settlement", operationplan.EffectStepPersistence),
		)
	}
	return input
}

func mustCompileSyntheticApplySchedule(
	t *testing.T,
	input applyScheduleInput,
) applyForwardEffectSchedule {
	t.Helper()
	work := operationplan.ApplyWork{
		ExecuteGates:  0,
		StatefilePath: "state.json",
	}
	if input.coreChanged {
		work.ExecuteGates = 1
	}
	for _, route := range input.providerRoutes {
		work.ProviderActions = append(work.ProviderActions, route.work)
	}
	for _, route := range input.finalRoutes {
		work.FinalRoutes = append(work.FinalRoutes, route.work)
	}
	for _, removal := range input.carrierRemovals {
		work.CarrierRemovals = append(work.CarrierRemovals, removal.work)
	}
	for _, class := range input.orderClasses {
		work.OrderClasses = append(work.OrderClasses, operationplan.OrderClassWork{
			RequiresMutation: class.requiresMutation,
		})
	}
	for _, action := range input.delegates {
		work.Delegates = append(work.Delegates, action.work)
	}
	envelope, err := operationplan.CompileApply(work)
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := compileApplySchedule(input, envelope.Demand())
	if err != nil {
		t.Fatal(err)
	}
	return schedule
}

type applyScheduleTestCursor struct {
	t      *testing.T
	cursor *operationplan.EffectCursor
}

func (test applyScheduleTestCursor) consume(id string, kind operationplan.EffectStepKind) {
	test.t.Helper()
	if err := test.cursor.Consume(id, kind); err != nil {
		test.t.Fatalf("consume %q/%d: %v", id, kind, err)
	}
}

func (test applyScheduleTestCursor) consumeRepeated(
	id string,
	kind operationplan.EffectStepKind,
	count int,
) {
	test.t.Helper()
	for range count {
		test.consume(id, kind)
	}
}

func (test applyScheduleTestCursor) bindInitialStatefileAuthority() {
	test.t.Helper()
	test.selectAlternative("apply/statefile/initial-authority", 0)
	test.consume("apply/statefile/bind", operationplan.EffectStepBindDescendant)
}

func (test applyScheduleTestCursor) selectAlternative(id string, index int) {
	test.t.Helper()
	if err := test.cursor.SelectAlternative(id, index); err != nil {
		test.t.Fatalf("select %q[%d]: %v", id, index, err)
	}
}

func (test applyScheduleTestCursor) failFastChoice(ref string) {
	test.t.Helper()
	test.selectAlternative(ref, 0)
	test.consume(ref+"/success", operationplan.EffectStepNoOp)
}

func (test applyScheduleTestCursor) failFastSuccess(ref string) {
	test.t.Helper()
	test.consume(ref+"/persistence", operationplan.EffectStepPersistence)
	test.failFastChoice(ref + "/outcome")
}

func (test applyScheduleTestCursor) finish() {
	test.t.Helper()
	if err := test.cursor.FinishSuccess(); err != nil {
		test.t.Fatal(err)
	}
}
