package ownership

import (
	"fmt"
	"sort"
)

// ClaimChange describes one exact expected-to-target registry fact.
type ClaimChange struct {
	address  ManagedAddress
	expected ClaimValue
	target   ClaimValue
}

// NewClaimChange constructs one exact registry convergence fact.
func NewClaimChange(address ManagedAddress, expected ClaimValue, target ClaimValue) (ClaimChange, error) {
	change := ClaimChange{address: address, expected: expected, target: target}
	if err := change.validate(); err != nil {
		return ClaimChange{}, err
	}
	return change, nil
}

func (change ClaimChange) validate() error {
	if err := change.address.Validate(); err != nil {
		return fmt.Errorf("ownership claim change address: %w", err)
	}
	if err := validateClaimValueAddress("expected", change.address, change.expected); err != nil {
		return err
	}
	if err := validateClaimValueAddress("target", change.address, change.target); err != nil {
		return err
	}
	return nil
}

// ClaimConvergence is a canonical, non-overlapping set of exact claim changes.
// Each address may currently contain either its expected or target value.
type ClaimConvergence struct {
	changes     []ClaimChange
	initialized bool
}

// NewClaimConvergence validates, copies, and canonically orders exact changes.
func NewClaimConvergence(changes []ClaimChange) (ClaimConvergence, error) {
	canonical := append([]ClaimChange(nil), changes...)
	for index, change := range canonical {
		if err := change.validate(); err != nil {
			return ClaimConvergence{}, fmt.Errorf("ownership claim convergence change[%d]: %w", index, err)
		}
	}
	sort.Slice(canonical, func(left int, right int) bool {
		return canonical[left].address.Less(canonical[right].address)
	})
	addresses := make([]ManagedAddress, len(canonical))
	for index, change := range canonical {
		addresses[index] = change.address
	}
	if left, right, overlap := firstOverlappingAddress(addresses); overlap {
		return ClaimConvergence{}, fmt.Errorf(
			"ownership claim convergence changes[%d] and [%d] overlap",
			left,
			right,
		)
	}
	return ClaimConvergence{changes: canonical, initialized: true}, nil
}

// Validate checks that the immutable convergence was constructed successfully.
func (convergence ClaimConvergence) Validate() error {
	if !convergence.initialized {
		return fmt.Errorf("ownership claim convergence is required")
	}
	return nil
}

// ExpectedRemovals returns exact claims whose target is absence. Stores use
// these facts to admit deletion-aware path-authority validation.
func (convergence ClaimConvergence) ExpectedRemovals() []Claim {
	removals := make([]Claim, 0)
	for _, change := range convergence.changes {
		expected, expectedPresent := change.expected.Get()
		_, targetPresent := change.target.Get()
		if expectedPresent && !targetPresent {
			removals = append(removals, expected)
		}
	}
	return removals
}

// Apply derives one deterministic successor. A row already at its target is
// idempotent; any third value is stale. No partial successor is returned.
func (convergence ClaimConvergence) Apply(registry Registry) (Registry, bool, error) {
	if err := convergence.Validate(); err != nil {
		return Registry{}, false, err
	}
	if !registry.initialized {
		return Registry{}, false, fmt.Errorf("ownership registry is required")
	}

	currentByAddress := make(map[ManagedAddress]Claim, len(registry.claims))
	overlapIndex := addressOverlapIndex{roots: make(map[string]*physicalAddressNode)}
	for index, claim := range registry.claims {
		currentByAddress[claim.Address()] = claim
		if _, overlap := overlapIndex.insert(index, claim.Address()); overlap {
			return Registry{}, false, fmt.Errorf("ownership registry contains overlapping claims")
		}
	}
	changedAddresses := make(map[ManagedAddress]ClaimValue, len(convergence.changes))
	for _, change := range convergence.changes {
		actual := NoClaim()
		if claim, present := currentByAddress[change.address]; present {
			actual, _ = PresentClaim(claim)
		}
		if actual.Equal(change.target) {
			if _, targetPresent := change.target.Get(); targetPresent {
				continue
			}
			if conflictIndex, present := overlapIndex.first(change.address); !present {
				continue
			} else {
				actual, _ = PresentClaim(registry.claims[conflictIndex])
			}
		}
		if !actual.Equal(change.expected) {
			if _, actualPresent := actual.Get(); !actualPresent {
				if conflictIndex, present := overlapIndex.first(change.address); present {
					actual, _ = PresentClaim(registry.claims[conflictIndex])
				}
			}
			return Registry{}, false, &StaleClaimError{
				Address:  change.address,
				Expected: change.expected,
				Actual:   actual,
			}
		}
		changedAddresses[change.address] = change.target
	}
	if len(changedAddresses) == 0 {
		return registry, false, nil
	}

	nextClaims := make([]Claim, 0, len(registry.claims)+len(changedAddresses))
	for _, claim := range registry.claims {
		if _, changed := changedAddresses[claim.Address()]; !changed {
			nextClaims = append(nextClaims, claim)
		}
	}
	for _, change := range convergence.changes {
		target, present := changedAddresses[change.address]
		if !present {
			continue
		}
		if claim, present := target.Get(); present {
			nextClaims = append(nextClaims, claim)
		}
	}
	next, err := NewRegistry(nextClaims)
	if err != nil {
		return Registry{}, false, err
	}
	return next, true, nil
}
