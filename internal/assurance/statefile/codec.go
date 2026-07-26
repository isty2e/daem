package statefile

import "github.com/isty2e/daem/internal/assurance/durable"

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
