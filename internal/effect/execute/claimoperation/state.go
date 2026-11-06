// Package claimoperation owns ownership-claim plans, pending promotion state
// and batched Registry convergence, not physical binding or journal sequencing.
package claimoperation

import (
	"context"
	"fmt"
	"slices"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

type PromotionStep func(context.Context, ownership.ProvisionalAcquireIntent) (ownershipmutation.ClaimTransition, bool, error)

type State struct {
	*claimState
}

type claimState struct {
	transitions []ownershipmutation.ClaimTransition
	provisional []ownership.ProvisionalAcquireIntent
}

func NewState(plans ...Plan) State {
	state := State{claimState: &claimState{}}
	for _, plan := range plans {
		state.transitions = append(state.transitions, plan.transitions...)
		state.provisional = append(state.provisional, plan.intents...)
	}
	return state
}

func (state State) HasMutations() bool {
	return state.HasTransitions() || state.IntentCount() != 0
}

func (state State) HasTransitions() bool {
	return state.claimState != nil && len(state.transitions) != 0
}

func (state State) IntentCount() int {
	if state.claimState == nil {
		return 0
	}
	return len(state.provisional)
}

func (state State) Transitions() []ownershipmutation.ClaimTransition {
	if state.claimState == nil {
		return nil
	}
	return slices.Clone(state.transitions)
}

func (state State) Intents() []ownership.ProvisionalAcquireIntent {
	if state.claimState == nil {
		return nil
	}
	return slices.Clone(state.provisional)
}

func (state State) Promote(ctx context.Context, keys []ownership.LogicalKey, step PromotionStep) error {
	if state.claimState == nil || len(state.provisional) == 0 || len(keys) == 0 {
		return nil
	}

	requested := make(map[ownership.LogicalKey]struct{}, len(keys))
	for _, key := range keys {
		requested[key] = struct{}{}
	}

	for index := 0; index < len(state.provisional); {
		intent := state.provisional[index]
		key := ownership.LogicalKey{Destination: intent.Destination(), Footprint: intent.Footprint()}
		if _, selected := requested[key]; !selected {
			index++
			continue
		}
		if step == nil {
			return fmt.Errorf("ownership promotion step is required")
		}
		transition, promoted, err := step(ctx, intent)
		if err != nil {
			return err
		}
		if !promoted {
			index++
			continue
		}
		state.transitions = append(state.transitions, transition)
		state.provisional = append(state.provisional[:index], state.provisional[index+1:]...)
	}

	return nil
}
