package execute

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/mutation"
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
	if _, err := applyprogram.Begin(&prepared, expected); err == nil {
		t.Fatal("applyprogram.NewExecution returned nil error for different plans")
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

func TestApplyWithEffectPlanPreservesMutationAuthorityCloseFailure(t *testing.T) {
	fixture := newApplyEventFixture(t)
	input := fixture.input([]applyEventAction{
		fixture.createAction("create", "CREATE.md", "created\n"),
	})
	plan, err := PrepareApplyEffectPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	closeFailure := errors.New("close mutation authority")
	result, err := ApplyWithEffectPlan(
		context.Background(),
		input,
		plan,
		ApplyOptions{closeMutationAuthority: func(authority *mutationAuthority) error {
			return errors.Join(authority.close(), closeFailure)
		}},
	)
	if !errors.Is(err, closeFailure) {
		t.Fatalf("ApplyWithEffectPlan error = %v, want mutation-authority close failure", err)
	}
	if !result.ExecutionAttempted || result.ActionCount != 1 {
		t.Fatalf("ApplyWithEffectPlan result = %#v, want committed result", result)
	}
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestApplyWithEffectPlanJoinsPrimaryAndMutationAuthorityCloseFailures(t *testing.T) {
	fixture := newApplyEventFixture(t)
	input := fixture.input([]applyEventAction{
		fixture.createAction("create", "CREATE.md", "created\n"),
	})
	plan, err := PrepareApplyEffectPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	primary := errors.New("reject forward authority")
	closeFailure := errors.New("close mutation authority")
	result, err := ApplyWithEffectPlan(
		context.Background(),
		input,
		plan,
		ApplyOptions{
			ValidateBeforeEffects: func(context.Context, mutation.PhysicalAuthoritySet) error {
				return primary
			},
			closeMutationAuthority: func(authority *mutationAuthority) error {
				return errors.Join(authority.close(), closeFailure)
			},
		},
	)
	if !errors.Is(err, primary) || !errors.Is(err, closeFailure) {
		t.Fatalf("ApplyWithEffectPlan error = %v, want primary and close failures", err)
	}
	if strings.Contains(err.Error(), "apply effect structure") {
		t.Fatalf("ApplyWithEffectPlan error leaked cursor settlement failure: %v", err)
	}
	if result.ExecutionAttempted {
		t.Fatalf("ApplyWithEffectPlan result = %#v, want no attempted execution", result)
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
