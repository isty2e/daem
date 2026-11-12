package carrierclaim

import (
	"context"
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
)

func (store Store) ReservePinTransitionIfCurrent(ctx context.Context, expected durablecarrier.GlobalCarrierClaims, transition durablecarrier.PinTransition) (durablecarrier.GlobalCarrierClaims, error) {
	next, err := expected.WithReservedPinTransition(transition)
	if err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	return store.commitPinTransition(ctx, expected, next)
}

func (store Store) CompletePinTransitionIfCurrent(ctx context.Context, expected durablecarrier.GlobalCarrierClaims, transition durablecarrier.PinTransition, observation observerelation.CorrelationResult) (durablecarrier.GlobalCarrierClaims, error) {
	next, err := expected.WithCompletedPinTransition(transition, observation)
	if err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	return store.commitPinTransition(ctx, expected, next)
}

func (store Store) commitPinTransition(ctx context.Context, expected, next durablecarrier.GlobalCarrierClaims) (durablecarrier.GlobalCarrierClaims, error) {
	if ctx == nil {
		return durablecarrier.GlobalCarrierClaims{}, fmt.Errorf("carrier claim registry context is required")
	}
	if err := ctx.Err(); err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	current, identity, exists, err := store.loadForCommit(ctx)
	if err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	if !current.registry.Equal(expected) {
		return durablecarrier.GlobalCarrierClaims{}, fmt.Errorf("carrier claim registry changed since confirmed observation")
	}
	if current.registry.Equal(next) {
		return current.registry, nil
	}
	if err := store.commitRegistry(ctx, current, next, nil, identity, exists); err != nil {
		return durablecarrier.GlobalCarrierClaims{}, err
	}
	return next, nil
}
