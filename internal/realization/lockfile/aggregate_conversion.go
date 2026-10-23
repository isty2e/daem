package lockfile

import (
	"fmt"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func aggregateRealizationFromDTO(body managedAggregateContributionDTO) (realization.RealizationSpec, error) {
	root, err := output.Parse(body.AggregateRoot)
	if err != nil {
		return realization.RealizationSpec{}, err
	}

	key, err := output.ParseKeyPath(body.KeyPath)
	if err != nil {
		return realization.RealizationSpec{}, fmt.Errorf("aggregate key_path: %w", err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		return realization.RealizationSpec{}, fmt.Errorf("aggregate key_path: %w", err)
	}

	contract, err := aggregate.NewProjectionContract(aggregate.ProjectionContractInput{
		PlacementID: body.PlacementID, Target: target.Target(body.Target), Scope: target.Scope(body.Scope),
		AggregateRoot: root, Footprint: footprint,
		MergeUnit: aggregate.MergeUnit(body.MergeUnit), SiblingRetention: aggregate.SiblingRetention(body.SiblingRetention),
		Cardinality:         aggregate.ContributionCardinality(body.ContributionCardinality),
		SiblingPreservation: aggregate.SiblingPreservation(body.SiblingPreservation),
		Equivalence:         aggregate.Equivalence(body.Equivalence),
		CodecContractID:     aggregate.CodecContractID(body.CodecContract), ComparedFields: body.ComparedFields,
	})
	if err != nil {
		return realization.RealizationSpec{}, err
	}

	contribution, err := aggregate.NewManagedContribution(contract, body.CanonicalContribution)
	if err != nil {
		return realization.RealizationSpec{}, err
	}
	return realization.NewManagedAggregateContribution(contribution)
}

func aggregateRealizationToDTO(body aggregate.ManagedContribution) managedAggregateContributionDTO {
	return managedAggregateContributionDTO{
		PlacementID: body.PlacementID(), Target: string(body.Target()), Scope: string(body.Scope()),
		AggregateRoot: body.AggregateRoot().String(), KeyPath: body.Address().KeyPath().String(), MergeUnit: string(body.MergeUnit()),
		ContributionCardinality: string(body.Cardinality()),
		SiblingRetention:        string(body.SiblingRetention()), SiblingPreservation: string(body.SiblingPreservation()),
		Equivalence: string(body.Equivalence()), CanonicalContribution: body.CanonicalContribution(),
		CodecContract: string(body.CodecContractID()), ComparedFields: body.ComparedFields(),
	}
}
