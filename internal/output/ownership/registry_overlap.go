package ownership

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
)

// Overlapping returns every actual claim overlapping address in canonical order.
// A matching first claim is not evidence that a whole request has no blockers.
func (registry Registry) Overlapping(address ManagedAddress) ([]Claim, error) {
	if !registry.initialized {
		return nil, fmt.Errorf("ownership registry is required")
	}
	if err := address.Validate(); err != nil {
		return nil, err
	}
	var result []Claim
	for _, claim := range registry.claims {
		if claim.Address().Overlaps(address) {
			result = append(result, claim)
		}
	}
	return result, nil
}

// ProvisionalAncestorClaims retains all containing claims without promoting the
// candidate spelling to exact authority.
func (registry Registry) ProvisionalAncestorClaims(candidate pathauthority.Provisional) ([]Claim, error) {
	if !registry.initialized {
		return nil, fmt.Errorf("ownership registry is required")
	}
	if err := candidate.Validate(); err != nil {
		return nil, err
	}

	var result []Claim
	for _, claim := range registry.claims {
		if candidate.CandidateWithin(claim.Address().PathAuthority()) {
			result = append(result, claim)
		}
	}
	return result, nil
}

// RequireWriteExclusion checks value, existence, mode and provisional effects
// separately. It grants neither mutation authority nor a mode transition;
// phase and protection checks remain separate.
func (registry Registry) RequireWriteExclusion(owner stateauthority.Key, values []ManagedAddress, existence, modes []pathauthority.Exact, missing []pathauthority.Provisional) error {
	if !registry.initialized {
		return fmt.Errorf("ownership registry is required")
	}
	if err := owner.Validate(); err != nil {
		return fmt.Errorf("write owner: %w", err)
	}

	for _, address := range values {
		claims, err := registry.Overlapping(address)
		if err != nil {
			return err
		}
		for _, claim := range claims {
			if claim.Owner().Key() != owner {
				return &ForeignClaimError{Existing: claim}
			}
		}
	}

	for _, physical := range existence {
		whole, err := NewManagedAddress(physical, output.WholePathFootprint())
		if err != nil {
			return err
		}
		claims, err := registry.Overlapping(whole)
		if err != nil {
			return err
		}
		for _, claim := range claims {
			if claim.Owner().Key() != owner {
				return &ForeignClaimError{Existing: claim}
			}
		}
	}

	for _, physical := range modes {
		whole, err := NewManagedAddress(physical, output.WholePathFootprint())
		if err != nil {
			return err
		}
		claims, err := registry.Overlapping(whole)
		if err != nil {
			return err
		}
		for _, claim := range claims {
			if claim.Address().WholePath() && claim.Address().PathAuthority().Contains(physical) && claim.Owner().Key() != owner {
				return &ForeignClaimError{Existing: claim}
			}
		}
	}

	for _, candidate := range missing {
		claims, err := registry.ProvisionalAncestorClaims(candidate)
		if err != nil {
			return err
		}
		for _, claim := range claims {
			if claim.Owner().Key() != owner {
				return &ForeignClaimError{Existing: claim}
			}
		}
	}
	return nil
}

// ForeignClaimError identifies the blocker without inventing an exact request
// address for a provisional comparison.
type ForeignClaimError struct {
	Existing Claim
}

func (err *ForeignClaimError) Error() string {
	return fmt.Sprintf("write conflicts with footprint %s at %q owned by state authority %q", addressFootprintLabel(err.Existing.Address()), err.Existing.Address().Path(), err.Existing.Owner().StatefileKey())
}
