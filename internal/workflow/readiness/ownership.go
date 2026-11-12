package readiness

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	ownershipobserve "github.com/isty2e/daem/internal/assurance/observe/ownership"
	"github.com/isty2e/daem/internal/output"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func buildOwnershipObservations(
	ctx context.Context,
	paths daempaths.Paths,
	resolver liveDestinationResolver,
	managedPaths []ownershipobserve.ManagedPathInput,
	aggregateProjections []aggregate.ProjectionAddress,
	currentState durable.Snapshot,
	selection targetselection.Selection,
) (ownershipobserve.Result, error) {
	registryStore, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		return ownershipobserve.Result{}, fmt.Errorf("open ownership registry: %w", err)
	}
	registry, err := registryStore.Load(ctx)
	if err != nil {
		return ownershipobserve.Result{}, err
	}
	result, err := ownershipobserve.Build(ownershipobserve.Input{
		Paths:           paths,
		Resolver:        ownershipobserve.DestinationResolver(resolver),
		ManagedPaths:    managedPaths,
		Aggregates:      aggregateProjections,
		StatePaths:      currentState.ManagedPaths(),
		StateAggregates: currentState.ManagedAggregates(),
		Selection:       selection,
		Registry:        registry,
	})
	if err != nil {
		return ownershipobserve.Result{}, err
	}
	return result, nil
}

type liveDestinationResolver func(output.Destination) (string, error)
