package statefile

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/encoding/toml"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
)

// Codec implements the canonical durable snapshot codec with the current statefile schema.
type Codec struct{}

func (Codec) physicalImageFacts(ctx context.Context) (snapshotCodec, error) {
	return currentSnapshotCodec(ctx)
}

var _ durable.SnapshotCodec = Codec{}

// Encode renders one canonical Snapshot as deterministic statefile JSON.
func (Codec) Encode(snapshot durable.Snapshot) ([]byte, error) {
	return Marshal(snapshot)
}

// Decode parses one strict statefile JSON value into canonical durable state.
func (Codec) Decode(content []byte) (durable.Snapshot, error) {
	return Decode(content)
}

// NewCodec restricts snapshot admission to the supplied contributions and ordinary baselines.
func NewCodec(contributions aggregate.ContributionCatalog) (durable.SnapshotCodec, error) {
	if err := contributions.Validate(); err != nil {
		return nil, err
	}
	return snapshotCodec{contributions: contributions}, nil
}

// NewNativeCodec admits current baselines with the supplied contribution validators.
func NewNativeCodec(contributions aggregate.ContributionCatalog) (durable.SnapshotCodec, error) {
	if err := contributions.Validate(); err != nil {
		return nil, err
	}
	return snapshotCodec{contributions: contributions, nativeDocuments: true}, nil
}

func currentSnapshotCodec(ctx context.Context) (snapshotCodec, error) {
	contributions, err := aggregatecodec.ForContributions(ctx, aggregatecodec.ContractIDs(), configdocument.Limits{
		MaximumBytes: maximumStatefileBytes,
		MaximumDepth: tomlsyntax.MaximumDepth,
	})
	if err != nil {
		return snapshotCodec{}, err
	}
	return snapshotCodec{contributions: contributions, nativeDocuments: true}, nil
}

type snapshotCodec struct {
	contributions   aggregate.ContributionCatalog
	nativeDocuments bool
}

func (codec snapshotCodec) physicalImageFacts(context.Context) (snapshotCodec, error) {
	return codec, nil
}

func (codec snapshotCodec) Encode(snapshot durable.Snapshot) ([]byte, error) {
	return marshalSnapshot(snapshot, codec)
}

func (codec snapshotCodec) Decode(content []byte) (durable.Snapshot, error) {
	return decodeSnapshot(content, codec)
}
