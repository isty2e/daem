package hostroute

import (
	"fmt"
	"sort"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// CarrierRemovalRouteResolver selects an already-admitted operation-specific
// removal route. Returning UnavailableRoute is an honest supported outcome.
type CarrierRemovalRouteResolver func(
	durablecarrier.ManagedCarrierClaim,
) (carrierabsence.RouteAdmission, error)

// CarrierAbsenceInput separates current-authority candidates from the complete
// claim set used to derive daem-known occupancy.
type CarrierAbsenceInput struct {
	Locked          lock.File
	SelectedTargets reconcile.SelectedTargets
	Observations    relationobserve.Batch
	CurrentOwner    durablecarrier.StateAuthority
	AllClaims       []durablecarrier.ManagedCarrierClaim
	PendingRemovals []durablecarrier.PendingCarrierRemoval
	ResolveRoute    CarrierRemovalRouteResolver
}

// BuildCarrierAbsenceActions plans one pure decision for each exact managed
// claim owned by the selected state authority. It never treats another
// manifest's global claim or a pending install as a removal candidate.
func BuildCarrierAbsenceActions(input CarrierAbsenceInput) ([]carrierabsence.Action, error) {
	if err := input.CurrentOwner.Validate(); err != nil {
		return nil, fmt.Errorf("carrier absence current owner: %w", err)
	}
	records, err := selectedCarrierRelationRecords(input.Locked, input.SelectedTargets)
	if err != nil {
		return nil, err
	}
	claims, err := canonicalCarrierClaims(input.AllClaims)
	if err != nil {
		return nil, err
	}
	pendingByClaim, err := pendingCarrierRemovals(
		input.PendingRemovals,
		input.CurrentOwner,
		claims,
	)
	if err != nil {
		return nil, err
	}
	occupancies, err := carrierOccupancies(claims)
	if err != nil {
		return nil, err
	}

	actions := make([]carrierabsence.Action, 0)
	for index, claim := range claims {
		if !claim.Owner().Equal(input.CurrentOwner) ||
			!input.SelectedTargets.Contains(claim.Identity().Target()) {
			continue
		}
		desired := desiredStateForClaim(claim, records)
		actionInput := carrierabsence.ActionInput{
			Claim:     claim,
			Desired:   desired,
			Occupancy: occupancies[claim.Identity().Carrier()],
			Route:     carrierabsence.UnavailableRoute(),
		}
		if pending, present := pendingByClaim[carrierClaimKey(claim)]; present {
			actionInput.Pending = &pending
		}
		if desired == carrierabsence.DesiredAbsent {
			observation, err := observationForClaim(claim, input.Observations)
			if err != nil {
				return nil, fmt.Errorf("plan carrier absence observation[%d]: %w", index, err)
			}
			actionInput.Observation = observation
			if carrierabsence.ObservationAdmitsRouteResolution(
				claim.Identity(),
				observation.Result,
			) &&
				input.ResolveRoute != nil {
				route, err := input.ResolveRoute(claim)
				if err != nil {
					return nil, fmt.Errorf("resolve carrier absence route[%d]: %w", index, err)
				}
				actionInput.Route = route
			}
		}
		action, err := carrierabsence.NewAction(actionInput)
		if err != nil {
			return nil, fmt.Errorf("plan carrier absence claim[%d]: %w", index, err)
		}
		actions = append(actions, action)
	}
	sort.Slice(actions, func(left int, right int) bool {
		return actions[left].Compare(actions[right]) < 0
	})
	return actions, nil
}

type carrierOwnerRelationKey struct {
	statefileKey string
	subject      topology.SubjectID
}

func carrierClaimKey(claim durablecarrier.ManagedCarrierClaim) carrierOwnerRelationKey {
	return carrierOwnerRelationKey{
		statefileKey: claim.Owner().StatefileKey(),
		subject:      claim.Identity().RelationSubject(),
	}
}

func canonicalCarrierClaims(
	claims []durablecarrier.ManagedCarrierClaim,
) ([]durablecarrier.ManagedCarrierClaim, error) {
	canonical := make([]durablecarrier.ManagedCarrierClaim, 0, len(claims))
	byOwnerRelation := make(map[carrierOwnerRelationKey]durablecarrier.ManagedCarrierClaim, len(claims))
	for index, claim := range claims {
		if err := claim.Validate(); err != nil {
			return nil, fmt.Errorf("carrier absence claim[%d]: %w", index, err)
		}
		key := carrierClaimKey(claim)
		if existing, present := byOwnerRelation[key]; present {
			if existing.ExactEqual(claim) {
				continue
			}
			return nil, fmt.Errorf(
				"carrier absence claims conflict for owner %q relation %q",
				key.statefileKey,
				key.subject,
			)
		}
		byOwnerRelation[key] = claim
		canonical = append(canonical, claim)
	}
	sort.Slice(canonical, func(left int, right int) bool {
		if canonical[left].Owner().StatefileKey() != canonical[right].Owner().StatefileKey() {
			return canonical[left].Owner().StatefileKey() < canonical[right].Owner().StatefileKey()
		}
		return topology.CompareSubjectID(
			canonical[left].Identity().RelationSubject(),
			canonical[right].Identity().RelationSubject(),
		) < 0
	})
	return canonical, nil
}

func pendingCarrierRemovals(
	values []durablecarrier.PendingCarrierRemoval,
	currentOwner durablecarrier.StateAuthority,
	claims []durablecarrier.ManagedCarrierClaim,
) (map[carrierOwnerRelationKey]durablecarrier.PendingCarrierRemoval, error) {
	claimByKey := make(map[carrierOwnerRelationKey]durablecarrier.ManagedCarrierClaim, len(claims))
	for _, claim := range claims {
		claimByKey[carrierClaimKey(claim)] = claim
	}
	pendingByClaim := make(
		map[carrierOwnerRelationKey]durablecarrier.PendingCarrierRemoval,
		len(values),
	)
	for index, pending := range values {
		if err := pending.Validate(); err != nil {
			return nil, fmt.Errorf("carrier absence pending removal[%d]: %w", index, err)
		}
		if !pending.Owner().ExactEqual(currentOwner) {
			return nil, fmt.Errorf(
				"carrier absence pending removal[%d] belongs to a foreign state authority",
				index,
			)
		}
		key := carrierClaimKey(pending.Claim())
		claim, present := claimByKey[key]
		if !present || !claim.ExactEqual(pending.Claim()) {
			return nil, fmt.Errorf(
				"carrier absence pending removal[%d] has no exact active claim",
				index,
			)
		}
		if _, duplicate := pendingByClaim[key]; duplicate {
			return nil, fmt.Errorf(
				"carrier absence pending removal[%d] duplicates one owner relation",
				index,
			)
		}
		pendingByClaim[key] = pending
	}
	return pendingByClaim, nil
}

func carrierOccupancies(
	claims []durablecarrier.ManagedCarrierClaim,
) (map[extensiontopology.Carrier]durablecarrier.CarrierOccupancy, error) {
	byCarrier := make(map[extensiontopology.Carrier][]durablecarrier.ManagedCarrierClaim)
	for _, claim := range claims {
		carrier := claim.Identity().Carrier()
		byCarrier[carrier] = append(byCarrier[carrier], claim)
	}
	occupancies := make(map[extensiontopology.Carrier]durablecarrier.CarrierOccupancy, len(byCarrier))
	for carrier, carrierClaims := range byCarrier {
		occupancy, err := durablecarrier.NewCarrierOccupancy(carrier, carrierClaims)
		if err != nil {
			return nil, err
		}
		occupancies[carrier] = occupancy
	}
	return occupancies, nil
}

func desiredStateForClaim(
	claim durablecarrier.ManagedCarrierClaim,
	records []carrierRelationRecord,
) carrierabsence.DesiredRelationState {
	for _, item := range records {
		if claim.MatchesLockedRecord(item.contract) {
			return carrierabsence.DesiredRetained
		}
	}
	for _, item := range records {
		if item.contract.SubjectID() == claim.Identity().RelationSubject() {
			return carrierabsence.DesiredTransitionConflict
		}
	}
	return carrierabsence.DesiredAbsent
}

func observationForClaim(
	claim durablecarrier.ManagedCarrierClaim,
	observations relationobserve.Batch,
) (relationobserve.Correlation, error) {
	key, err := relationobserve.NewCorrelationKey(
		claim.Identity().RelationSubject(),
		claim.Identity().ExpectedRelation(),
	)
	if err != nil {
		return relationobserve.Correlation{}, err
	}
	if result, present := observations.Correlation(key); present {
		return relationobserve.Correlation{Key: key, Result: result}, nil
	}
	return relationobserve.Correlation{
		Key: key,
		Result: relationobserve.Correlate(
			claim.Identity().ExpectedRelation(),
			relationobserve.UnsupportedInventory(),
		),
	}, nil
}
