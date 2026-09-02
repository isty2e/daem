package execute

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/operationplan"
)

func TestApplyEffectExecutionRejectsDifferentPreparedPlan(t *testing.T) {
	fixture := newApplyEventFixture(t)
	create := fixture.createAction("create", "CREATE.md", "created\n")
	update := fixture.updateAction("update", "UPDATE.md", "old\n", "new\n")

	prepared, err := PrepareApplyEffectPlan(fixture.input([]applyEventAction{create}))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := PrepareApplyEffectPlan(fixture.input([]applyEventAction{create, update}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newApplyEffectExecution(prepared, expected); err == nil {
		t.Fatal("newApplyEffectExecution returned nil error for different plans")
	}
}

func TestApplyEffectExecutionUsesCurrentPromotionMetadataAfterStructuralParity(t *testing.T) {
	t.Parallel()

	preparedKey := ownershipOutputKey{contentPath: "/prepared"}
	currentKey := ownershipOutputKey{contentPath: "/current"}
	var builder operationplan.EffectStructureBuilder
	segment := operationplan.EffectSequence(
		applyCheckedStep(
			&builder,
			"apply/ownership-promotion/0/observation",
			operationplan.EffectStepObservation,
		),
		builder.Choice(
			"apply/ownership-promotion/0/choice",
			builder.Step("apply/ownership-promotion/0/noop", operationplan.EffectStepNoOp),
			applyForwardObligation(
				&builder,
				"apply/ownership-promotion/0/journal",
				operationplan.EffectStepPersistence,
			),
		),
	)
	structure, err := builder.Compile(builder.ForwardPhase("apply-effect/test", segment))
	if err != nil {
		t.Fatal(err)
	}
	prepared := ApplyEffectPlan{
		structure:        structure,
		segment:          segment,
		promotionIndexes: map[ownershipOutputKey]int{preparedKey: 0},
		changed:          true,
		valid:            true,
	}
	current := ApplyEffectPlan{
		structure:        structure,
		segment:          segment,
		promotionIndexes: map[ownershipOutputKey]int{currentKey: 0},
		changed:          true,
		valid:            true,
	}
	execution, err := newApplyEffectExecution(prepared, current)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := execution.promotionReference(preparedKey); err == nil {
		t.Fatal("prepared-plan promotion metadata remained authoritative")
	}
	if got, err := execution.promotionReference(currentKey); err != nil ||
		got != "apply/ownership-promotion/0" {
		t.Fatalf("current promotion reference = %q, %v", got, err)
	}
}

func TestApplyEffectExecutionVisibilityGateRunsInExactOrder(t *testing.T) {
	t.Parallel()

	plan := singleApplyVisibilityPlan(t, "apply/test-effect", operationplan.EffectStepPersistence)
	execution, err := newApplyEffectExecution(plan, plan)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	gate := execution.visibilityGate(visibilityEffectGate{
		before: func(context.Context) error {
			order = append(order, "validate")
			return nil
		},
		after: func(context.Context) error {
			order = append(order, "accept")
			return nil
		},
	}, "apply/test-effect", operationplan.EffectStepPersistence)
	if err := gate.validateBefore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := gate.applyEffect(func() error {
		order = append(order, "effect")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := gate.acceptAfter(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := execution.finish(nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(order, []string{"validate", "effect", "accept"}) {
		t.Fatalf("visibility order = %v", order)
	}
}

func TestApplyEffectExecutionVisibilityFailuresStopAtExactBoundary(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("injected boundary failure")
	tests := []struct {
		name      string
		failAt    string
		wantOrder []string
	}{
		{name: "validation", failAt: "validate", wantOrder: []string{"validate"}},
		{name: "effect", failAt: "effect", wantOrder: []string{"validate", "effect"}},
		{name: "acceptance", failAt: "accept", wantOrder: []string{"validate", "effect", "accept"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			plan := singleApplyVisibilityPlan(t, "apply/test-effect", operationplan.EffectStepPersistence)
			execution, err := newApplyEffectExecution(plan, plan)
			if err != nil {
				t.Fatal(err)
			}
			var order []string
			gate := execution.visibilityGate(visibilityEffectGate{
				before: func(context.Context) error {
					order = append(order, "validate")
					if test.failAt == "validate" {
						return wantErr
					}
					return nil
				},
				after: func(context.Context) error {
					order = append(order, "accept")
					if test.failAt == "accept" {
						return wantErr
					}
					return nil
				},
			}, "apply/test-effect", operationplan.EffectStepPersistence)

			err = gate.validateBefore(context.Background())
			if err == nil {
				err = gate.applyEffect(func() error {
					order = append(order, "effect")
					if test.failAt == "effect" {
						return wantErr
					}
					return nil
				})
			}
			if err == nil {
				err = gate.acceptAfter(context.Background())
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("boundary error = %v, want injected failure", err)
			}
			if !slices.Equal(order, test.wantOrder) {
				t.Fatalf("visibility order = %v, want %v", order, test.wantOrder)
			}
			if finishErr := execution.finish(wantErr); finishErr != nil {
				t.Fatalf("finish terminal failure: %v", finishErr)
			}
		})
	}
}

func TestApplyEffectExecutionPromotionBranchesAndFinalization(t *testing.T) {
	t.Parallel()

	key := ownershipOutputKey{}
	for _, test := range []struct {
		name   string
		active bool
	}{
		{name: "no-op", active: false},
		{name: "active", active: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var builder operationplan.EffectStructureBuilder
			promotionSchedule := &provisionalPromotionSchedule{
				intentByKey: map[ownershipOutputKey]int{key: 0},
				keys:        []ownershipOutputKey{key},
				scheduled:   make([]bool, 1),
				triggered:   true,
			}
			promotion := promotionSchedule.forKeys(&builder, []ownershipOutputKey{key})
			segment := operationplan.EffectSequence(
				operationplan.EffectSequence(promotion...),
				builder.Conditional(
					applyOwnershipPromotionTrigger,
					applyClaimTransitionObligation(
						&builder,
						"apply/ownership-promotion/finalization",
					),
				),
			)
			structure, err := builder.Compile(builder.ForwardPhase("apply-effect/test", segment))
			if err != nil {
				t.Fatal(err)
			}
			plan := ApplyEffectPlan{
				structure:        structure,
				segment:          segment,
				promotionIndexes: maps.Clone(promotionSchedule.intentByKey),
				changed:          true,
				valid:            true,
			}
			execution, err := newApplyEffectExecution(plan, plan)
			if err != nil {
				t.Fatal(err)
			}
			if err := execution.runObservation(
				"apply/ownership-promotion/0/observation",
				func() error { return nil },
			); err != nil {
				t.Fatal(err)
			}
			if err := execution.selectPromotion(key, test.active); err != nil {
				t.Fatal(err)
			}
			if test.active {
				for _, step := range []struct {
					id   string
					kind operationplan.EffectStepKind
				}{
					{id: "apply/ownership-promotion/0/journal", kind: operationplan.EffectStepPersistence},
					{id: "apply/ownership-promotion/0/claim", kind: operationplan.EffectStepPersistence},
				} {
					if err := settleApplyVisibilityStep(execution, step.id, step.kind); err != nil {
						t.Fatal(err)
					}
				}
				if err := execution.runObservation(
					"apply/ownership-promotion/finalization/transition-plan",
					func() error { return nil },
				); err != nil {
					t.Fatal(err)
				}
				if err := settleApplyVisibilityStep(
					execution,
					"apply/ownership-promotion/finalization",
					operationplan.EffectStepPersistence,
				); err != nil {
					t.Fatal(err)
				}
			}
			if err := execution.finish(nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestApplyEffectExecutionFailureTerminatesExactPath(t *testing.T) {
	fixture := newApplyEventFixture(t)
	plan, err := PrepareApplyEffectPlan(fixture.input([]applyEventAction{
		fixture.createAction("create", "CREATE.md", "created\n"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	execution, err := newApplyEffectExecution(plan, plan)
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("validation failed")
	calls := 0
	err = execution.runCheckedStep(
		"apply/journal-publication/forward",
		operationplan.EffectStepForwardEffect,
		func() error {
			calls++
			return wantErr
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("runCheckedStep error = %v, want validation failure", err)
	}
	if calls != 1 {
		t.Fatalf("action calls = %d, want 1", calls)
	}
	if err := execution.finish(err); err != nil {
		t.Fatalf("finish failed terminal path: %v", err)
	}
	if err := execution.runObservation("unused", func() error { return nil }); err == nil {
		t.Fatal("terminal execution accepted another step")
	}
}

func TestApplyEffectExecutionClaimPlanningFailureTerminatesBeforePersistence(t *testing.T) {
	t.Parallel()

	var builder operationplan.EffectStructureBuilder
	segment := applyClaimTransitionObligation(&builder, "apply/ownership-preparation")
	structure, err := builder.Compile(builder.ForwardPhase("apply-effect/test", segment))
	if err != nil {
		t.Fatal(err)
	}
	plan := ApplyEffectPlan{structure: structure, segment: segment, changed: true, valid: true}
	execution, err := newApplyEffectExecution(plan, plan)
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("invalid ownership transition plan")
	err = execution.runObservation(
		"apply/ownership-preparation/transition-plan",
		func() error { return wantErr },
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("transition-plan error = %v, want injected failure", err)
	}
	persisted := false
	gate := execution.visibilityGate(
		visibilityEffectGate{},
		"apply/ownership-preparation",
		operationplan.EffectStepPersistence,
	)
	if persistenceErr := gate.applyEffect(func() error {
		persisted = true
		return nil
	}); persistenceErr == nil {
		t.Fatal("terminal transition-plan failure admitted ownership persistence")
	}
	if persisted {
		t.Fatal("ownership persistence ran after transition-plan failure")
	}
	if finishErr := execution.finish(wantErr); finishErr != nil {
		t.Fatalf("finish terminal transition-plan failure: %v", finishErr)
	}
}

func TestApplyEffectExecutionRejectsPreEffectAbortAfterEffectStarted(t *testing.T) {
	t.Parallel()

	var builder operationplan.EffectStructureBuilder
	segment := operationplan.EffectSequence(
		applyForwardObligation(&builder, "apply/test-effect", operationplan.EffectStepPersistence),
		applyCheckedStep(&builder, "apply/post-effect", operationplan.EffectStepObservation),
	)
	structure, err := builder.Compile(builder.ForwardPhase("apply-effect/test", segment))
	if err != nil {
		t.Fatal(err)
	}
	plan := ApplyEffectPlan{structure: structure, segment: segment, changed: true, valid: true}
	execution, err := newApplyEffectExecution(plan, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := settleApplyVisibilityStep(
		execution,
		"apply/test-effect",
		operationplan.EffectStepPersistence,
	); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("unscheduled post-effect failure")
	if finishErr := execution.finish(wantErr); finishErr == nil ||
		!strings.Contains(finishErr.Error(), "remained incomplete after failure") {
		t.Fatalf("finish error = %v, want incomplete post-effect schedule", finishErr)
	}
}

func TestApplyEffectExecutionRejectsUnderConsumedSuccess(t *testing.T) {
	fixture := newApplyEventFixture(t)
	plan, err := PrepareApplyEffectPlan(fixture.input([]applyEventAction{
		fixture.createAction("create", "CREATE.md", "created\n"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	execution, err := newApplyEffectExecution(plan, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := execution.finish(nil); err == nil || !strings.Contains(err.Error(), "was not consumed") {
		t.Fatalf("finish error = %v, want under-consumed schedule", err)
	}
}

func TestApplyWithEffectPlanAcceptsExactNoChangePlanWithoutEffects(t *testing.T) {
	t.Parallel()

	fixture := newApplyEventFixture(t)
	input := fixture.input(nil)
	plan, err := PrepareApplyEffectPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	var events []Event
	result, err := ApplyWithEffectPlan(
		context.Background(),
		input,
		plan,
		ApplyOptions{Events: func(event Event) { events = append(events, event) }},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.ActionCount != 0 || result.ExecutionAttempted {
		t.Fatalf("no-change result = %#v", result)
	}
	if len(events) != 0 {
		t.Fatalf("no-change events = %#v, want none", events)
	}
}

func TestApplyWithEffectPlanConsumesExactChangedPlan(t *testing.T) {
	fixture := newApplyEventFixture(t)
	input := fixture.input([]applyEventAction{
		fixture.createAction("create", "CREATE.md", "created\n"),
	})
	plan, err := PrepareApplyEffectPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ApplyWithEffectPlan(context.Background(), input, plan, ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ExecutionAttempted || result.ActionCount != 1 {
		t.Fatalf("ApplyWithEffectPlan result = %#v", result)
	}
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestApplyWithEffectPlanRejectsMismatchBeforeJournalCapture(t *testing.T) {
	fixture := newApplyEventFixture(t)
	create := fixture.createAction("create", "CREATE.md", "created\n")
	update := fixture.updateAction("update", "UPDATE.md", "old\n", "new\n")
	prepared, err := PrepareApplyEffectPlan(fixture.input([]applyEventAction{create}))
	if err != nil {
		t.Fatal(err)
	}
	var events []Event
	_, err = ApplyWithEffectPlan(
		context.Background(),
		fixture.input([]applyEventAction{create, update}),
		prepared,
		ApplyOptions{Events: func(event Event) { events = append(events, event) }},
	)
	if err == nil || !strings.Contains(err.Error(), "prepared and current apply effect plans differ") {
		t.Fatalf("ApplyWithEffectPlan error = %v, want plan mismatch", err)
	}
	if containsApplyEventKind(events, EventJournalCaptureStarted) {
		t.Fatalf("events = %#v, journal capture must not start after plan mismatch", events)
	}
}

func singleApplyVisibilityPlan(
	t *testing.T,
	id string,
	kind operationplan.EffectStepKind,
) ApplyEffectPlan {
	t.Helper()
	var builder operationplan.EffectStructureBuilder
	segment := applyForwardObligation(&builder, id, kind)
	structure, err := builder.Compile(builder.ForwardPhase("apply-effect/test", segment))
	if err != nil {
		t.Fatal(err)
	}
	return ApplyEffectPlan{
		structure: structure,
		segment:   segment,
		changed:   true,
		valid:     true,
	}
}

func settleApplyVisibilityStep(
	execution *applyEffectExecution,
	id string,
	kind operationplan.EffectStepKind,
) error {
	gate := execution.visibilityGate(visibilityEffectGate{}, id, kind)
	if err := gate.validateBefore(context.Background()); err != nil {
		return err
	}
	if err := gate.applyEffect(func() error { return nil }); err != nil {
		return err
	}
	return gate.acceptAfter(context.Background())
}
