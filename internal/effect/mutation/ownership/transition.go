// Package ownership models legal durable ownership-claim mutation phases.
package ownership

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
)

// TransitionKind identifies one durable ownership mutation family.
type TransitionKind string

const (
	// TransitionAcquire reserves an absent address, then activates it after host/state commit.
	TransitionAcquire TransitionKind = "acquire"
	// TransitionRelease retains active ownership through host/state commit, then removes it.
	TransitionRelease TransitionKind = "release"
	// TransitionRetain guards an unchanged active claim through update or state refresh.
	TransitionRetain TransitionKind = "retain"
)

// ClaimTransition records the only legal durable phases for one address mutation.
type ClaimTransition struct {
	kind     TransitionKind
	before   outputownership.ClaimValue
	prepared outputownership.ClaimValue
	after    outputownership.ClaimValue
}

// NewTransition validates and reconstructs one transition from canonical phase values.
func NewTransition(
	kind TransitionKind,
	before outputownership.ClaimValue,
	prepared outputownership.ClaimValue,
	after outputownership.ClaimValue,
) (ClaimTransition, error) {
	transition := ClaimTransition{kind: kind, before: before, prepared: prepared, after: after}
	if err := transition.Validate(); err != nil {
		return ClaimTransition{}, err
	}
	return transition, nil
}

// NewAcquireTransition constructs absent -> reserved -> active ownership.
func NewAcquireTransition(
	address outputownership.ManagedAddress,
	owner stateauthority.Authority,
	operationID string,
) (ClaimTransition, error) {
	reserved, err := outputownership.NewReservedClaim(address, owner, operationID)
	if err != nil {
		return ClaimTransition{}, err
	}
	active, err := outputownership.NewActiveClaim(address, owner)
	if err != nil {
		return ClaimTransition{}, err
	}
	prepared, err := outputownership.PresentClaim(reserved)
	if err != nil {
		return ClaimTransition{}, err
	}
	after, err := outputownership.PresentClaim(active)
	if err != nil {
		return ClaimTransition{}, err
	}
	return NewTransition(TransitionAcquire, outputownership.NoClaim(), prepared, after)
}

// NewReleaseTransition constructs active -> active -> absent ownership.
func NewReleaseTransition(active outputownership.Claim) (ClaimTransition, error) {
	if err := active.Validate(); err != nil {
		return ClaimTransition{}, err
	}
	if active.State() != outputownership.ClaimActive {
		return ClaimTransition{}, fmt.Errorf("ownership release requires an active claim")
	}
	value, err := outputownership.PresentClaim(active)
	if err != nil {
		return ClaimTransition{}, err
	}
	return NewTransition(TransitionRelease, value, value, outputownership.NoClaim())
}

// NewRetainTransition constructs active -> active -> active ownership.
func NewRetainTransition(active outputownership.Claim) (ClaimTransition, error) {
	if err := active.Validate(); err != nil {
		return ClaimTransition{}, err
	}
	if active.State() != outputownership.ClaimActive {
		return ClaimTransition{}, fmt.Errorf("ownership retention requires an active claim")
	}
	value, err := outputownership.PresentClaim(active)
	if err != nil {
		return ClaimTransition{}, err
	}
	return NewTransition(TransitionRetain, value, value, value)
}

// Validate rejects a zero value or an impossible phase sequence.
func (transition ClaimTransition) Validate() error {
	switch transition.kind {
	case TransitionAcquire:
		if _, present := transition.before.Get(); present {
			return fmt.Errorf("ownership acquire before claim must be absent")
		}
		prepared, err := requireClaimState("acquire prepared", transition.prepared, outputownership.ClaimReserved)
		if err != nil {
			return err
		}
		after, err := requireClaimState("acquire after", transition.after, outputownership.ClaimActive)
		if err != nil {
			return err
		}
		if !prepared.Address().Equal(after.Address()) || !prepared.Owner().ExactEqual(after.Owner()) {
			return fmt.Errorf("ownership acquire phases must have one address and owner")
		}
	case TransitionRelease:
		before, err := requireClaimState("release before", transition.before, outputownership.ClaimActive)
		if err != nil {
			return err
		}
		prepared, err := requireClaimState("release prepared", transition.prepared, outputownership.ClaimActive)
		if err != nil {
			return err
		}
		if !before.Equal(prepared) {
			return fmt.Errorf("ownership release must retain the active claim until finalization")
		}
		if _, present := transition.after.Get(); present {
			return fmt.Errorf("ownership release after claim must be absent")
		}
	case TransitionRetain:
		before, err := requireClaimState("retain before", transition.before, outputownership.ClaimActive)
		if err != nil {
			return err
		}
		prepared, err := requireClaimState("retain prepared", transition.prepared, outputownership.ClaimActive)
		if err != nil {
			return err
		}
		after, err := requireClaimState("retain after", transition.after, outputownership.ClaimActive)
		if err != nil {
			return err
		}
		if !before.Equal(prepared) || !prepared.Equal(after) {
			return fmt.Errorf("ownership retain phases must preserve one active claim")
		}
	default:
		return fmt.Errorf("unsupported ownership transition kind %q", transition.kind)
	}
	return nil
}

// Kind returns the transition family.
func (transition ClaimTransition) Kind() TransitionKind {
	return transition.kind
}

// Address returns the one managed footprint changed by this transition.
func (transition ClaimTransition) Address() outputownership.ManagedAddress {
	claim, ok := transition.prepared.Get()
	if !ok {
		claim, _ = transition.before.Get()
	}
	return claim.Address()
}

// Before returns the claim expected before preparation.
func (transition ClaimTransition) Before() outputownership.ClaimValue {
	return transition.before
}

// Prepared returns the claim required before the first host effect.
func (transition ClaimTransition) Prepared() outputownership.ClaimValue {
	return transition.prepared
}

// After returns the claim required after host and local-state commit.
func (transition ClaimTransition) After() outputownership.ClaimValue {
	return transition.after
}

// Equal reports exact transition-family and phase-claim equality.
func (transition ClaimTransition) Equal(other ClaimTransition) bool {
	return transition.kind == other.kind &&
		transition.before.Equal(other.before) &&
		transition.prepared.Equal(other.prepared) &&
		transition.after.Equal(other.after)
}

// Owner returns the state authority responsible for every phase of the transition.
func (transition ClaimTransition) Owner() stateauthority.Authority {
	claim, ok := transition.prepared.Get()
	if !ok {
		claim, _ = transition.before.Get()
	}
	return claim.Owner()
}

func requireClaimState(
	phase string,
	value outputownership.ClaimValue,
	state outputownership.ClaimState,
) (outputownership.Claim, error) {
	claim, present := value.Get()
	if !present {
		return outputownership.Claim{}, fmt.Errorf("ownership %s claim must be present", phase)
	}
	if err := claim.Validate(); err != nil {
		return outputownership.Claim{}, fmt.Errorf("ownership %s claim: %w", phase, err)
	}
	if claim.State() != state {
		return outputownership.Claim{}, fmt.Errorf("ownership %s claim must be %s", phase, state)
	}
	return claim, nil
}
