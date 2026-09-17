package hostroute

import (
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
)

func pinTransitionRelationAction(input RelationInput, item carrierRelationRecord, records []carrierRelationRecord, ordinary reconciliation.RelationAction) (reconciliation.RelationAction, error) {
	for _, claim := range input.ManagedClaims {
		if !claim.Owner().Equal(input.CurrentOwner) || claim.Identity().RelationSubject() != ordinary.Subject() {
			continue
		}
		transition, err := claim.PinTransitionTo(item.contract)
		if err != nil {
			if claim.RequireStable() != nil {
				return ordinary.BlockForPinTransition(), nil
			}
			continue
		}
		key, err := relationobserve.NewCorrelationKey(claim.Identity().RelationSubject(), claim.Identity().ExpectedRelation())
		if err != nil {
			return reconciliation.RelationAction{}, err
		}
		before, found := input.Observations.Correlation(key)
		if !found {
			before = relationobserve.Correlate(claim.Identity().ExpectedRelation(), relationobserve.UnsupportedInventory())
		}
		after, _ := ordinary.Correlation()
		return reconciliation.NewPinTransitionAction(reconciliation.PinTransitionActionInput{
			Transition: transition, CurrentClaim: claim,
			BeforeCorrelation: before, AfterCorrelation: after,
			RouteAdmission: ordinary.RouteAdmission(),
			Exclusive:      pinTransitionExclusive(input, records, transition),
		})
	}

	for _, claim := range input.ManagedClaims {
		if claim.RequireStable() != nil && durablecarrier.SharesPiGitFootprint(claim.Identity(), ordinary.CarrierIdentity()) {
			return ordinary.BlockForPinTransition(), nil
		}
	}
	return ordinary, nil
}

func pinTransitionExclusive(input RelationInput, records []carrierRelationRecord, transition durablecarrier.PinTransition) bool {
	before := transition.Before()
	if _, err := transition.Reserve(input.ManagedClaims); err != nil {
		return false
	}
	for _, pending := range input.PendingInstalls {
		if durablecarrier.SharesPiGitFootprint(pending.Identity(), transition.Identity()) {
			return false
		}
	}
	for _, pending := range input.PendingRemovals {
		if durablecarrier.SharesPiGitFootprint(pending.Identity(), transition.Identity()) {
			return false
		}
	}
	for _, item := range records {
		if item.contract.SubjectID() == before.Identity().RelationSubject() {
			continue
		}
		identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(item.contract)
		if err != nil || !admitted || durablecarrier.SharesPiGitFootprint(identity, transition.Identity()) {
			return false
		}
	}
	return true
}
