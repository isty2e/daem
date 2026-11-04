package statefile

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

type aggregateStateRecord interface {
	canonical() (durable.ManagedAggregateState, error)
}

type snapshotDTO = snapshotDocumentDTO[managedAggregateV10DTO]

type managedAggregateV10DTO struct {
	Subject               subjectDTO `json:"subject"`
	PlacementID           string     `json:"placement_id"`
	Target                string     `json:"target"`
	Scope                 string     `json:"scope"`
	AggregateRoot         string     `json:"aggregate_root"`
	KeyPath               string     `json:"key_path"`
	MergeUnit             string     `json:"merge_unit"`
	Cardinality           string     `json:"cardinality"`
	SiblingRetention      string     `json:"sibling_retention"`
	SiblingPreservation   string     `json:"sibling_preservation"`
	Equivalence           string     `json:"equivalence"`
	CanonicalContribution string     `json:"canonical_contribution"`
	CodecContractID       string     `json:"codec_contract_id"`
	ComparedFields        []string   `json:"compared_fields"`
}

func (persisted managedAggregateV10DTO) canonical() (durable.ManagedAggregateState, error) {
	subject, err := persisted.Subject.canonical()
	if err != nil {
		return durable.ManagedAggregateState{}, err
	}
	if err := requireCanonicalStrings(persisted.ComparedFields, "compared_fields"); err != nil {
		return durable.ManagedAggregateState{}, err
	}

	root, err := output.Parse(persisted.AggregateRoot)
	if err != nil {
		return durable.ManagedAggregateState{}, err
	}
	key, err := output.ParseKeyPath(persisted.KeyPath)
	if err != nil {
		return durable.ManagedAggregateState{}, fmt.Errorf("key_path: %w", err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		return durable.ManagedAggregateState{}, fmt.Errorf("key_path: %w", err)
	}

	contract, err := aggregate.NewProjectionContract(aggregate.ProjectionContractInput{
		PlacementID: persisted.PlacementID, Target: target.Target(persisted.Target),
		Scope: target.Scope(persisted.Scope), AggregateRoot: root, Footprint: footprint,
		MergeUnit: aggregate.MergeUnit(persisted.MergeUnit), Cardinality: aggregate.ContributionCardinality(persisted.Cardinality),
		SiblingRetention: aggregate.SiblingRetention(persisted.SiblingRetention), SiblingPreservation: aggregate.SiblingPreservation(persisted.SiblingPreservation),
		Equivalence: aggregate.Equivalence(persisted.Equivalence), CodecContractID: aggregate.CodecContractID(persisted.CodecContractID),
		ComparedFields: persisted.ComparedFields,
	})
	if err != nil {
		return durable.ManagedAggregateState{}, err
	}

	contribution, err := aggregate.NewManagedContribution(contract, persisted.CanonicalContribution)
	if err != nil {
		return durable.ManagedAggregateState{}, err
	}
	return durable.NewManagedAggregateState(subject, contribution)
}
