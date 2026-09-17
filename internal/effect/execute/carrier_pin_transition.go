package execute

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func CommitReservedProjectPinTransition(ctx context.Context, filesystem mutationfs.RootedStore, authority *rootedpath.EntryAuthority, current durable.Snapshot, transition durablecarrier.PinTransition, encoder durable.SnapshotEncoder) (durable.Snapshot, error) {
	next, err := current.WithReservedCarrierPinTransition(transition)
	if err != nil {
		return current, err
	}
	if err := commitCarrierState(ctx, filesystem, authority, next, encoder, "reserved Pi pin transition"); err != nil {
		return current, err
	}
	return next, nil
}

func CommitCompletedProjectPinTransition(ctx context.Context, filesystem mutationfs.RootedStore, authority *rootedpath.EntryAuthority, current durable.Snapshot, transition durablecarrier.PinTransition, observation observerelation.CorrelationResult, encoder durable.SnapshotEncoder) (durable.Snapshot, error) {
	next, err := current.WithCompletedCarrierPinTransition(transition, observation)
	if err != nil {
		return current, err
	}
	if err := commitCarrierState(ctx, filesystem, authority, next, encoder, "completed Pi pin transition"); err != nil {
		return current, err
	}
	return next, nil
}
