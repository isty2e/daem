package observe

import (
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

// NativeDocumentClaimObservation adds exact claim comparison to participant
// facts. Comparison may be retained history, never current path authority.
type NativeDocumentClaimObservation struct {
	participants  NativeDocumentParticipants
	comparison    pathauthority.Exact
	beforeClaims  []ownership.Claim
	currentClaims []ownership.Claim
	initialized   bool
}

func NewNativeDocumentClaimObservation(input NativeDocumentClaimObservationInput) (NativeDocumentClaimObservation, error) {
	whole, err := ownership.NewManagedAddress(input.Comparison, output.WholePathFootprint())
	if err != nil {
		return NativeDocumentClaimObservation{}, err
	}
	participants, err := NewNativeDocumentParticipants(NativeDocumentParticipantsInput{
		Owner: input.Owner, Aliases: input.Aliases, BeforeSubjects: input.BeforeSubjects, CurrentBindings: input.CurrentBindings,
	})
	if err != nil {
		return NativeDocumentClaimObservation{}, err
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
	for _, binding := range participants.CurrentBindings() {
		if !binding.PathAuthority().Equal(input.Comparison) {
			return NativeDocumentClaimObservation{}, fmt.Errorf("native binding fragment differs from its comparison namespace")
		}
	}
	return NativeDocumentClaimObservation{
		participants: participants, comparison: input.Comparison,
		beforeClaims: before, currentClaims: current, initialized: true,
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
	return observation.participants.Owner()
}

// ComparisonNamespace is for claim lookup, not current exact path authority.
func (observation NativeDocumentClaimObservation) ComparisonNamespace() pathauthority.Exact {
	return observation.comparison
}

func (observation NativeDocumentClaimObservation) Participants() NativeDocumentParticipants {
	return observation.participants
}

func (observation NativeDocumentClaimObservation) Aliases() []aggregate.DocumentAddress {
	return observation.participants.Aliases()
}

func (observation NativeDocumentClaimObservation) BeforeSubjects() []topology.SubjectID {
	return observation.participants.BeforeSubjects()
}

func (observation NativeDocumentClaimObservation) BeforeClaims() []ownership.Claim {
	return slices.Clone(observation.beforeClaims)
}

func (observation NativeDocumentClaimObservation) CurrentClaims() []ownership.Claim {
	return slices.Clone(observation.currentClaims)
}

func (observation NativeDocumentClaimObservation) CurrentBindings() []ownership.DocumentBinding {
	return observation.participants.CurrentBindings()
}
