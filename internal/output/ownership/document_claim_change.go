package ownership

import (
	"fmt"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/output"
)

// DocumentClaimChange compares complete claim fragments, not independently
// retryable rows. Its two non-overlapping phases may overlap each other.
type DocumentClaimChange struct {
	expected    []Claim
	target      []Claim
	cover       []ManagedAddress
	initialized bool
}

func NewDocumentClaimChange(expected, target []Claim) (DocumentClaimChange, error) {
	if len(expected) == 0 && len(target) == 0 {
		return DocumentClaimChange{}, fmt.Errorf("document claim change requires a nonempty phase")
	}
	before, err := NewRegistry(expected)
	if err != nil {
		return DocumentClaimChange{}, fmt.Errorf("document claim expected phase: %w", err)
	}
	after, err := NewRegistry(target)
	if err != nil {
		return DocumentClaimChange{}, fmt.Errorf("document claim target phase: %w", err)
	}
	claims := append(before.Claims(), after.claims...)
	path := claims[0].Address().PathAuthority()
	owner := claims[0].Owner()
	for _, claim := range claims {
		if !claim.Address().PathAuthority().Equal(path) {
			return DocumentClaimChange{}, fmt.Errorf("document claim phases require one exact document")
		}
		if !claim.Owner().Equal(owner) {
			return DocumentClaimChange{}, fmt.Errorf("document claim phases require one state authority")
		}
	}

	return DocumentClaimChange{
		expected: before.claims, target: after.claims,
		cover: documentClaimCover(claims), initialized: true,
	}, nil
}

func (change DocumentClaimChange) Validate() error {
	if !change.initialized {
		return fmt.Errorf("document claim change is required")
	}
	return nil
}

func (change DocumentClaimChange) Expected() []Claim { return append([]Claim(nil), change.expected...) }

func (change DocumentClaimChange) Target() []Claim { return append([]Claim(nil), change.target...) }

// FootprintCover returns only input addresses that cover both phases. It does
// not introduce a common ancestor over otherwise disjoint claims.
func (change DocumentClaimChange) FootprintCover() []ManagedAddress {
	return append([]ManagedAddress(nil), change.cover...)
}

func documentClaimCover(claims []Claim) []ManagedAddress {
	type candidate struct {
		address ManagedAddress
		order   string
	}
	candidates := make([]candidate, len(claims))
	for index, claim := range claims {
		address := claim.Address()
		key, _ := address.Footprint().Key()
		order := key.String() + "/"
		if address.Footprint().Kind() == output.FootprintPath {
			order = ""
		}
		candidates[index] = candidate{address: address, order: order}
	}
	sort.Slice(candidates, func(left, right int) bool {
		return strings.Compare(candidates[left].order, candidates[right].order) < 0
	})
	cover := make([]ManagedAddress, 0, len(candidates))
	for _, candidate := range candidates {
		if len(cover) != 0 && cover[len(cover)-1].Footprint().Contains(candidate.address.Footprint()) {
			continue
		}
		cover = append(cover, candidate.address)
	}
	return cover
}

func sameDocumentClaims(left, right []Claim) bool {
	if len(left) != len(right) {
		return false
	}
	for index, claim := range left {
		if !claim.Equal(right[index]) {
			return false
		}
	}
	return true
}

func documentClaimMismatch(expected, actual []Claim) error {
	actualByAddress := make(map[ManagedAddress]Claim, len(actual))
	for _, claim := range actual {
		actualByAddress[claim.Address()] = claim
	}
	for _, claim := range expected {
		current, present := actualByAddress[claim.Address()]
		if present && current.Equal(claim) {
			delete(actualByAddress, claim.Address())
			continue
		}
		wanted, _ := PresentClaim(claim)
		observed := NoClaim()
		if present {
			observed, _ = PresentClaim(current)
		}
		return &StaleClaimError{Address: claim.Address(), Expected: wanted, Actual: observed}
	}
	for _, claim := range actual {
		if _, extra := actualByAddress[claim.Address()]; extra {
			observed, _ := PresentClaim(claim)
			return &StaleClaimError{Address: claim.Address(), Expected: NoClaim(), Actual: observed}
		}
	}
	return fmt.Errorf("document claim fragment differs from expected phase")
}
