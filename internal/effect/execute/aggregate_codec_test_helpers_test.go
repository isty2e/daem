package execute

import (
	"context"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
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

func testPhysicalStateCodec() statefile.PhysicalCodec {
	return statefile.DefaultPhysicalCodec()
}

func testStateReader(path string) durable.SnapshotReader {
	return func(ctx context.Context) (durable.Snapshot, error) {
		return statefile.LoadOptional(ctx, path)
	}
}

func testPlanLoadOptions(paths Paths) journal.PlanLoadOptions {
	registry, err := ownershipstore.NewRecoveryReader(testRecoveryRegistryPath(paths))
	if err != nil {
		panic(err)
	}
	return journal.PlanLoadOptions{
		Filesystem: testFilesystem(), Resolver: journalDestinationResolver(paths),
		OwnershipRegistry: registry, Codecs: testAggregateCodecs(),
		StateCodec: testStateCodec(), StateReader: testStateReader(paths.StatefilePath),
	}
}

func testRecoveryRegistryPath(paths Paths) string {
	if paths.OwnershipRegistryPath != "" {
		return paths.OwnershipRegistryPath
	}
	return filepath.Join(filepath.Dir(paths.StatefilePath), "test-ownership.json")
}

func testRecoveryOptions(paths Paths) RecoveryOptions {
	return RecoveryOptions{
		Resolver:                destinationResolver(paths),
		Codecs:                  testAggregateCodecs(),
		StateCodec:              testStateCodec(),
		StateReader:             testStateReader(paths.StatefilePath),
		Filesystem:              testFilesystem(),
		OwnershipRegistryBinder: testOwnershipRegistryBinder(),
	}
}

func loadActivePlanWithTestCodecs(ctx context.Context, paths Paths) (recovery.Plan, error) {
	registry, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		return recovery.Plan{}, err
	}
	return journal.LoadActivePlanWithOptions(
		ctx,
		paths.JournalPaths(),
		journal.PlanLoadOptions{
			Filesystem:        testFilesystem(),
			Resolver:          journalDestinationResolver(paths),
			OwnershipRegistry: registry,
			Codecs:            testAggregateCodecs(),
			StateCodec:        testStateCodec(),
			StateReader:       testStateReader(paths.StatefilePath),
		},
	)
}
