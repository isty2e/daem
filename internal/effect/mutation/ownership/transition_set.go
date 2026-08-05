package ownership

import (
	"fmt"
	"sort"

	outputownership "github.com/isty2e/daem/internal/output/ownership"
)

// ClaimTransitionSet is one canonical set of independent ownership lifecycles.
type ClaimTransitionSet struct {
	transitions []ClaimTransition
	initialized bool
}

// NewClaimTransitionSet validates, copies, and canonically orders transitions.
func NewClaimTransitionSet(transitions []ClaimTransition) (ClaimTransitionSet, error) {
	canonical := append([]ClaimTransition(nil), transitions...)
	for index, transition := range canonical {
		if err := transition.Validate(); err != nil {
			return ClaimTransitionSet{}, fmt.Errorf("ownership claim transition[%d]: %w", index, err)
		}
	}
	sort.Slice(canonical, func(left int, right int) bool {
		return canonical[left].Address().Less(canonical[right].Address())
	})
	if len(canonical) != 0 {
		owner := canonical[0].Owner()
		operationID := ""
		for index, transition := range canonical[1:] {
			if !owner.ExactEqual(transition.Owner()) {
				return ClaimTransitionSet{}, fmt.Errorf(
					"ownership claim transition[%d] has a different state authority",
					index+1,
				)
			}
		}
		for index, transition := range canonical {
			if transition.Kind() != TransitionAcquire {
				continue
			}
			prepared, _ := transition.Prepared().Get()
			if operationID == "" {
				operationID = prepared.OperationID()
				continue
			}
			if operationID != prepared.OperationID() {
				return ClaimTransitionSet{}, fmt.Errorf(
					"ownership claim transition[%d] belongs to a different operation",
					index,
				)
			}
		}
	}
	changes := make([]outputownership.ClaimChange, 0, len(canonical))
	for _, transition := range canonical {
		change, err := outputownership.NewClaimChange(
			transition.Address(),
			transition.Before(),
			transition.Before(),
		)
		if err != nil {
			return ClaimTransitionSet{}, err
		}
		changes = append(changes, change)
	}
	if _, err := outputownership.NewClaimConvergence(changes); err != nil {
		return ClaimTransitionSet{}, fmt.Errorf("ownership claim transition set: %w", err)
	}
	return ClaimTransitionSet{transitions: canonical, initialized: true}, nil
}

// Preparation derives the before-to-prepared registry convergence.
func (set ClaimTransitionSet) Preparation() (outputownership.ClaimConvergence, error) {
	return set.convergence(func(transition ClaimTransition) (outputownership.ClaimValue, outputownership.ClaimValue) {
		return transition.Before(), transition.Prepared()
	})
}

// Finalization derives the prepared-to-after registry convergence.
func (set ClaimTransitionSet) Finalization() (outputownership.ClaimConvergence, error) {
	return set.convergence(func(transition ClaimTransition) (outputownership.ClaimValue, outputownership.ClaimValue) {
		return transition.Prepared(), transition.After()
	})
}

// Rollback derives the prepared-to-before registry convergence.
func (set ClaimTransitionSet) Rollback() (outputownership.ClaimConvergence, error) {
	return set.convergence(func(transition ClaimTransition) (outputownership.ClaimValue, outputownership.ClaimValue) {
		return transition.Prepared(), transition.Before()
	})
}

func (set ClaimTransitionSet) convergence(
	values func(ClaimTransition) (outputownership.ClaimValue, outputownership.ClaimValue),
) (outputownership.ClaimConvergence, error) {
	if !set.initialized {
		return outputownership.ClaimConvergence{}, fmt.Errorf("ownership claim transition set is required")
	}
	changes := make([]outputownership.ClaimChange, 0, len(set.transitions))
	for _, transition := range set.transitions {
		expected, target := values(transition)
		change, err := outputownership.NewClaimChange(transition.Address(), expected, target)
		if err != nil {
			return outputownership.ClaimConvergence{}, err
		}
		changes = append(changes, change)
	}
	return outputownership.NewClaimConvergence(changes)
}
