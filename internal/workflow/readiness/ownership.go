package readiness

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	ownershipobserve "github.com/isty2e/daem/internal/assurance/observe/ownership"
	"github.com/isty2e/daem/internal/output"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
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
) (outputownership.OwnerAuthority, []observe.OwnershipObservation, error) {
	registryStore, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		return outputownership.OwnerAuthority{}, nil, fmt.Errorf("open ownership registry: %w", err)
	}
	registry, err := registryStore.Load(ctx)
	if err != nil {
		return outputownership.OwnerAuthority{}, nil, err
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
		return outputownership.OwnerAuthority{}, nil, err
	}
	return result.Owner, result.Observations, nil
}

type liveDestinationResolver func(output.Destination) (string, error)
