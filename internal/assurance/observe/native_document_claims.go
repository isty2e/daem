package observe

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
)

type NativeDocumentClaimObservationInput struct {
	Owner           stateauthority.Authority
	Aliases         []aggregate.DocumentAddress
	Comparison      pathauthority.Exact
	BeforeSubjects  []topology.SubjectID
	BeforeClaims    []ownership.Claim
	CurrentClaims   []ownership.Claim
	CurrentBindings []ownership.DocumentBinding
}

// NativeDocumentClaimObservation is a read-only projection of a qualified
// physical group's membership, claims and stored binding participants.
// Comparison may be retained missing history, never current exact authority.
type NativeDocumentClaimObservation struct {
	owner           stateauthority.Authority
	aliases         []aggregate.DocumentAddress
	comparison      pathauthority.Exact
	beforeSubjects  []topology.SubjectID
	beforeClaims    []ownership.Claim
	currentClaims   []ownership.Claim
	currentBindings []ownership.DocumentBinding
	initialized     bool
}

func NewNativeDocumentClaimObservation(input NativeDocumentClaimObservationInput) (NativeDocumentClaimObservation, error) {
	if err := input.Owner.Validate(); err != nil {
		return NativeDocumentClaimObservation{}, err
	}
	whole, err := ownership.NewManagedAddress(input.Comparison, output.WholePathFootprint())
	if err != nil {
		return NativeDocumentClaimObservation{}, err
	}
	aliases := slices.Clone(input.Aliases)
	if len(aliases) == 0 {
		return NativeDocumentClaimObservation{}, fmt.Errorf("native claim observation requires aliases")
	}
	seen := make(map[aggregate.DocumentAddress]struct{}, len(aliases))
	for _, alias := range aliases {
		if err := alias.Validate(); err != nil {
			return NativeDocumentClaimObservation{}, err
		}
		if _, duplicate := seen[alias]; duplicate {
			return NativeDocumentClaimObservation{}, fmt.Errorf("native claim observation repeats an alias")
		}
		seen[alias] = struct{}{}
	}
	slices.SortFunc(aliases, func(left, right aggregate.DocumentAddress) int {
		for _, order := range []int{cmp.Compare(left.Target(), right.Target()), cmp.Compare(left.Scope(), right.Scope()), cmp.Compare(left.AggregateRoot().String(), right.AggregateRoot().String())} {
			if order != 0 {
				return order
			}
		}
		return 0
	})
	subjects := slices.Clone(input.BeforeSubjects)
	slices.SortFunc(subjects, topology.CompareSubjectID)
	for index, subject := range subjects {
		if err := subject.Validate(); err != nil {
			return NativeDocumentClaimObservation{}, err
		}
		if index != 0 && subjects[index-1] == subject {
			return NativeDocumentClaimObservation{}, fmt.Errorf("native claim observation repeats a before subject")
		}
	}
	current, err := canonicalObservedClaims(input.CurrentClaims, whole)
	if err != nil {
		return NativeDocumentClaimObservation{}, err
	}
	before, err := canonicalObservedClaims(input.BeforeClaims, whole)
	if err != nil {
		return NativeDocumentClaimObservation{}, err
	}
	for _, claim := range before {
		if !claim.OwnedBy(input.Owner) || claim.State() != ownership.ClaimActive || !claim.Address().PathAuthority().Equal(input.Comparison) || !slices.ContainsFunc(current, claim.Equal) {
			return NativeDocumentClaimObservation{}, fmt.Errorf("native before claims differ from active owned current history")
		}
	}
	bindings, err := ownership.NewRegistry(nil, input.CurrentBindings...)
	if err != nil {
		return NativeDocumentClaimObservation{}, err
	}
	for _, binding := range bindings.DocumentBindings() {
		if !binding.PathAuthority().Equal(input.Comparison) {
			return NativeDocumentClaimObservation{}, fmt.Errorf("native binding fragment differs from its comparison namespace")
		}
	}
	return NativeDocumentClaimObservation{
		owner: input.Owner, aliases: aliases, comparison: input.Comparison,
		beforeSubjects: subjects, beforeClaims: before, currentClaims: current, currentBindings: bindings.DocumentBindings(), initialized: true,
	}, nil
}

func canonicalObservedClaims(input []ownership.Claim, whole ownership.ManagedAddress) ([]ownership.Claim, error) {
	claims := slices.Clone(input)
	slices.SortFunc(claims, func(left, right ownership.Claim) int {
		if left.Address().Less(right.Address()) {
			return -1
		}
		if right.Address().Less(left.Address()) {
			return 1
		}
		return 0
	})
	for index, claim := range claims {
		if err := claim.Validate(); err != nil {
			return nil, err
		}
		if !claim.Address().Overlaps(whole) || index != 0 && claims[index-1].Address().Equal(claim.Address()) {
			return nil, fmt.Errorf("native claim fragment is unrelated or repeats an address")
		}
	}
	return claims, nil
}

func (observation NativeDocumentClaimObservation) Validate() error {
	if !observation.initialized {
		return fmt.Errorf("native document claim observation is required")
	}
	return nil
}

func (observation NativeDocumentClaimObservation) Owner() stateauthority.Authority {
	return observation.owner
}

// ComparisonNamespace is for claim lookup, not current exact path authority.
func (observation NativeDocumentClaimObservation) ComparisonNamespace() pathauthority.Exact {
	return observation.comparison
}

func (observation NativeDocumentClaimObservation) Aliases() []aggregate.DocumentAddress {
	return slices.Clone(observation.aliases)
}

func (observation NativeDocumentClaimObservation) BeforeSubjects() []topology.SubjectID {
	return slices.Clone(observation.beforeSubjects)
}

func (observation NativeDocumentClaimObservation) BeforeClaims() []ownership.Claim {
	return slices.Clone(observation.beforeClaims)
}

func (observation NativeDocumentClaimObservation) CurrentClaims() []ownership.Claim {
	return slices.Clone(observation.currentClaims)
}

func (observation NativeDocumentClaimObservation) CurrentBindings() []ownership.DocumentBinding {
	return slices.Clone(observation.currentBindings)
}
