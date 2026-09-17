package durable

import (
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
)

func (snapshot Snapshot) WithReservedCarrierPinTransition(transition durablecarrier.PinTransition) (Snapshot, error) {
	claims, err := transition.Reserve(snapshot.ManagedCarrierClaims())
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot.WithManagedCarrierClaims(claims)
}

func (snapshot Snapshot) WithCompletedCarrierPinTransition(transition durablecarrier.PinTransition, observation observerelation.CorrelationResult) (Snapshot, error) {
	claims, err := transition.Complete(snapshot.ManagedCarrierClaims(), observation)
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot.WithManagedCarrierClaims(claims)
}
