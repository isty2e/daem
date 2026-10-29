package ownership

import (
	"fmt"
	"sort"
)

// WithDocumentChanges adds exact fragment changes without weakening independent
// row semantics. Components must have disjoint mutation extents.
func (convergence ClaimConvergence) WithDocumentChanges(changes []DocumentClaimChange) (ClaimConvergence, error) {
	if err := convergence.Validate(); err != nil {
		return ClaimConvergence{}, err
	}
	documents := append(append([]DocumentClaimChange(nil), convergence.documents...), changes...)
	for index, change := range documents {
		if err := change.Validate(); err != nil {
			return ClaimConvergence{}, fmt.Errorf("document claim changes[%d]: %w", index, err)
		}
	}
	sort.Slice(documents, func(left, right int) bool {
		return documents[left].cover[0].Path() < documents[right].cover[0].Path()
	})
	addresses := make([]ManagedAddress, 0, len(convergence.changes))
	for _, change := range convergence.changes {
		addresses = append(addresses, change.address)
	}
	scopes := addressOverlapIndex{roots: make(map[string]*physicalAddressNode)}
	var owners []int
	for index, change := range documents {
		if index != 0 && change.cover[0].Path() == documents[index-1].cover[0].Path() {
			return ClaimConvergence{}, fmt.Errorf("document claim changes repeat a physical document")
		}
		for _, address := range change.cover {
			addresses = append(addresses, address)
			if _, overlap := scopes.insert(len(owners), address); overlap {
				return ClaimConvergence{}, fmt.Errorf("document claim change extents overlap")
			}
			owners = append(owners, index)
		}
	}
	if _, _, overlap := firstOverlappingAddress(addresses); overlap {
		return ClaimConvergence{}, fmt.Errorf("independent and document claim change extents overlap")
	}
	convergence.documents = documents
	convergence.documentScopes = scopes
	convergence.documentOwners = owners
	return convergence, nil
}

func (convergence ClaimConvergence) applyDocumentChanges(registry Registry) (Registry, bool, error) {
	independent := ClaimConvergence{changes: convergence.changes, initialized: true}
	current, changed, err := independent.Apply(registry)
	if err != nil {
		return Registry{}, false, err
	}
	fragments := make([][]Claim, len(convergence.documents))
	for _, claim := range current.claims {
		if scope, overlap := convergence.documentScopes.first(claim.Address()); overlap {
			owner := convergence.documentOwners[scope]
			fragments[owner] = append(fragments[owner], claim)
		}
	}
	updates := make(map[ManagedAddress]ClaimValue)
	for index, change := range convergence.documents {
		actual := fragments[index]
		if sameDocumentClaims(actual, change.target) {
			continue
		}
		for _, address := range change.cover {
			if err := current.RequireWritable(address.PathAuthority()); err != nil {
				return Registry{}, false, err
			}
		}
		if !sameDocumentClaims(actual, change.expected) {
			return Registry{}, false, documentClaimMismatch(change.expected, actual)
		}
		for _, claim := range change.expected {
			updates[claim.Address()] = NoClaim()
		}
		for _, claim := range change.target {
			updates[claim.Address()], _ = PresentClaim(claim)
		}
	}
	if len(updates) == 0 {
		return current, changed, nil
	}
	claims := make([]Claim, 0, len(current.claims)+len(updates))
	for _, claim := range current.claims {
		if _, replaced := updates[claim.Address()]; !replaced {
			claims = append(claims, claim)
		}
	}
	for _, value := range updates {
		if claim, present := value.Get(); present {
			claims = append(claims, claim)
		}
	}
	next, err := current.withFacts(claims, current.bindings)
	if err != nil {
		return Registry{}, false, err
	}
	return next, true, nil
}

func (convergence ClaimConvergence) documentRemovals() []Claim {
	var removals []Claim
	for _, change := range convergence.documents {
		targets := make(map[ManagedAddress]struct{}, len(change.target))
		for _, claim := range change.target {
			targets[claim.Address()] = struct{}{}
		}
		for _, claim := range change.expected {
			if _, retained := targets[claim.Address()]; !retained {
				removals = append(removals, claim)
			}
		}
	}
	return removals
}
