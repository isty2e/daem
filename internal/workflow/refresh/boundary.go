package refresh

import (
	"context"
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

// PlanOptions supplies boundary implementations without weakening canonical
// refresh selection or authorization.
type PlanOptions struct {
	CommandBuilder CommandBuilder
	Observer       RelationObserver
}

func withPlanDefaults(options PlanOptions) PlanOptions {
	if options.CommandBuilder == nil {
		options.CommandBuilder = defaultCommandBuilder
	}
	if options.Observer == nil {
		options.Observer = defaultRelationObserver
	}
	return options
}

func defaultCommandBuilder(input CommandBuildInput) (CommandSpec, error) {
	command, err := executehostroute.BuildOperationCommand(executehostroute.OperationBuildInput{
		Contract:  input.Contract,
		Operation: input.Operation,
		WorkDir:   input.WorkDir,
	})
	if err != nil {
		return CommandSpec{}, err
	}
	disclosure, ok := command.Disclosure()
	if !ok {
		return CommandSpec{}, fmt.Errorf("refresh route adapter has no complete effect disclosure")
	}
	return NewCommandSpec(command.AttemptRequest(), disclosure)
}

func defaultRelationObserver(
	ctx context.Context,
	input ObservationRequest,
) (RelationObservation, error) {
	selection, err := targetselection.ForAvailableTargets(
		[]target.Target{input.Target},
		[]string{string(input.Target)},
	)
	if err != nil {
		return RelationObservation{}, err
	}
	contract, ok := input.Lockfile.Locked.Subject(input.Subject)
	if !ok {
		return RelationObservation{}, fmt.Errorf("refresh observation subject is not locked")
	}
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contract)
	if err != nil {
		return RelationObservation{}, err
	}
	if !admitted {
		return RelationObservation{}, fmt.Errorf("refresh observation subject is not an admitted carrier relation")
	}
	onlyCorrelation, err := relationobserve.NewCorrelationKey(
		identity.RelationSubject(),
		identity.ExpectedRelation(),
	)
	if err != nil {
		return RelationObservation{}, err
	}
	carrierClaimsStore, err := carrierclaimstore.New(input.Paths.CarrierClaimRegistryPath)
	if err != nil {
		return RelationObservation{}, err
	}
	globalClaims, err := carrierClaimsStore.LoadForSelectedAuthority(
		ctx,
		input.Paths.StatefilePath,
		input.Paths.ManifestPath,
	)
	if err != nil {
		return RelationObservation{}, err
	}
	allClaims := append(input.CurrentState.ManagedCarrierClaims(), globalClaims.Claims()...)
	batch, err := relationhost.Observe(ctx, relationhost.Input{
		Paths:                input.Paths,
		Lockfile:             input.Lockfile,
		ManagedCarrierClaims: allClaims,
		Selection:            selection,
		OnlyCorrelation:      &onlyCorrelation,
	})
	if err != nil {
		return RelationObservation{}, err
	}
	result, present := batch.Correlation(onlyCorrelation)
	return RelationObservation{
		Result:         result,
		Present:        present,
		AuthorityPaths: batch.AuthorityPaths(),
	}, nil
}
