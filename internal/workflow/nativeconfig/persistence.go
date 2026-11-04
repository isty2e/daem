package nativeconfig

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func Codecs(ctx context.Context) (aggregate.CodecCatalog, error) {
	return aggregatecodec.ForOperation(ctx, aggregatecodec.ContractIDs(), DocumentLimits(), DocumentLimits())
}

// LoadPersistenceEpoch selects native-capable decoding at the CLI boundary.
// The allocation starts before the state read and stays pending through assessment.
func LoadPersistenceEpoch(ctx context.Context, paths daempaths.Paths) (readiness.PersistenceEpoch, error) {
	return LoadPersistenceEpochWithBudget(ctx, paths, physicalwork.NewCaptureBudget(nil))
}

// LoadPersistenceEpochWithBudget keeps repeated observation and its eventual
// native journal in the caller's retained segment allocation.
func LoadPersistenceEpochWithBudget(ctx context.Context, paths daempaths.Paths, budget *physicalwork.Budget) (readiness.PersistenceEpoch, error) {
	if err := budget.RequireCapturePending(); err != nil {
		return readiness.PersistenceEpoch{}, err
	}
	codecs, err := Codecs(ctx)
	if err != nil {
		return readiness.PersistenceEpoch{}, err
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		return readiness.PersistenceEpoch{}, err
	}
	state, err := statefile.LoadOptionalWithOptions(ctx, paths.StatefilePath, statefile.ReadOptions{
		Codec: codec, PhysicalPathBudget: budget,
	})
	if err != nil {
		return readiness.PersistenceEpoch{}, err
	}
	store, err := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
	if err != nil {
		return readiness.PersistenceEpoch{}, err
	}
	claims, err := store.LoadForSelectedAuthority(ctx, paths.StatefilePath, paths.ManifestPath)
	if err != nil {
		return readiness.PersistenceEpoch{}, err
	}
	return readiness.NewPersistenceEpochWithBudget(state, claims, budget), nil
}
