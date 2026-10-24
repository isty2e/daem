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
	return snapshotCodec{contributions: contributions}, nil
}

// NewNativeCodec also admits native whole-file baselines in the current schema.
// It grants no filesystem authority and leaves ordinary and legacy readers closed.
func NewNativeCodec(contributions aggregate.ContributionCatalog) (durable.SnapshotCodec, error) {
	if err := contributions.Validate(); err != nil {
		return nil, err
	}
	return snapshotCodec{contributions: contributions, nativeDocuments: true}, nil
}

type snapshotCodec struct {
	contributions   aggregate.ContributionCatalog
	nativeDocuments bool
}

func (codec snapshotCodec) Encode(snapshot durable.Snapshot) ([]byte, error) {
	return marshalSnapshot(snapshot, codec)
}

func (codec snapshotCodec) Decode(content []byte) (durable.Snapshot, error) {
	return decodeSnapshot(content, codec)
}
