package execute

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
)

type failingStateCodec struct {
	encodeErr error
	decodeErr error
}

func (codec failingStateCodec) Encode(snapshot durable.Snapshot) ([]byte, error) {
	if codec.encodeErr != nil {
		return nil, codec.encodeErr
	}
	return testStateCodec().Encode(snapshot)
}

func (codec failingStateCodec) Decode(content []byte) (durable.Snapshot, error) {
	if codec.decodeErr != nil {
		return durable.Snapshot{}, codec.decodeErr
	}
	return testStateCodec().Decode(content)
}

func testAggregateCodecs() aggregate.CodecCatalog {
	return aggregatecodec.Catalog()
}

func testOwnershipRegistryBinder() ownershipmutation.RootedRegistryBinder {
	return ownershipstore.BindRooted
}

func testStateCodec() durable.SnapshotCodec {
	return statefile.Codec{}
}

func testStateReader(path string) durable.SnapshotReader {
	return func(ctx context.Context) (durable.Snapshot, error) {
		return statefile.LoadOptional(ctx, path)
	}
}

func testPlanLoadOptions(paths Paths) journal.PlanLoadOptions {
	return journal.PlanLoadOptions{
		Filesystem:  testFilesystem(),
		Resolver:    destinationResolver(paths),
		Codecs:      testAggregateCodecs(),
		StateCodec:  testStateCodec(),
		StateReader: testStateReader(paths.StatefilePath),
	}
}

func testRecoveryOptions(paths Paths) RecoveryOptions {
	return RecoveryOptions{
		Resolver:    destinationResolver(paths),
		Codecs:      testAggregateCodecs(),
		StateCodec:  testStateCodec(),
		StateReader: testStateReader(paths.StatefilePath),
		Filesystem:  testFilesystem(),
	}
}

func loadActivePlanWithTestCodecs(ctx context.Context, paths Paths) (recovery.Plan, error) {
	registry, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		return recovery.Plan{}, err
	}
	return journal.LoadActivePlanWithOptions(
		ctx,
		paths.journalPaths(),
		journal.PlanLoadOptions{
			Filesystem:        testFilesystem(),
			Resolver:          destinationResolver(paths),
			OwnershipRegistry: registry.Load,
			Codecs:            testAggregateCodecs(),
			StateCodec:        testStateCodec(),
			StateReader:       testStateReader(paths.StatefilePath),
		},
	)
}
