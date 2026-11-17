package reconcile

import (
	"fmt"
	"sort"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/topology"
)

// CarrierHandoffAcquisition associates an actual acquisition request with the
// old facts that must retire first. Its action is not a post-retirement action.
type CarrierHandoffAcquisition struct {
	action      RelationAction
	retirements []durablecarrier.CarrierFactKey
}

func (acquisition CarrierHandoffAcquisition) Action() RelationAction { return acquisition.action }

func (acquisition CarrierHandoffAcquisition) RetirementKeys() []durablecarrier.CarrierFactKey {
	return append([]durablecarrier.CarrierFactKey(nil), acquisition.retirements...)
}

type CarrierHandoffsInput struct {
	CurrentOwner    stateauthority.Authority
	Claims          []durablecarrier.ManagedCarrierClaim
	PendingRemovals []durablecarrier.PendingCarrierRemoval
	Acquisitions    []RelationAction
	Absences        []carrierabsence.Action
	Observations    relationobserve.Batch
}

// CarrierHandoffs is one planning-only retirement set, not durable intent or
// execution authority. All occurrence comparisons are confined to its input batch.
type CarrierHandoffs struct {
	retirements     []carrierabsence.Action
	acquisitions    []CarrierHandoffAcquisition
	prospective     relationobserve.Batch
	claims          []durablecarrier.ManagedCarrierClaim
	pendingRemovals []durablecarrier.PendingCarrierRemoval
}

func NewCarrierHandoffs(input CarrierHandoffsInput) (CarrierHandoffs, error) {
	if err := input.CurrentOwner.Validate(); err != nil {
		return CarrierHandoffs{}, fmt.Errorf("carrier handoff owner: %w", err)
	}
	claims := make(map[durablecarrier.CarrierFactKey]durablecarrier.ManagedCarrierClaim, len(input.Claims))
	owned := make(map[topology.SubjectID]durablecarrier.ManagedCarrierClaim)
	for _, claim := range input.Claims {
		if err := claim.Validate(); err != nil {
			return CarrierHandoffs{}, err
		}
		if previous, duplicate := claims[claim.FactKey()]; duplicate && !previous.ExactEqual(claim) {
			return CarrierHandoffs{}, fmt.Errorf("carrier handoff claims contradict one owner relation")
		}
		claims[claim.FactKey()] = claim
		if claim.Owner().Equal(input.CurrentOwner) {
			owned[claim.Identity().RelationSubject()] = claim
		}
	}

	var candidates []carrierabsence.Action
	bySubject := make(map[topology.SubjectID]int)
	byOccurrence := make(map[relationobserve.Occurrence][]int)
	for _, absence := range input.Absences {
		if err := absence.Validate(); err != nil {
			return CarrierHandoffs{}, err
		}
		if !absence.Claim().Owner().Equal(input.CurrentOwner) {
			return CarrierHandoffs{}, fmt.Errorf("carrier handoff absence belongs to another owner")
		}
		if absence.Desired() != carrierabsence.DesiredAbsent || !absence.RetiresClaim() {
			continue
		}
		claim, present := claims[absence.Claim().FactKey()]
		if !present || !claim.ExactEqual(absence.Claim()) {
			return CarrierHandoffs{}, fmt.Errorf("carrier handoff retirement has no exact current claim")
		}
		observed, present := absence.Observation()
		current, currentPresent := input.Observations.Correlation(observed.Key)
		if !present || !currentPresent || current.State() != observed.Result.State() {
			return CarrierHandoffs{}, fmt.Errorf("carrier handoff retirement correlation is missing or has a different current state")
		}
		if _, duplicate := bySubject[absence.Subject()]; duplicate {
			return CarrierHandoffs{}, fmt.Errorf("carrier handoff retirement is repeated")
		}
		index := len(candidates)
		bySubject[absence.Subject()] = index
		candidates = append(candidates, absence)
		for _, occurrence := range current.CoveredOccurrences() {
			byOccurrence[occurrence] = append(byOccurrence[occurrence], index)
		}
	}

	selected := make(map[int]struct{})
	seen := make(map[topology.SubjectID]struct{}, len(input.Acquisitions))
	var acquisitions []CarrierHandoffAcquisition
	for _, action := range input.Acquisitions {
		if err := validateRelationAction(action); err != nil {
			return CarrierHandoffs{}, err
		}
		if _, duplicate := seen[action.Subject()]; duplicate {
			return CarrierHandoffs{}, fmt.Errorf("carrier handoff acquisition is repeated")
		}
		seen[action.Subject()] = struct{}{}
		if _, pin := action.PinTransition(); pin {
			continue
		}
		if retained, present := owned[action.Subject()]; present &&
			retained.Identity().ExactEqual(action.CarrierIdentity()) && retained.InstallRequest().Equal(action.RouteRequest()) {
			continue
		}
		dependencies := make(map[int]struct{})
		if index, present := bySubject[action.Subject()]; present &&
			candidates[index].Claim().Identity().SourceChangeTo(action.CarrierIdentity()) {
			dependencies[index] = struct{}{}
		}
		key, err := relationobserve.NewCorrelationKey(action.Subject(), action.ExpectedRelation())
		if err != nil {
			return CarrierHandoffs{}, err
		}
		if current, present := input.Observations.Correlation(key); present {
			actual, _ := action.Correlation()
			if actual.State() != current.State() {
				return CarrierHandoffs{}, fmt.Errorf("carrier handoff acquisition correlation has a different current state")
			}
			for _, occurrence := range current.CoveredOccurrences() {
				for _, index := range byOccurrence[occurrence] {
					dependencies[index] = struct{}{}
				}
			}
		}
		if len(dependencies) == 0 {
			continue
		}
		var before []durablecarrier.ManagedCarrierClaim
		for index := range dependencies {
			selected[index] = struct{}{}
			before = append(before, candidates[index].Claim())
		}
		sort.Slice(before, func(left, right int) bool { return before[left].Compare(before[right]) < 0 })
		acquisition := CarrierHandoffAcquisition{action: action}
		for _, claim := range before {
			acquisition.retirements = append(acquisition.retirements, claim.FactKey())
		}
		acquisitions = append(acquisitions, acquisition)
	}

	handoffs := CarrierHandoffs{acquisitions: acquisitions}
	retiredFacts := make(map[durablecarrier.CarrierFactKey]struct{}, len(selected))
	for index := range selected {
		handoffs.retirements = append(handoffs.retirements, candidates[index])
		retiredFacts[candidates[index].Claim().FactKey()] = struct{}{}
	}
	for _, claim := range input.Claims {
		if _, retired := retiredFacts[claim.FactKey()]; !retired {
			handoffs.claims = append(handoffs.claims, claim)
		}
	}
	for _, pending := range input.PendingRemovals {
		if err := pending.Validate(); err != nil {
			return CarrierHandoffs{}, err
		}
		if _, retired := retiredFacts[pending.FactKey()]; !retired {
			handoffs.pendingRemovals = append(handoffs.pendingRemovals, pending)
		}
	}
	sort.Slice(handoffs.retirements, func(left, right int) bool {
		return handoffs.retirements[left].Compare(handoffs.retirements[right]) < 0
	})
	sort.Slice(handoffs.acquisitions, func(left, right int) bool {
		return handoffs.acquisitions[left].action.Compare(handoffs.acquisitions[right].action) < 0
	})
	var retiring []relationobserve.CorrelationKey
	for _, absence := range handoffs.retirements {
		observed, _ := absence.Observation()
		retiring = append(retiring, observed.Key)
	}
	if len(retiring) == 0 {
		handoffs.prospective = input.Observations.Clone()
		return handoffs, nil
	}
	prospective, err := input.Observations.WithoutRetiring(retiring)
	if err != nil {
		return CarrierHandoffs{}, err
	}
	handoffs.prospective = prospective
	return handoffs, nil
}

func (handoffs CarrierHandoffs) Empty() bool { return len(handoffs.retirements) == 0 }

func (handoffs CarrierHandoffs) Retirements() []carrierabsence.Action {
	return append([]carrierabsence.Action(nil), handoffs.retirements...)
}

func (handoffs CarrierHandoffs) Acquisitions() []CarrierHandoffAcquisition {
	return append([]CarrierHandoffAcquisition(nil), handoffs.acquisitions...)
}

func (handoffs CarrierHandoffs) ProspectiveObservations() relationobserve.Batch {
	return handoffs.prospective.Clone()
}

func (handoffs CarrierHandoffs) ProspectiveClaims() []durablecarrier.ManagedCarrierClaim {
	return append([]durablecarrier.ManagedCarrierClaim(nil), handoffs.claims...)
}

func (handoffs CarrierHandoffs) ProspectivePendingRemovals() []durablecarrier.PendingCarrierRemoval {
	return append([]durablecarrier.PendingCarrierRemoval(nil), handoffs.pendingRemovals...)
}
