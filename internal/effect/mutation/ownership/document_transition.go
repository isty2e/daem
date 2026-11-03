package ownership

import (
	"fmt"

	outputownership "github.com/isty2e/daem/internal/output/ownership"
)

// DocumentClaimTransition keeps exclusion across footprint reshaping without
// authorizing a host write. Prepared claims cover only actual before/after units.
type DocumentClaimTransition struct {
	change   outputownership.DocumentClaimChange
	prepared []outputownership.Claim
}

func NewDocumentClaimTransition(before, after []outputownership.Claim, operationID string) (DocumentClaimTransition, error) {
	change, err := outputownership.NewDocumentClaimChange(before, after)
	if err != nil {
		return DocumentClaimTransition{}, err
	}
	byAddress := make(map[outputownership.ManagedAddress]outputownership.Claim, len(before))
	for _, claim := range change.Expected() {
		if claim.State() != outputownership.ClaimActive {
			return DocumentClaimTransition{}, fmt.Errorf("document transition before claims must be active")
		}
		byAddress[claim.Address()] = claim
	}
	afterByAddress := make(map[outputownership.ManagedAddress]outputownership.Claim, len(after))
	for _, claim := range change.Target() {
		if claim.State() != outputownership.ClaimActive {
			return DocumentClaimTransition{}, fmt.Errorf("document transition after claims must be active")
		}
		if previous, present := byAddress[claim.Address()]; present && !previous.Equal(claim) {
			return DocumentClaimTransition{}, fmt.Errorf("document transition changes a retained claim's provenance")
		}
		afterByAddress[claim.Address()] = claim
	}

	prepared := make([]outputownership.Claim, 0)
	for _, address := range change.FootprintCover() {
		if claim, present := byAddress[address]; present {
			prepared = append(prepared, claim)
			continue
		}
		claim := afterByAddress[address]
		reserved, err := outputownership.NewReservedClaim(address, claim.Owner(), operationID)
		if err != nil {
			return DocumentClaimTransition{}, err
		}
		prepared = append(prepared, reserved)
	}
	phase, err := outputownership.NewRegistry(prepared)
	if err != nil {
		return DocumentClaimTransition{}, err
	}
	return DocumentClaimTransition{change: change, prepared: phase.Claims()}, nil
}

func (transition DocumentClaimTransition) Before() []outputownership.Claim {
	return transition.change.Expected()
}

func (transition DocumentClaimTransition) Prepared() []outputownership.Claim {
	return append([]outputownership.Claim(nil), transition.prepared...)
}

func (transition DocumentClaimTransition) After() []outputownership.Claim {
	return transition.change.Target()
}

func (transition DocumentClaimTransition) Preparation() (outputownership.DocumentClaimChange, error) {
	return transition.phaseChange(transition.Before(), transition.Prepared())
}

func (transition DocumentClaimTransition) Finalization() (outputownership.DocumentClaimChange, error) {
	return transition.phaseChange(transition.Prepared(), transition.After())
}

func (transition DocumentClaimTransition) Rollback() (outputownership.DocumentClaimChange, error) {
	return transition.phaseChange(transition.Prepared(), transition.Before())
}

func (transition DocumentClaimTransition) phaseChange(expected, target []outputownership.Claim) (outputownership.DocumentClaimChange, error) {
	if err := transition.change.Validate(); err != nil {
		return outputownership.DocumentClaimChange{}, err
	}
	return outputownership.NewDocumentClaimChange(expected, target)
}
