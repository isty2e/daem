package apply

import (
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/reconcile"
)

func TestApplyFinalRoutePlanBindsExactAcceptedCommand(t *testing.T) {
	root, manifestPath, lockfilePath, missingInventory, locked, _ := writeApplyClaudePluginCarrierCommandFixture(t)
	prepared, err := PlanWrite(t.Context(), CommandInput{
		ManifestPath:         manifestPath,
		LockfilePath:         lockfilePath,
		TargetValues:         []string{"claude-code"},
		RelationObservations: &missingInventory,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = prepared.Close() })

	actions := prepared.lifecycle.planned.assessment.Reconciliation.Relations()
	if len(actions) != 1 || !actions[0].InvokesHostRoute() {
		t.Fatalf("relation actions = %#v, want one host invocation", actions)
	}
	facts, err := applyRouteScheduleFacts(
		"apply/final/route",
		prepared.lifecycle.planned.assessment.CurrentState,
		actions,
		locked,
		root,
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := compileApplyFinalRoutePlan(facts, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if facts[0].preflight.kind != applyRoutePreflightAccepted {
		t.Fatalf("preflight kind = %d, want accepted", facts[0].preflight.kind)
	}

	matched, err := plan.routeFor(actions[0])
	if err != nil {
		t.Fatal(err)
	}
	if !applyRouteScheduleFactsEqual(matched, facts[0]) {
		t.Fatal("route lookup did not retain the exact compiled command")
	}

	changedFacts, err := applyRouteScheduleFacts(
		"apply/final/route",
		prepared.lifecycle.planned.assessment.CurrentState,
		actions,
		locked,
		root+"-other",
	)
	if err != nil {
		t.Fatal(err)
	}
	changedPlan, err := compileApplyFinalRoutePlan(changedFacts, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.equal(changedPlan) {
		t.Fatal("final route plans with different command workdirs compare equal")
	}
	if err := bindApplyFinalRoutePlans(
		applyContinuationPlan{finalRoutePlan: plan},
		applyContinuationPlan{finalRoutePlan: changedPlan},
	); err == nil {
		t.Fatal("prepared/current command drift was accepted")
	}

	planned := prepared.lifecycle.planned
	applyInput, err := applyEffectInput(planned)
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := compileApplyForwardEffectSchedule(planned, nil, applyInput)
	if err != nil {
		t.Fatal(err)
	}
	reserved := schedule.finalBinding()
	reserved.routes = changedPlan
	if _, err := equivalentProviderFinalSchedule(reserved, planned, nil); err == nil {
		t.Fatal("provider replan accepted semantically different final route facts")
	}
}

func TestApplyFinalRoutePlanPreservesClosedPreflightOutcomes(t *testing.T) {
	root, manifestPath, lockfilePath, missingInventory, locked, _ := writeApplyClaudePluginCarrierCommandFixture(t)
	prepared, err := PlanWrite(t.Context(), CommandInput{
		ManifestPath:         manifestPath,
		LockfilePath:         lockfilePath,
		TargetValues:         []string{"claude-code"},
		RelationObservations: &missingInventory,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = prepared.Close() })

	action := prepared.lifecycle.planned.assessment.Reconciliation.Relations()[0]
	accepted, err := applyRouteScheduleFacts(
		"apply/final/route",
		prepared.lifecycle.planned.assessment.CurrentState,
		[]reconcile.RelationAction{action},
		locked,
		root,
	)
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := applyRouteScheduleFacts(
		"apply/final/route",
		prepared.lifecycle.planned.assessment.CurrentState,
		[]reconcile.RelationAction{action},
		locked,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if rejected[0].preflight.kind != applyRoutePreflightRejected ||
		rejected[0].preflight.rejection == nil {
		t.Fatalf("typed rejection = %#v, want retained validation error", rejected[0].preflight)
	}

	operational := accepted[0]
	operational.preflight = rejectedApplyRoutePreflight(errors.New(
		strings.Repeat("route codec failed ", maximumApplyRoutePreflightDiagnosticRunes),
	))
	if operational.preflight.kind != applyRoutePreflightOperationalFailure {
		t.Fatalf("operational kind = %d", operational.preflight.kind)
	}
	if got := len([]rune(operational.preflight.operationalDiagnostic)); got != maximumApplyRoutePreflightDiagnosticRunes {
		t.Fatalf("operational diagnostic runes = %d", got)
	}
	notApplicable := applyRouteScheduleFact{
		ref:    "apply/final/route/none",
		action: action,
		work:   operationplan.RouteWork{},
	}
	for _, facts := range [][]applyRouteScheduleFact{
		{notApplicable},
		accepted,
		rejected,
		{operational},
	} {
		if _, err := compileApplyFinalRoutePlan(facts, false, false); err != nil {
			t.Fatalf("compile preflight kind %d: %v", facts[0].preflight.kind, err)
		}
	}
}
