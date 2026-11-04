package journal

import (
	"errors"
	"fmt"
	"slices"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

type ordinaryOwnershipFacts struct {
	OperationID         string
	Entries             []recoveryEntry
	ClaimTransitions    []recoveryClaimTransition
	ProvisionalAcquires []recoveryProvisionalAcquireIntent
}

func (material ordinaryOwnershipFacts) promoteOwnership(intent ownership.ProvisionalAcquireIntent, transition ownershipmutation.ClaimTransition) (ordinaryOwnershipFacts, error) {
	if err := errors.Join(intent.Validate(), transition.Validate()); err != nil {
		return ordinaryOwnershipFacts{}, err
	}
	derived, err := ownershipmutation.NewAcquireTransitionFromIntent(intent, transition.Address())
	if err != nil || !derived.Equal(transition) {
		return ordinaryOwnershipFacts{}, errors.Join(fmt.Errorf("ownership promotion differs from its intent"), err)
	}
	if material.OperationID != intent.OperationID() {
		return ordinaryOwnershipFacts{}, fmt.Errorf("recovery journal operation differs from ownership intent")
	}
	intents, err := canonicalProvisionalAcquireIntents(material.ProvisionalAcquires)
	if err != nil {
		return ordinaryOwnershipFacts{}, err
	}
	remaining := make([]ownership.ProvisionalAcquireIntent, 0, len(intents))
	matched := 0
	for _, candidate := range intents {
		if candidate.Equal(intent) {
			matched++
			continue
		}
		remaining = append(remaining, candidate)
	}
	if matched != 1 {
		return ordinaryOwnershipFacts{}, fmt.Errorf("recovery journal contains %d matching provisional ownership intents, want 1", matched)
	}
	entryIndex, err := promotedIntentEntryIndex(material.Entries, intent)
	if err != nil {
		return ordinaryOwnershipFacts{}, err
	}
	transitions, err := canonicalClaimTransitions(material.ClaimTransitions)
	if err != nil {
		return ordinaryOwnershipFacts{}, err
	}
	transitions = append(transitions, transition)
	persistedTransitions, err := recoveryClaimTransitions(transitions)
	if err != nil {
		return ordinaryOwnershipFacts{}, err
	}
	persistedIntents, err := recoveryProvisionalAcquireIntents(remaining)
	if err != nil {
		return ordinaryOwnershipFacts{}, err
	}
	candidate := material
	candidate.Entries = slices.Clone(material.Entries)
	candidate.Entries[entryIndex].OwnershipPathAuthority = persistedPathAuthority(transition.Address().PathAuthority())
	candidate.ClaimTransitions, candidate.ProvisionalAcquires = persistedTransitions, persistedIntents
	return candidate, nil
}
