package apply

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/target"
)

func TestApplyScheduleReferencesAreOrdinalAndByteNeutral(t *testing.T) {
	t.Parallel()
	if got := applyOrdinalScheduleReference("apply/final/route", 12); got != "apply/final/route/000012" {
		t.Fatalf("schedule reference = %q", got)
	}
}

func TestApplyScheduleRejectsMissingOrDuplicateFactReferences(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		input applyScheduleInput
		want  string
	}{
		{
			name: "missing",
			input: applyScheduleInput{finalRoutes: []applyRouteScheduleFact{{
				work: operationplan.RouteWork{InvokesHost: true},
			}}},
			want: "is not canonical",
		},
		{
			name: "duplicate",
			input: applyScheduleInput{
				providerRoutes: []applyRouteScheduleFact{{
					ref:  "shared-route",
					work: operationplan.RouteWork{InvokesHost: true},
				}},
				finalRoutes: []applyRouteScheduleFact{{
					ref:  "shared-route",
					work: operationplan.RouteWork{InvokesHost: true},
				}},
			},
			want: "is shared",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := compileApplySchedule(test.input, operationplan.Demand{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("compileApplySchedule error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestApplyContinuationExecutionUsesCurrentFactsAfterStructuralParity(t *testing.T) {
	preparedAction := newWorkflowFixture(t, target.ScopeProject).action
	currentAction := newWorkflowFixture(t, target.ScopeProject).action

	preparedInput := syntheticApplyScheduleInput(t, 0)
	preparedInput.carrierRemovals = []applyCarrierScheduleFact{{
		ref:    "carrier-removal",
		action: preparedAction,
		work:   operationplan.CarrierWork{InvokesHost: true},
		mode:   applyCarrierScheduleHostRoute,
		scope:  target.ScopeProject,
	}}
	currentInput := syntheticApplyScheduleInput(t, 0)
	currentInput.carrierRemovals = []applyCarrierScheduleFact{{
		ref:    "carrier-removal",
		action: currentAction,
		work:   operationplan.CarrierWork{InvokesHost: true},
		mode:   applyCarrierScheduleHostRoute,
		scope:  target.ScopeProject,
	}}
	prepared := mustCompileSyntheticApplySchedule(t, preparedInput)
	current := mustCompileSyntheticApplySchedule(t, currentInput)

	execution, err := newApplyContinuationExecution(prepared.continuation, current.continuation)
	if err != nil {
		t.Fatal(err)
	}
	if ref, err := execution.carrierRemovalReference(currentAction); err != nil || ref != "carrier-removal" {
		t.Fatalf("current carrier removal reference = (%q, %v), want current plan fact", ref, err)
	}
	if _, err := execution.carrierRemovalReference(preparedAction); err == nil {
		t.Fatal("prepared semantic fact remained authoritative after current-plan binding")
	}
}

func TestApplyContinuationExecutionRejectsUnavailableOrDifferentPlans(t *testing.T) {
	t.Parallel()

	base := mustCompileSyntheticApplySchedule(t, syntheticApplyScheduleInput(t, 0))
	differentInput := syntheticApplyScheduleInput(t, 0)
	differentInput.hasGlobalAdoption = true
	different := mustCompileSyntheticApplySchedule(t, differentInput)

	for _, test := range []struct {
		name     string
		prepared applyContinuationPlan
		current  applyContinuationPlan
	}{
		{name: "unavailable prepared", current: base.continuation},
		{name: "unavailable current", prepared: base.continuation},
		{name: "different structure", prepared: base.continuation, current: different.continuation},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := newApplyContinuationExecution(test.prepared, test.current); err == nil {
				t.Fatal("newApplyContinuationExecution accepted an invalid plan pair")
			}
		})
	}
}

func TestApplyContinuationExecutionAcceptsEmptyContinuation(t *testing.T) {
	t.Parallel()

	schedule := mustCompileSyntheticApplySchedule(t, syntheticApplyScheduleInput(t, 0))
	execution, err := newApplyContinuationExecution(
		schedule.continuation,
		schedule.continuation,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := execution.finish(nil); err != nil {
		t.Fatalf("finish empty continuation: %v", err)
	}
}

func TestScheduledContinuationCleanupReportsWhetherCleanupRan(t *testing.T) {
	t.Parallel()

	var builder operationplan.EffectStructureBuilder
	structure, err := builder.Compile(builder.Step("other-cleanup", operationplan.EffectStepCleanup))
	if err != nil {
		t.Fatal(err)
	}
	plan := applyContinuationPlan{structure: structure, available: true}
	execution, err := newApplyContinuationExecution(plan, plan)
	if err != nil {
		t.Fatal(err)
	}
	cleanupCalls := 0
	attempted, err := scheduledContinuationCleanup(execution, "expected-cleanup", func() error {
		cleanupCalls++
		return nil
	})
	if err == nil {
		t.Fatal("scheduled cleanup accepted the wrong cursor step")
	}
	if attempted || cleanupCalls != 0 {
		t.Fatalf("cleanup attempted/calls = %t/%d, want false/0", attempted, cleanupCalls)
	}
}

func TestApplyScheduleSuccessfulSettlementOrder(t *testing.T) {
	t.Parallel()
	input := syntheticApplyScheduleInput(t, 1)
	input.hasGlobalRetirement = true
	input.carrierRemovals = []applyCarrierScheduleFact{{
		ref:  "carrier-removal",
		work: operationplan.CarrierWork{InvokesHost: true},
		mode: applyCarrierScheduleHostRoute,
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
	cursor.checkedSuccess("apply/global-claim-retirements/pre-registry", operationplan.EffectStepForwardEffect)
	cursor.checkedSuccess("apply/global-claim-retirements/persistence", operationplan.EffectStepPersistence)
	cursor.checkedSuccess("apply/global-claim-retirements/post-registry", operationplan.EffectStepForwardEffect)

	cursor.consume("carrier-removal/preflight", operationplan.EffectStepObservation)
	cursor.selectAlternative("carrier-removal/preflight-outcome", 1)
	cursor.consume("carrier-removal/binding", operationplan.EffectStepObservation)
	cursor.selectAlternative("carrier-removal/binding-outcome", 1)
	cursor.checkedSuccess("carrier-removal/prepared/baselines", operationplan.EffectStepObservation)
	cursor.checkedSuccess("carrier-removal/prepared/forward", operationplan.EffectStepForwardEffect)
	cursor.checkedInitialStatefileAuthority("carrier-removal/prepared/statefile")
	cursor.checkedSuccess("carrier-removal/prepared/statefile/pending/publish", operationplan.EffectStepPublishDescendant)
	cursor.checkedSuccess("carrier-removal/prepared/context-before-host", operationplan.EffectStepObservation)
	cursor.checkedSuccess("carrier-removal/prepared/statefile/pre-host/validate", operationplan.EffectStepValidateDescendant)
	cursor.checkedSuccess("carrier-removal/prepared/host", operationplan.EffectStepExternal)
	cursor.consume("carrier-removal/prepared/statefile/post-host/validate", operationplan.EffectStepValidateDescendant)
	cursor.selectAlternative("carrier-removal/prepared/post-host-outcome", 0)
	cursor.consume("carrier-removal/prepared/post-host-success", operationplan.EffectStepNoOp)
	cursor.checkedSuccess("carrier-removal/prepared/classify", operationplan.EffectStepObservation)
	cursor.checkedSuccess("carrier-removal/prepared/attempt/pre-persistence/validate", operationplan.EffectStepValidateDescendant)
	cursor.checkedSuccess("carrier-removal/prepared/attempt/persistence/publish", operationplan.EffectStepPublishDescendant)
	cursor.checkedSuccess("carrier-removal/prepared/attempt/post-persistence/validate", operationplan.EffectStepValidateDescendant)
	cursor.selectAlternative("carrier-removal/prepared/attempt-outcome", 0)
	cursor.checkedSuccess("carrier-removal/statefile/pre-retirement/validate", operationplan.EffectStepValidateDescendant)
	cursor.checkedSuccess("carrier-removal/statefile/retirement/publish", operationplan.EffectStepPublishDescendant)
	cursor.checkedSuccess("carrier-removal/statefile/post-retirement/validate", operationplan.EffectStepValidateDescendant)

	cursor.consume("final-route/preflight", operationplan.EffectStepObservation)
	cursor.selectAlternative("final-route/preflight-outcome", 0)
	cursor.consume("final-route/preflight-accepted", operationplan.EffectStepNoOp)
	cursor.checkedSuccess("apply/final-routes/initial-project-root", operationplan.EffectStepObservation)
	cursor.checkedSuccess("final-route/forward", operationplan.EffectStepForwardEffect)
	cursor.checkedExistingStatefileAuthority("final-route/statefile")
	cursor.consume("final-route/binding", operationplan.EffectStepObservation)
	cursor.selectAlternative("final-route/binding-outcome", 1)
	cursor.checkedSuccess("final-route/statefile/pending/publish", operationplan.EffectStepPublishDescendant)
	cursor.checkedSuccess("final-route/context-before-host", operationplan.EffectStepObservation)
	cursor.checkedSuccess("final-route/statefile/pre-host/validate", operationplan.EffectStepValidateDescendant)
	cursor.consume("final-route/host", operationplan.EffectStepExternal)
	cursor.consume("final-route/statefile/post-host/validate", operationplan.EffectStepValidateDescendant)
	cursor.selectAlternative("final-route/post-host-outcome", 0)
	cursor.consume("final-route/post-host-success", operationplan.EffectStepNoOp)
	cursor.consume("final-route/post-host-observation", operationplan.EffectStepObservation)
	cursor.selectAlternative("final-route/classification", 0)
	cursor.checkedSuccess("final-route/project-root-before-settlement", operationplan.EffectStepObservation)
	cursor.selectAlternative("final-route/claim-promotion", 0)
	cursor.consume("final-route/claim-promotion-none", operationplan.EffectStepNoOp)
	cursor.checkedSuccess("final-route/statefile/pre-retirement/validate", operationplan.EffectStepValidateDescendant)
	cursor.checkedSuccess("final-route/statefile/retirement/publish", operationplan.EffectStepPublishDescendant)
	cursor.checkedSuccess("final-route/attempt-record", operationplan.EffectStepObservation)
	cursor.checkedSuccess("final-route/statefile/pre-attempt/validate", operationplan.EffectStepValidateDescendant)
	cursor.checkedSuccess("final-route/statefile/attempt/publish", operationplan.EffectStepPublishDescendant)
	cursor.checkedSuccess("final-route/statefile/post-attempt/validate", operationplan.EffectStepValidateDescendant)
	cursor.checkedSuccess("final-route/project-root-after-settlement", operationplan.EffectStepObservation)
	cursor.checkedSuccess("final-route/binding-release", operationplan.EffectStepCleanup)
	cursor.checkedSuccess("final-route/declarations-current", operationplan.EffectStepObservation)
	cursor.selectAlternative("apply/final-routes/outcome", 0)
	cursor.consume("apply/final-routes/success", operationplan.EffectStepNoOp)

	cursor.consume("apply/relation-order/reobserve", operationplan.EffectStepObservation)
	cursor.failFastChoice("apply/relation-order/admission")
	cursor.selectAlternative("relation-order/choice", 1)
	cursor.checkedSuccess("relation-order/forward", operationplan.EffectStepForwardEffect)
	cursor.checkedSuccess("relation-order/binding", operationplan.EffectStepObservation)
	cursor.consume("relation-order/external", operationplan.EffectStepExternal)
	cursor.checkedSuccess("relation-order/settlement", operationplan.EffectStepObservation)

	cursor.consume("apply/delegates/admission", operationplan.EffectStepObservation)
	cursor.optional("apply/delegates/forward", operationplan.EffectStepForwardEffect)
	cursor.optionalExistingStatefileAuthority("apply/delegates/statefile")
	cursor.optional("delegate/declarations-before", operationplan.EffectStepObservation)
	cursor.optional("delegate/statefile/pre-attempt/validate", operationplan.EffectStepValidateDescendant)
	cursor.optional("delegate/attempt", operationplan.EffectStepExternal)
	cursor.optional("delegate/statefile/post-attempt/validate", operationplan.EffectStepValidateDescendant)
	cursor.optional("delegate/declarations-after", operationplan.EffectStepObservation)
	cursor.selectAlternative("delegate/outcome", 0)
	cursor.consume("delegate/success", operationplan.EffectStepNoOp)
	cursor.consume("apply/delegates/result", operationplan.EffectStepObservation)
	cursor.optional("apply/delegates/statefile/pre-persistence/validate", operationplan.EffectStepValidateDescendant)
	cursor.optional("apply/delegates/statefile/persistence/publish", operationplan.EffectStepPublishDescendant)
	cursor.optional("apply/delegates/statefile/post-persistence/validate", operationplan.EffectStepValidateDescendant)
	cursor.optional("apply/delegates/project-root", operationplan.EffectStepObservation)
	cursor.failFastChoice("apply/delegates/outcome")
	cursor.checkedSuccess("apply/global-claim-adoptions/pre-registry", operationplan.EffectStepForwardEffect)
	cursor.checkedSuccess("apply/global-claim-adoptions/persistence", operationplan.EffectStepPersistence)
	cursor.checkedSuccess("apply/global-claim-adoptions/post-registry", operationplan.EffectStepForwardEffect)
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
	cursor.bindInitialStatefileAuthority("provider-route/statefile")
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
		mode: applyCarrierScheduleDirectProjection,
	}}
	input.hasGlobalAdoption = true
	schedule := mustCompileSyntheticApplySchedule(t, input)
	cursor := applyScheduleTestCursor{t: t, cursor: schedule.full.Begin()}
	cursor.consume("apply/effect-segment", operationplan.EffectStepNoOp)
	cursor.consume("apply/effect-segment/no-change", operationplan.EffectStepNoOp)
	cursor.checkedSuccess("carrier-removal/prepare-direct", operationplan.EffectStepObservation)
	cursor.checkedSuccess("carrier-removal/forward", operationplan.EffectStepForwardEffect)
	cursor.checkedInitialStatefileAuthority("carrier-removal/statefile")
	cursor.checkedSuccess("carrier-removal/statefile/pending/publish", operationplan.EffectStepPublishDescendant)
	cursor.checkedSuccess("carrier-removal/statefile/pre-effect/validate", operationplan.EffectStepValidateDescendant)
	cursor.checkedFailure("carrier-removal/effect", operationplan.EffectStepPersistence)
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
	cursor.consume("route-1/preflight", operationplan.EffectStepObservation)
	cursor.selectAlternative("route-1/preflight-outcome", 0)
	cursor.consume("route-1/preflight-accepted", operationplan.EffectStepNoOp)
	cursor.consume("route-2/preflight", operationplan.EffectStepObservation)
	cursor.selectAlternative("route-2/preflight-outcome", 0)
	cursor.consume("route-2/preflight-accepted", operationplan.EffectStepNoOp)
	cursor.checkedSuccess("apply/final-routes/initial-project-root", operationplan.EffectStepObservation)
	cursor.consumePreparedFinalRoute("route-1", true)
	cursor.consumePreparedFinalRoute("route-2", false)
	cursor.selectAlternative("apply/final-routes/outcome", 1)
	cursor.consume("apply/final-routes/failure", operationplan.EffectStepTerminal)
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
	cursor.consume("route-1/preflight", operationplan.EffectStepObservation)
	cursor.selectAlternative("route-1/preflight-outcome", 0)
	cursor.consume("route-1/preflight-accepted", operationplan.EffectStepNoOp)
	cursor.consume("route-2/preflight", operationplan.EffectStepObservation)
	cursor.selectAlternative("route-2/preflight-outcome", 0)
	cursor.consume("route-2/preflight-accepted", operationplan.EffectStepNoOp)
	cursor.checkedSuccess("apply/final-routes/initial-project-root", operationplan.EffectStepObservation)
	cursor.checkedFailure("route-1/forward", operationplan.EffectStepForwardEffect)
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
	cursor.consume("apply/delegates/admission", operationplan.EffectStepObservation)
	cursor.optional("apply/delegates/forward", operationplan.EffectStepForwardEffect)
	cursor.optionalInitialStatefileAuthority("apply/delegates/statefile")
	cursor.optional("delegate-1/declarations-before", operationplan.EffectStepObservation)
	cursor.optional("delegate-1/statefile/pre-attempt/validate", operationplan.EffectStepValidateDescendant)
	cursor.optional("delegate-1/attempt", operationplan.EffectStepExternal)
	cursor.optional("delegate-1/statefile/post-attempt/validate", operationplan.EffectStepValidateDescendant)
	cursor.optional("delegate-1/declarations-after", operationplan.EffectStepObservation)
	cursor.selectAlternative("delegate-1/outcome", 1)
	cursor.consume("delegate-1/ordinary", operationplan.EffectStepNoOp)
	cursor.optional("delegate-2/declarations-before", operationplan.EffectStepObservation)
	cursor.optional("delegate-2/statefile/pre-attempt/validate", operationplan.EffectStepValidateDescendant)
	cursor.optional("delegate-2/attempt", operationplan.EffectStepExternal)
	cursor.optional("delegate-2/statefile/post-attempt/validate", operationplan.EffectStepValidateDescendant)
	cursor.optional("delegate-2/declarations-after", operationplan.EffectStepObservation)
	cursor.selectAlternative("delegate-2/outcome", 0)
	cursor.consume("delegate-2/success", operationplan.EffectStepNoOp)
	cursor.consume("apply/delegates/result", operationplan.EffectStepObservation)
	cursor.optional("apply/delegates/statefile/pre-persistence/validate", operationplan.EffectStepValidateDescendant)
	cursor.optional("apply/delegates/statefile/persistence/publish", operationplan.EffectStepPublishDescendant)
	cursor.optional("apply/delegates/statefile/post-persistence/validate", operationplan.EffectStepValidateDescendant)
	cursor.optional("apply/delegates/project-root", operationplan.EffectStepObservation)
	cursor.selectAlternative("apply/delegates/outcome", 1)
	cursor.consume("apply/delegates/outcome/failure", operationplan.EffectStepTerminal)
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
	cursor.consume("apply/delegates/admission", operationplan.EffectStepObservation)
	cursor.optional("apply/delegates/forward", operationplan.EffectStepForwardEffect)
	cursor.optionalInitialStatefileAuthority("apply/delegates/statefile")
	cursor.optional("delegate-1/declarations-before", operationplan.EffectStepObservation)
	cursor.optionalSkip("delegate-1/statefile/pre-attempt/validate")
	cursor.optionalSkip("delegate-1/attempt")
	cursor.optionalSkip("delegate-1/statefile/post-attempt/validate")
	cursor.optionalSkip("delegate-1/declarations-after")
	cursor.selectAlternative("delegate-1/outcome", 2)
	cursor.consume("delegate-1/skipped", operationplan.EffectStepNoOp)
	cursor.optionalSkip("delegate-2/declarations-before")
	cursor.optionalSkip("delegate-2/statefile/pre-attempt/validate")
	cursor.optionalSkip("delegate-2/attempt")
	cursor.optionalSkip("delegate-2/statefile/post-attempt/validate")
	cursor.optionalSkip("delegate-2/declarations-after")
	cursor.selectAlternative("delegate-2/outcome", 2)
	cursor.consume("delegate-2/skipped", operationplan.EffectStepNoOp)
	cursor.consume("apply/delegates/result", operationplan.EffectStepObservation)
	cursor.optionalSkip("apply/delegates/statefile/pre-persistence/validate")
	cursor.optionalSkip("apply/delegates/statefile/persistence/publish")
	cursor.optionalSkip("apply/delegates/statefile/post-persistence/validate")
	cursor.optionalSkip("apply/delegates/project-root")
	cursor.selectAlternative("apply/delegates/outcome", 1)
	cursor.consume("apply/delegates/outcome/failure", operationplan.EffectStepTerminal)
	cursor.finish()
	if err := cursor.cursor.Consume("delegate-2/attempt", operationplan.EffectStepExternal); err == nil {
		t.Fatal("structural delegate failure admitted a later delegate")
	}
}

func TestApplyScheduleSupportsMoreThanTriggerLimitDelegates(t *testing.T) {
	t.Parallel()
	input := syntheticApplyScheduleInput(t, 0)
	for index := range 65 {
		input.delegates = append(input.delegates, applyDelegateScheduleFact{
			ref:  applyOrdinalScheduleReference("delegate", index),
			work: operationplan.DelegateWork{SchedulesAttempt: true},
		})
	}
	schedule := mustCompileSyntheticApplySchedule(t, input)
	if _, err := schedule.full.DemandAlternatives(); err != nil {
		t.Fatalf("delegate schedule demand alternatives: %v", err)
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
		ExecuteGates:            0,
		GlobalCarrierRetirement: input.hasGlobalRetirement,
		GlobalCarrierAdoption:   input.hasGlobalAdoption,
		StatefilePath:           "state.json",
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

func (test applyScheduleTestCursor) optional(
	ref string,
	kind operationplan.EffectStepKind,
) {
	test.t.Helper()
	test.selectAlternative(ref+"/execution", 0)
	test.consume(ref, kind)
}

func (test applyScheduleTestCursor) optionalSkip(ref string) {
	test.t.Helper()
	test.selectAlternative(ref+"/execution", 1)
	test.consume(ref+"/skipped", operationplan.EffectStepNoOp)
}

func (test applyScheduleTestCursor) optionalInitialStatefileAuthority(prefix string) {
	test.t.Helper()
	test.selectAlternative(prefix+"/execution", 0)
	test.bindInitialStatefileAuthority(prefix)
}

func (test applyScheduleTestCursor) optionalExistingStatefileAuthority(prefix string) {
	test.t.Helper()
	test.selectAlternative(prefix+"/execution", 0)
	test.consume(prefix+"/ensure-validate", operationplan.EffectStepValidateDescendant)
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

func (test applyScheduleTestCursor) bindInitialStatefileAuthority(prefix string) {
	test.t.Helper()
	test.selectAlternative(prefix+"/initial-authority", 0)
	test.consume(prefix+"/bind", operationplan.EffectStepBindDescendant)
}

func (test applyScheduleTestCursor) checkedSuccess(
	ref string,
	kind operationplan.EffectStepKind,
) {
	test.t.Helper()
	test.consume(ref, kind)
	test.failFastChoice(ref + "/outcome")
}

func (test applyScheduleTestCursor) checkedFailure(
	ref string,
	kind operationplan.EffectStepKind,
) {
	test.t.Helper()
	test.consume(ref, kind)
	test.selectAlternative(ref+"/outcome", 1)
	test.consume(ref+"/outcome/failure", operationplan.EffectStepTerminal)
}

func (test applyScheduleTestCursor) checkedInitialStatefileAuthority(prefix string) {
	test.t.Helper()
	test.selectAlternative(prefix+"/initial-authority", 0)
	test.consume(prefix+"/bind", operationplan.EffectStepBindDescendant)
	test.failFastChoice(prefix + "/ensure-outcome")
}

func (test applyScheduleTestCursor) checkedExistingStatefileAuthority(prefix string) {
	test.t.Helper()
	test.consume(prefix+"/ensure-validate", operationplan.EffectStepValidateDescendant)
	test.failFastChoice(prefix + "/ensure-outcome")
}

func (test applyScheduleTestCursor) consumePreparedFinalRoute(ref string, initialStatefile bool) {
	test.t.Helper()
	test.checkedSuccess(ref+"/forward", operationplan.EffectStepForwardEffect)
	if initialStatefile {
		test.checkedInitialStatefileAuthority(ref + "/statefile")
	} else {
		test.checkedExistingStatefileAuthority(ref + "/statefile")
	}
	test.consume(ref+"/binding", operationplan.EffectStepObservation)
	test.selectAlternative(ref+"/binding-outcome", 1)
	test.checkedSuccess(ref+"/statefile/pending/publish", operationplan.EffectStepPublishDescendant)
	test.checkedSuccess(ref+"/context-before-host", operationplan.EffectStepObservation)
	test.checkedSuccess(ref+"/statefile/pre-host/validate", operationplan.EffectStepValidateDescendant)
	test.consume(ref+"/host", operationplan.EffectStepExternal)
	test.consume(ref+"/statefile/post-host/validate", operationplan.EffectStepValidateDescendant)
	test.selectAlternative(ref+"/post-host-outcome", 0)
	test.consume(ref+"/post-host-success", operationplan.EffectStepNoOp)
	test.consume(ref+"/post-host-observation", operationplan.EffectStepObservation)
	test.selectAlternative(ref+"/classification", 0)
	test.checkedSuccess(ref+"/project-root-before-settlement", operationplan.EffectStepObservation)
	test.selectAlternative(ref+"/claim-promotion", 0)
	test.consume(ref+"/claim-promotion-none", operationplan.EffectStepNoOp)
	test.checkedSuccess(ref+"/statefile/pre-retirement/validate", operationplan.EffectStepValidateDescendant)
	test.checkedSuccess(ref+"/statefile/retirement/publish", operationplan.EffectStepPublishDescendant)
	test.checkedSuccess(ref+"/attempt-record", operationplan.EffectStepObservation)
	test.checkedSuccess(ref+"/statefile/pre-attempt/validate", operationplan.EffectStepValidateDescendant)
	test.checkedSuccess(ref+"/statefile/attempt/publish", operationplan.EffectStepPublishDescendant)
	test.checkedSuccess(ref+"/statefile/post-attempt/validate", operationplan.EffectStepValidateDescendant)
	test.checkedSuccess(ref+"/project-root-after-settlement", operationplan.EffectStepObservation)
	test.checkedSuccess(ref+"/binding-release", operationplan.EffectStepCleanup)
	test.checkedSuccess(ref+"/declarations-current", operationplan.EffectStepObservation)
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
