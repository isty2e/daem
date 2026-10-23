package aggregate

import (
	"fmt"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

// ProjectionContractInput supplies static projection facts without desired content.
type ProjectionContractInput struct {
	PlacementID         string
	Target              target.Target
	Scope               target.Scope
	AggregateRoot       output.Destination
	Footprint           output.Footprint
	MergeUnit           MergeUnit
	Cardinality         ContributionCardinality
	SiblingRetention    SiblingRetention
	SiblingPreservation SiblingPreservation
	Equivalence         Equivalence
	CodecContractID     CodecContractID
	ComparedFields      []string
}

// NewProjectionContract admits a native key scope independently of desired content.
func NewProjectionContract(input ProjectionContractInput) (ProjectionContract, error) {
	address, err := newProjectionAddress(
		input.PlacementID, input.Target, input.Scope, input.AggregateRoot,
		input.MergeUnit, input.Footprint,
	)
	if err != nil {
		return ProjectionContract{}, fmt.Errorf("managed aggregate contribution: %w", err)
	}
	contract := ProjectionContract{
		address:             address,
		cardinality:         input.Cardinality,
		siblingRetention:    input.SiblingRetention,
		siblingPreservation: input.SiblingPreservation,
		equivalence:         input.Equivalence,
		codecContractID:     input.CodecContractID,
		comparedFields:      canonicalTokenSet(input.ComparedFields),
	}
	if err := contract.Validate(); err != nil {
		return ProjectionContract{}, err
	}
	return contract, nil
}

func newProjectionAddress(
	placementID string,
	selectedTarget target.Target,
	scope target.Scope,
	aggregateRoot output.Destination,
	mergeUnit MergeUnit,
	footprint output.Footprint,
) (ProjectionAddress, error) {
	if err := validateToken("aggregate placement id", placementID); err != nil {
		return ProjectionAddress{}, err
	}
	document, err := newDocumentAddress(selectedTarget, scope, aggregateRoot)
	if err != nil {
		return ProjectionAddress{}, err
	}
	if err := footprint.Validate(); err != nil {
		return ProjectionAddress{}, fmt.Errorf("aggregate footprint: %w", err)
	}
	if footprint.Kind() != output.FootprintKey {
		return ProjectionAddress{}, fmt.Errorf("aggregate projection requires a key footprint")
	}
	if err := validateToken("aggregate merge unit", string(mergeUnit)); err != nil {
		return ProjectionAddress{}, err
	}
	return ProjectionAddress{
		document: document, placementID: placementID, mergeUnit: mergeUnit, footprint: footprint,
	}, nil
}

// LegacyContentPath refuses native scopes that the frozen aggregate grammar cannot carry.
func (address ProjectionAddress) LegacyContentPath() (ContentPath, error) {
	if err := address.Validate(); err != nil {
		return "", err
	}
	legacy, err := address.footprint.LegacyContentPath()
	if err != nil {
		return "", err
	}
	parsed, err := ParseContentPath(string(legacy))
	if err != nil {
		return "", fmt.Errorf("%w: aggregate token grammar", output.ErrNotLegacyFootprint)
	}
	return parsed, nil
}

func (contribution ManagedContribution) LegacyContentPath() (string, error) {
	legacy, err := contribution.address.LegacyContentPath()
	return string(legacy), err
}

func (address ProjectionAddress) Footprint() output.Footprint { return address.footprint }

// KeyPath projects the aggregate's key-only scope; the zero address stays a root selector, not authority.
func (address ProjectionAddress) KeyPath() output.KeyPath {
	key, _ := address.footprint.Key()
	return key
}

func (contribution ManagedContribution) Footprint() output.Footprint {
	return contribution.address.footprint
}
