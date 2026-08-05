package ownership

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

// Registry is a canonical, overlap-free set of durable ownership claims.
type Registry struct {
	claims      []Claim
	initialized bool
}

// NewRegistry validates, copies, and canonically orders claims.
func NewRegistry(claims []Claim) (Registry, error) {
	normalized := append([]Claim(nil), claims...)
	for index, claim := range normalized {
		if err := claim.Validate(); err != nil {
			return Registry{}, fmt.Errorf("ownership claim[%d]: %w", index, err)
		}
	}
	sort.Slice(normalized, func(left int, right int) bool {
		return normalized[left].Address().Less(normalized[right].Address())
	})
	addresses := make([]ManagedAddress, len(normalized))
	for index, claim := range normalized {
		addresses[index] = claim.Address()
	}
	if existing, requested, overlap := firstOverlappingAddress(addresses); overlap {
		return Registry{}, &ConflictError{
			Existing:  normalized[existing],
			Requested: normalized[requested].Address(),
		}
	}
	return Registry{claims: normalized, initialized: true}, nil
}

// EmptyRegistry returns a valid registry with no claims.
func EmptyRegistry() Registry {
	return Registry{claims: []Claim{}, initialized: true}
}

// Claims returns a defensive copy in canonical order.
func (registry Registry) Claims() []Claim {
	return append([]Claim(nil), registry.claims...)
}

// Exact returns the claim for an exact address.
func (registry Registry) Exact(address ManagedAddress) (Claim, bool) {
	for _, claim := range registry.claims {
		if claim.Address().Equal(address) {
			return claim, true
		}
	}
	return Claim{}, false
}

// Conflict returns the first canonically ordered claim overlapping address.
func (registry Registry) Conflict(address ManagedAddress) (Claim, bool) {
	for _, claim := range registry.claims {
		if claim.Address().Overlaps(address) {
			return claim, true
		}
	}
	return Claim{}, false
}

// ProvisionalAncestorConflict returns the first exact claim whose physical
// footprint contains a provisional candidate. It never treats the candidate
// itself as exact authority.
func (registry Registry) ProvisionalAncestorConflict(candidate pathauthority.Provisional) (Claim, bool) {
	for _, claim := range registry.claims {
		if candidate.CandidateWithin(claim.Address().PathAuthority()) {
			return claim, true
		}
	}
	return Claim{}, false
}

// Apply returns a new registry after an exact expected-before transition.
// It never changes claims outside address.
func (registry Registry) Apply(address ManagedAddress, expected ClaimValue, replacement ClaimValue) (Registry, error) {
	if !registry.initialized {
		return Registry{}, fmt.Errorf("ownership registry is required")
	}
	if err := address.Validate(); err != nil {
		return Registry{}, err
	}
	if err := validateClaimValueAddress("expected", address, expected); err != nil {
		return Registry{}, err
	}
	if err := validateClaimValueAddress("replacement", address, replacement); err != nil {
		return Registry{}, err
	}

	current, present := registry.Exact(address)
	currentValue := NoClaim()
	if present {
		currentValue, _ = PresentClaim(current)
	}
	if !currentValue.Equal(expected) {
		return Registry{}, &StaleClaimError{Address: address, Expected: expected, Actual: currentValue}
	}

	next := make([]Claim, 0, len(registry.claims)+1)
	for _, claim := range registry.claims {
		if !claim.Address().Equal(address) {
			next = append(next, claim)
		}
	}
	if claim, ok := replacement.Get(); ok {
		for _, existing := range next {
			if existing.ConflictsWith(claim) {
				return Registry{}, &ConflictError{Existing: existing, Requested: address}
			}
		}
		next = append(next, claim)
	}
	return NewRegistry(next)
}

// ConflictError reports an overlapping claim owned by an existing authority.
type ConflictError struct {
	Existing  Claim
	Requested ManagedAddress
}

func (err *ConflictError) Error() string {
	return fmt.Sprintf(
		"managed address %q content path %q overlaps claim owned by state authority %q",
		err.Requested.Path(),
		err.Requested.ContentPath(),
		err.Existing.Owner().StatefileKey(),
	)
}

// StaleClaimError reports that an exact expected transition no longer matches.
type StaleClaimError struct {
	Address  ManagedAddress
	Expected ClaimValue
	Actual   ClaimValue
}

func (err *StaleClaimError) Error() string {
	return fmt.Sprintf("managed address %q content path %q claim changed", err.Address.Path(), err.Address.ContentPath())
}

func validateClaimValueAddress(name string, address ManagedAddress, value ClaimValue) error {
	claim, present := value.Get()
	if !present {
		return nil
	}
	if err := claim.Validate(); err != nil {
		return fmt.Errorf("%s claim: %w", name, err)
	}
	if !claim.Address().Equal(address) {
		return fmt.Errorf("%s claim address does not match transition address", name)
	}
	return nil
}
