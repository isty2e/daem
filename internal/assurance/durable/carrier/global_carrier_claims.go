package carrier

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/target"
)

// GlobalCarrierClaims is the canonical daem-known global carrier claim set.
// It does not claim knowledge of ambient or manually managed consumers.
type GlobalCarrierClaims struct {
	claims []ManagedCarrierClaim
}

// NewGlobalCarrierClaims validates and canonically orders global claims.
func NewGlobalCarrierClaims(claims []ManagedCarrierClaim) (GlobalCarrierClaims, error) {
	canonical := append([]ManagedCarrierClaim(nil), claims...)
	seen := make(map[CarrierFactKey]struct{}, len(canonical))
	for index, claim := range canonical {
		if err := claim.Validate(); err != nil {
			return GlobalCarrierClaims{}, fmt.Errorf("global carrier claim[%d]: %w", index, err)
		}
		if claim.Identity().Scope() != target.ScopeGlobal {
			return GlobalCarrierClaims{}, fmt.Errorf(
				"global carrier claim[%d] requires global scope",
				index,
			)
		}
		key := claim.FactKey()
		if _, duplicate := seen[key]; duplicate {
			return GlobalCarrierClaims{}, fmt.Errorf(
				"global carrier claim[%d] duplicates one owner relation",
				index,
			)
		}
		seen[key] = struct{}{}
	}
	sort.Slice(canonical, func(left int, right int) bool {
		return canonical[left].Compare(canonical[right]) < 0
	})
	return GlobalCarrierClaims{claims: canonical}, nil
}

// EmptyGlobalCarrierClaims returns the canonical empty registry model.
func EmptyGlobalCarrierClaims() GlobalCarrierClaims {
	claims, err := NewGlobalCarrierClaims(nil)
	if err != nil {
		panic(err)
	}
	return claims
}

// Claims returns a defensive copy of daem-known global claims.
func (registry GlobalCarrierClaims) Claims() []ManagedCarrierClaim {
	return append([]ManagedCarrierClaim(nil), registry.claims...)
}

// Equal reports exact equality of canonical claim sets.
func (registry GlobalCarrierClaims) Equal(other GlobalCarrierClaims) bool {
	if len(registry.claims) != len(other.claims) {
		return false
	}
	for index := range registry.claims {
		if !registry.claims[index].ExactEqual(other.claims[index]) {
			return false
		}
	}
	return true
}

// WithClaim upserts one exact claim. A different claim for the same owner and
// relation is a contradiction rather than an update.
func (registry GlobalCarrierClaims) WithClaim(
	claim ManagedCarrierClaim,
) (GlobalCarrierClaims, bool, error) {
	return registry.WithClaims([]ManagedCarrierClaim{claim})
}

// WithClaims atomically upserts one canonical batch. Duplicate batch keys and
// retained owner-relation contradictions are rejected rather than ordered.
func (registry GlobalCarrierClaims) WithClaims(
	claims []ManagedCarrierClaim,
) (GlobalCarrierClaims, bool, error) {
	next := registry.Claims()
	seen := make(map[CarrierFactKey]struct{}, len(claims))
	changed := false
	for index, claim := range claims {
		if err := claim.Validate(); err != nil {
			return GlobalCarrierClaims{}, false, fmt.Errorf(
				"global carrier claim[%d]: %w",
				index,
				err,
			)
		}
		if claim.Identity().Scope() != target.ScopeGlobal {
			return GlobalCarrierClaims{}, false, fmt.Errorf(
				"global carrier claim[%d] requires global scope",
				index,
			)
		}
		key := claim.FactKey()
		if _, duplicate := seen[key]; duplicate {
			return GlobalCarrierClaims{}, false, fmt.Errorf(
				"global carrier claim[%d] duplicates one owner relation",
				index,
			)
		}
		seen[key] = struct{}{}
		matched := false
		for _, existing := range next {
			if existing.FactKey() != key {
				continue
			}
			matched = true
			if !existing.ExactEqual(claim) {
				return GlobalCarrierClaims{}, false, fmt.Errorf(
					"global carrier claim conflicts with retained owner relation",
				)
			}
			break
		}
		if matched {
			continue
		}
		next = append(next, claim)
		changed = true
	}
	if !changed {
		return registry, false, nil
	}
	result, err := NewGlobalCarrierClaims(next)
	if err != nil {
		return GlobalCarrierClaims{}, false, err
	}
	return result, true, nil
}

// RetireClaims derives one exact all-or-nothing retirement batch. Every
// requested claim must exist exactly; input order does not affect the canonical
// successor or conflict precedence.
func (registry GlobalCarrierClaims) RetireClaims(
	claims []ManagedCarrierClaim,
) (GlobalCarrierClaims, error) {
	if len(claims) == 0 {
		return registry, nil
	}
	canonical := append([]ManagedCarrierClaim(nil), claims...)
	for index, claim := range canonical {
		if err := claim.Validate(); err != nil {
			return GlobalCarrierClaims{}, fmt.Errorf(
				"global carrier retirement claim[%d]: %w",
				index,
				err,
			)
		}
		if claim.Identity().Scope() != target.ScopeGlobal {
			return GlobalCarrierClaims{}, fmt.Errorf(
				"global carrier retirement claim[%d] requires global scope",
				index,
			)
		}
	}
	sort.Slice(canonical, func(left int, right int) bool {
		return canonical[left].Compare(canonical[right]) < 0
	})

	retirements := make(map[CarrierFactKey]ManagedCarrierClaim, len(canonical))
	for _, claim := range canonical {
		key := claim.FactKey()
		if previous, duplicate := retirements[key]; duplicate {
			if previous.ExactEqual(claim) {
				return GlobalCarrierClaims{}, fmt.Errorf(
					"global carrier retirement duplicates one exact claim",
				)
			}
			return GlobalCarrierClaims{}, fmt.Errorf(
				"global carrier retirement conflicts within one owner relation",
			)
		}
		retirements[key] = claim
	}

	current := make(map[CarrierFactKey]ManagedCarrierClaim, len(registry.claims))
	for _, claim := range registry.claims {
		current[claim.FactKey()] = claim
	}
	for _, retirement := range canonical {
		retained, present := current[retirement.FactKey()]
		if !present {
			return GlobalCarrierClaims{}, fmt.Errorf(
				"global carrier retirement exact claim is absent",
			)
		}
		if !retained.ExactEqual(retirement) {
			return GlobalCarrierClaims{}, fmt.Errorf(
				"global carrier retirement conflicts with retained owner relation",
			)
		}
	}

	next := make([]ManagedCarrierClaim, 0, len(registry.claims)-len(canonical))
	for _, claim := range registry.claims {
		if _, retire := retirements[claim.FactKey()]; retire {
			continue
		}
		next = append(next, claim)
	}
	return NewGlobalCarrierClaims(next)
}

// WithoutClaim removes only one exact claim. Absence is idempotent; a
// different claim for the same owner relation is a contradiction.
func (registry GlobalCarrierClaims) WithoutClaim(
	claim ManagedCarrierClaim,
) (GlobalCarrierClaims, bool, error) {
	if err := claim.Validate(); err != nil {
		return GlobalCarrierClaims{}, false, err
	}
	if claim.Identity().Scope() != target.ScopeGlobal {
		return GlobalCarrierClaims{}, false, fmt.Errorf("global carrier registry requires global scope")
	}
	key := claim.FactKey()
	next := registry.Claims()
	for index, existing := range next {
		if existing.FactKey() != key {
			continue
		}
		if !existing.ExactEqual(claim) {
			return GlobalCarrierClaims{}, false, fmt.Errorf(
				"global carrier claim conflicts with retained owner relation",
			)
		}
		next = append(next[:index], next[index+1:]...)
		result, err := NewGlobalCarrierClaims(next)
		if err != nil {
			return GlobalCarrierClaims{}, false, err
		}
		return result, true, nil
	}
	return registry, false, nil
}
