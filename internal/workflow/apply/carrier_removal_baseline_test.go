package apply

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observepostcondition "github.com/isty2e/daem/internal/assurance/observe/postcondition"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

func TestRunBaselineFailureHasNoDurableOrHostEffect(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	input := fixture.input(t)
	input.BaselineObserver = func(
		context.Context,
		carrierabsence.Action,
	) (durablecarrier.EffectBaselineSet, error) {
		return durablecarrier.EffectBaselineSet{}, errors.New("injected baseline failure")
	}

	result, err := runCarrierRemovals(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "injected baseline failure") {
		t.Fatalf("error = %v, want baseline failure", err)
	}
	if fixture.executorCalls != 0 {
		t.Fatalf("executor calls = %d, want 0", fixture.executorCalls)
	}
	if len(result.Attempts) != 0 || len(result.State.PendingCarrierRemovals()) != 0 {
		t.Fatalf(
			"attempts/pending = %d/%d, want no durable effect",
			len(result.Attempts),
			len(result.State.PendingCarrierRemovals()),
		)
	}
	if !result.State.Equal(fixture.current) || !fixture.persistedState(t).Equal(fixture.current) {
		t.Fatal("baseline failure changed returned or persisted state")
	}
}

func TestRunCapturesBaselineBeforePendingAndInvokesHostAfterPending(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	input := fixture.input(t)
	originalObserver := input.Observer
	events := make([]string, 0, 3)
	input.BaselineObserver = func(
		context.Context,
		carrierabsence.Action,
	) (durablecarrier.EffectBaselineSet, error) {
		events = append(events, "baseline")
		if pending := fixture.persistedState(t).PendingCarrierRemovals(); len(pending) != 0 {
			t.Fatalf("baseline observer saw pending removals: %#v", pending)
		}
		return durablecarrier.EffectBaselineSet{}, nil
	}
	input.Executor = subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			events = append(events, "execute")
			pending := fixture.persistedState(t).PendingCarrierRemovals()
			if len(pending) != 1 {
				t.Fatalf("executor saw %d pending removals, want 1", len(pending))
			}
			fixture.executorCalls++
			return fixture.runnerResult
		},
	})
	input.Observer = func(
		ctx context.Context,
		pending durablecarrier.PendingCarrierRemoval,
		claims []durablecarrier.ManagedCarrierClaim,
	) assurancehostroute.ObservationFact {
		events = append(events, "observe")
		return originalObserver(ctx, pending, claims)
	}

	if _, err := runCarrierRemovals(context.Background(), input); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if want := []string{"baseline", "execute", "observe"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

func TestRunValidatesAfterBaselineAndNeverAfterHostMutation(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	input := fixture.input(t)
	baselineObserved := false
	input.BaselineObserver = func(
		context.Context,
		carrierabsence.Action,
	) (durablecarrier.EffectBaselineSet, error) {
		baselineObserved = true
		return durablecarrier.EffectBaselineSet{}, nil
	}
	validations := 0
	input.ValidateBeforeEffects = func(context.Context, mutation.PhysicalAuthoritySet) error {
		validations++
		if !baselineObserved {
			return errors.New("pre-effect revision validator ran before baseline observation")
		}
		if fixture.executorCalls != 0 {
			return errors.New("pre-effect revision validator ran after host invocation")
		}
		return nil
	}

	if _, err := runCarrierRemovals(context.Background(), input); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if validations != 1 {
		t.Fatalf("pre-effect validations = %d, want one immediately before commit", validations)
	}
}

func TestPendingSettlementReusesPersistedBaselineWithoutRecapture(t *testing.T) {
	requirements, err := effectpostcondition.NewSet(
		[]effectpostcondition.Requirement{effectpostcondition.LocalSourceUnchanged},
	)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newWorkflowFixtureWithPostconditions(
		t,
		target.ScopeProject,
		requirements,
		"",
	)
	baseline, err := durablecarrier.NewAbsentEffectBaseline(effectpostcondition.LocalSourceUnchanged)
	if err != nil {
		t.Fatal(err)
	}
	baselines, err := durablecarrier.NewEffectBaselineSet([]durablecarrier.EffectBaseline{baseline})
	if err != nil {
		t.Fatal(err)
	}
	baselineCalls := 0
	firstInput := fixture.input(t)
	firstInput.BaselineObserver = func(
		context.Context,
		carrierabsence.Action,
	) (durablecarrier.EffectBaselineSet, error) {
		baselineCalls++
		return baselines, nil
	}
	firstInput.Observer = nil

	first, err := runCarrierRemovals(context.Background(), firstInput)
	if err == nil {
		t.Fatal("first Run returned nil error without post-observation")
	}
	pending := first.State.PendingCarrierRemovals()
	if len(pending) != 1 || !pending[0].EffectBaselines().Equal(baselines) {
		t.Fatalf("persisted baselines = %#v, want exact captured set", pending)
	}
	if durablePending := fixture.persistedState(t).PendingCarrierRemovals(); len(durablePending) != 1 ||
		!durablePending[0].EffectBaselines().Equal(baselines) {
		t.Fatalf("durable pending = %#v, want exact captured baselines", durablePending)
	}

	observed, present := fixture.action.Observation()
	if !present {
		t.Fatal("workflow fixture has no observation key")
	}
	settlement, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:   fixture.claim,
		Desired: carrierabsence.DesiredAbsent,
		Observation: observerelation.Correlation{
			Key:    observed.Key,
			Result: missingCorrelation(t, fixture.expected),
		},
		Occupancy: fixture.action.Occupancy(),
		Route:     carrierabsence.UnavailableRoute(),
		Pending:   &pending[0],
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.current = first.State
	fixture.action = settlement
	secondInput := fixture.input(t)
	secondInput.BaselineObserver = func(
		context.Context,
		carrierabsence.Action,
	) (durablecarrier.EffectBaselineSet, error) {
		baselineCalls++
		return durablecarrier.EffectBaselineSet{}, errors.New("baseline must not be recaptured")
	}
	secondInput.Observer = func(
		_ context.Context,
		current durablecarrier.PendingCarrierRemoval,
		_ []durablecarrier.ManagedCarrierClaim,
	) assurancehostroute.ObservationFact {
		evidence, evidenceErr := observepostcondition.NewEvidence(
			effectpostcondition.LocalSourceUnchanged,
			observepostcondition.EvidenceSatisfied,
		)
		if evidenceErr != nil {
			t.Fatal(evidenceErr)
		}
		evidenceSet, evidenceErr := observepostcondition.NewSet(observepostcondition.SetInput{
			Subject:      current.Identity().RelationSubject(),
			RouteRequest: current.RemoveRequest(),
			Evidence:     []observepostcondition.Evidence{evidence},
		})
		if evidenceErr != nil {
			t.Fatal(evidenceErr)
		}
		return assurancehostroute.CurrentObservationWithEffectEvidence(
			missingCorrelation(t, fixture.expected),
			evidenceSet,
		)
	}

	second, err := runCarrierRemovals(context.Background(), secondInput)
	if err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}
	if baselineCalls != 1 {
		t.Fatalf("baseline observer calls = %d, want 1 total", baselineCalls)
	}
	if fixture.executorCalls != 1 {
		t.Fatalf("executor calls = %d, want no settlement reinvocation", fixture.executorCalls)
	}
	assertConvergedProjectRemoval(t, second.State, fixture.claim)
}
