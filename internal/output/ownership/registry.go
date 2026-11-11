package ownership

import (
	"errors"
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

// ErrClaimMissing identifies prior managed state without its own active claim.
var ErrClaimMissing = errors.New("managed state has no matching own active ownership claim")

// Registry retains claims, document identity evidence and pending protection.
// Claim exclusion and document-wide safety fences remain distinct.
type Registry struct {
	claims         []Claim
	bindings       []DocumentBinding
	protections    []PendingProtection
	bindingIndices map[documentBindingKey]int
	overlaps       addressOverlapIndex
	initialized    bool
}

// NewRegistry validates, copies, and orders claims and optional document bindings.
func NewRegistry(claims []Claim, bindings ...DocumentBinding) (Registry, error) {
	normalized := append([]Claim(nil), claims...)
	for index, claim := range normalized {
		if err := claim.Validate(); err != nil {
			return Registry{}, fmt.Errorf("ownership claim[%d]: %w", index, err)
		}
	}
	sort.Slice(normalized, func(left int, right int) bool {
		return normalized[left].Address().Less(normalized[right].Address())
	})
	canonicalBindings, bindingIndices, err := canonicalDocumentBindings(bindings)
	if err != nil {
		return Registry{}, err
	}
	overlaps := addressOverlapIndex{}
	if len(normalized) != 0 {
		overlaps.roots = make(map[string]*physicalAddressNode)
		for requested, claim := range normalized {
			if existing, overlap := overlaps.insert(requested, claim.Address()); overlap {
				return Registry{}, &ConflictError{
					Existing:  normalized[existing],
					Requested: claim.Address(),
				}
			}
		}
	}
	return Registry{claims: normalized, bindings: canonicalBindings, bindingIndices: bindingIndices, overlaps: overlaps, initialized: true}, nil
}

// EmptyRegistry returns a valid registry with no retained facts.
func EmptyRegistry() Registry {
	return Registry{claims: []Claim{}, initialized: true}
}

// Claims returns a defensive copy in canonical order.
func (registry Registry) Claims() []Claim {
	return append([]Claim(nil), registry.claims...)
}

// Equal reports semantic equality between two canonical registries.
func (registry Registry) Equal(other Registry) bool {
	if !registry.initialized || !other.initialized || len(registry.claims) != len(other.claims) || len(registry.bindings) != len(other.bindings) || len(registry.protections) != len(other.protections) {
		return false
	}
	for index := range registry.claims {
		if !registry.claims[index].Equal(other.claims[index]) {
			return false
		}
	}
	for index := range registry.bindings {
		if !registry.bindings[index].Equal(other.bindings[index]) {
			return false
		}
	}
	for index := range registry.protections {
		if !registry.protections[index].Equal(other.protections[index]) {
			return false
		}
	}
	return true
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

// Conflict returns the first canonically ordered claim possibly overlapping address.
func (registry Registry) Conflict(address ManagedAddress) (Claim, bool) {
	if address.Validate() != nil {
		return Claim{}, false
	}
	if index, found := registry.overlaps.first(address); found {
		return registry.claims[index], true
	}
	return Claim{}, false
}

// ProvisionalAncestorConflict returns the first exact claim whose physical
// footprint contains a provisional candidate. It never treats the candidate
// itself as exact authority.
func (registry Registry) ProvisionalAncestorConflict(candidate pathauthority.Provisional) (Claim, bool) {
	if candidate.Validate() != nil {
		return Claim{}, false
	}
	if index, found := registry.overlaps.firstProvisional(candidate); found {
		return registry.claims[index], true
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
	if err := registry.RequireWritable(address.PathAuthority()); err != nil {
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
	return registry.withFacts(next, registry.bindings)
}

// ConflictError reports an overlapping claim owned by an existing authority.
type ConflictError struct {
	Existing  Claim
	Requested ManagedAddress
}

func (err *ConflictError) Error() string {
	return fmt.Sprintf(
		"managed address %q footprint %s overlaps claim owned by state authority %q",
		err.Requested.Path(),
		addressFootprintLabel(err.Requested),
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
	return fmt.Sprintf("managed address %q footprint %s claim changed", err.Address.Path(), addressFootprintLabel(err.Address))
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
