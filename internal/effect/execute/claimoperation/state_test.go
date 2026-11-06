package claimoperation

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/test/outputtest"
)

func TestClaimStateRetainsSelectedPromotionProgressAcrossFailureAndRetry(t *testing.T) {
	intents, transitions := claimStateFactsForTest(t)
	plan := Plan{transitions: []ownershipmutation.ClaimTransition{transitions[0]}, intents: append([]ownership.ProvisionalAcquireIntent(nil), intents[1:]...)}
	state := NewState(plan)
	copied := state
	plan.transitions[0], plan.intents[0] = transitions[4], intents[4]
	transitionSnapshot, intentSnapshot := state.Transitions(), state.Intents()
	transitionSnapshot[0], intentSnapshot[0] = transitions[3], intents[3]
	assertClaimState(t, state, transitions[:1], intents[1:])

	keys := []ownership.LogicalKey{
		claimIntentKey(intents[3]), claimIntentKey(intents[2]), claimIntentKey(intents[1]), claimIntentKey(intents[2]),
	}
	stop := errors.New("later promotion refused")
	var visited []ownership.ProvisionalAcquireIntent
	err := state.Promote(t.Context(), keys, func(_ context.Context, intent ownership.ProvisionalAcquireIntent) (ownershipmutation.ClaimTransition, bool, error) {
		visited = append(visited, intent)
		switch {
		case intent.Equal(intents[1]):
			return ownershipmutation.ClaimTransition{}, false, nil
		case intent.Equal(intents[2]):
			return transitions[2], true, nil
		case intent.Equal(intents[3]):
			return ownershipmutation.ClaimTransition{}, false, stop
		default:
			t.Fatal("unselected pending intent reached promotion")
			return ownershipmutation.ClaimTransition{}, false, nil
		}
	})
	if !errors.Is(err, stop) {
		t.Fatalf("promotion error = %v, want later callback error", err)
	}
	if len(visited) != 3 {
		t.Fatalf("promotion visits = %d, want ordered invisible/success/failure visits", len(visited))
	}
	for index, intent := range visited {
		if !intent.Equal(intents[index+1]) {
			t.Fatalf("promotion visit[%d] changed pending order", index)
		}
	}
	expectedTransitions := []ownershipmutation.ClaimTransition{transitions[0], transitions[2]}
	expectedPending := []ownership.ProvisionalAcquireIntent{intents[1], intents[3], intents[4]}
	assertClaimState(t, state, expectedTransitions, expectedPending)
	assertClaimState(t, copied, expectedTransitions, expectedPending)

	visited = nil
	err = copied.Promote(t.Context(), []ownership.LogicalKey{claimIntentKey(intents[3]), claimIntentKey(intents[1])}, func(_ context.Context, intent ownership.ProvisionalAcquireIntent) (ownershipmutation.ClaimTransition, bool, error) {
		visited = append(visited, intent)
		if intent.Equal(intents[1]) {
			return transitions[1], true, nil
		}
		if intent.Equal(intents[3]) {
			return transitions[3], true, nil
		}
		t.Fatal("retry visited an unselected or already promoted intent")
		return ownershipmutation.ClaimTransition{}, false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(visited) != 2 || !visited[0].Equal(intents[1]) || !visited[1].Equal(intents[3]) {
		t.Fatal("retry did not preserve the remaining pending order")
	}
	expectedTransitions = append(expectedTransitions, transitions[1], transitions[3])
	assertClaimState(t, state, expectedTransitions, intents[4:])
	calls := 0
	err = state.Promote(t.Context(), []ownership.LogicalKey{claimIntentKey(intents[4]), claimIntentKey(intents[4])}, func(_ context.Context, intent ownership.ProvisionalAcquireIntent) (ownershipmutation.ClaimTransition, bool, error) {
		calls++
		if !intent.Equal(intents[4]) {
			t.Fatal("final promotion changed its selected intent")
		}
		return transitions[4], true, nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("duplicate-key promotion = %d calls, %v; want one success", calls, err)
	}
	assertClaimState(t, copied, append(expectedTransitions, transitions[4]), nil)
}

func TestClaimStateEmptySelectionHasNoPromotionEffect(t *testing.T) {
	var zero State
	if zero.HasMutations() || zero.HasTransitions() || zero.IntentCount() != 0 || zero.Transitions() != nil || zero.Intents() != nil {
		t.Fatal("zero claim state is not empty")
	}
	if err := zero.Promote(t.Context(), nil, nil); err != nil {
		t.Fatal(err)
	}
	intents, transitions := claimStateFactsForTest(t)
	state := NewState(Plan{intents: intents[1:2]})
	if err := state.Promote(t.Context(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := state.Promote(t.Context(), []ownership.LogicalKey{claimIntentKey(intents[2])}, nil); err != nil {
		t.Fatalf("unselected key required an unused effect step: %v", err)
	}
	if err := state.Promote(t.Context(), []ownership.LogicalKey{claimIntentKey(intents[1])}, nil); err == nil {
		t.Fatal("selected promotion accepted a missing effect step")
	}
	assertClaimState(t, state, nil, intents[1:2])
	err := state.Promote(t.Context(), []ownership.LogicalKey{claimIntentKey(intents[2])}, func(context.Context, ownership.ProvisionalAcquireIntent) (ownershipmutation.ClaimTransition, bool, error) {
		t.Fatal("unselected promotion called its effect step")
		return transitions[2], true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertClaimState(t, state, nil, intents[1:2])
}

func claimStateFactsForTest(t *testing.T) ([]ownership.ProvisionalAcquireIntent, []ownershipmutation.ClaimTransition) {
	t.Helper()
	root := t.TempDir()
	owner, err := stateauthority.New(pathtest.Exact(filepath.Join(root, "state.json")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var intents []ownership.ProvisionalAcquireIntent
	var transitions []ownershipmutation.ClaimTransition
	for _, name := range []string{"existing", "invisible", "published", "failing", "unselected"} {
		path := filepath.Join(root, "Café-"+name)
		exact := pathtest.DarwinCaseSensitive(path)
		provisional, err := pathauthority.NewProvisional(path, exact.Witness(), root, pathtest.DarwinCaseSensitive(root).Witness())
		if err != nil {
			t.Fatal(err)
		}
		intent, err := ownership.NewProvisionalAcquireIntent(outputtest.Parse(t, "~/"+name), output.WholePathFootprint(), provisional, owner, "operation-1")
		if err != nil {
			t.Fatal(err)
		}
		address, err := ownership.NewManagedAddress(exact, intent.Footprint())
		if err != nil {
			t.Fatal(err)
		}
		transition, err := ownershipmutation.NewAcquireTransitionFromIntent(intent, address)
		if err != nil {
			t.Fatal(err)
		}
		intents, transitions = append(intents, intent), append(transitions, transition)
	}
	return intents, transitions
}

func claimIntentKey(intent ownership.ProvisionalAcquireIntent) ownership.LogicalKey {
	return ownership.LogicalKey{Destination: intent.Destination(), Footprint: intent.Footprint()}
}

func assertClaimState(t *testing.T, state State, transitions []ownershipmutation.ClaimTransition, intents []ownership.ProvisionalAcquireIntent) {
	t.Helper()
	actualTransitions, actualIntents := state.Transitions(), state.Intents()
	if len(actualTransitions) != len(transitions) || len(actualIntents) != len(intents) || state.IntentCount() != len(intents) || state.HasTransitions() != (len(transitions) != 0) || state.HasMutations() != (len(transitions)+len(intents) != 0) {
		t.Fatalf("claim state = %d transitions, %d intents; want %d, %d", len(actualTransitions), len(actualIntents), len(transitions), len(intents))
	}
	for index, transition := range actualTransitions {
		if !transition.Equal(transitions[index]) {
			t.Fatalf("claim transition[%d] changed its owned facts", index)
		}
	}
	for index, intent := range actualIntents {
		if !intent.Equal(intents[index]) {
			t.Fatalf("claim intent[%d] changed its owned facts", index)
		}
	}
}
