package statefile

import (
	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// Codec implements the canonical durable snapshot codec with the current statefile schema.
type Codec struct{}

var _ durable.SnapshotCodec = Codec{}

// Encode renders one canonical Snapshot as deterministic statefile JSON.
func (Codec) Encode(snapshot durable.Snapshot) ([]byte, error) {
	return Marshal(snapshot)
}

// Decode parses one strict statefile JSON value into canonical durable state.
func (Codec) Decode(content []byte) (durable.Snapshot, error) {
	return Decode(content)
}

// NewCodec binds both state directions to explicit contribution admission.
// It does not widen default state readers or supply document/effect capabilities.
func NewCodec(contributions aggregate.ContributionCatalog) (durable.SnapshotCodec, error) {
	if err := contributions.Validate(); err != nil {
		return nil, err
	}
	return contributionCodec{contributions: contributions}, nil
}

type contributionCodec struct {
	contributions aggregate.ContributionCatalog
}

func (codec contributionCodec) Encode(snapshot durable.Snapshot) ([]byte, error) {
	return marshalWithContributions(snapshot, codec.contributions)
}

func (codec contributionCodec) Decode(content []byte) (durable.Snapshot, error) {
	return decodeWithContributions(content, codec.contributions)
}
