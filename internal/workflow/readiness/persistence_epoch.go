package readiness

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/statefile"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	daempaths "github.com/isty2e/daem/internal/paths"
)

// PersistenceEpoch binds the canonical state and shared carrier claims observed
// for one readiness decision. The zero value is intentionally not an epoch.
type PersistenceEpoch struct {
	currentState        durable.Snapshot
	globalCarrierClaims durablecarrier.GlobalCarrierClaims
	initialized         bool
}

// NewPersistenceEpoch binds already-loaded canonical persistence facts.
func NewPersistenceEpoch(
	currentState durable.Snapshot,
	globalCarrierClaims durablecarrier.GlobalCarrierClaims,
) PersistenceEpoch {
	return PersistenceEpoch{
		currentState:        currentState,
		globalCarrierClaims: globalCarrierClaims,
		initialized:         true,
	}
}

func loadPersistenceEpoch(
	ctx context.Context,
	paths daempaths.Paths,
) (PersistenceEpoch, error) {
	currentState, err := statefile.LoadOptional(ctx, paths.StatefilePath)
	if err != nil {
		return PersistenceEpoch{}, err
	}
	carrierClaimsStore, err := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
	if err != nil {
		return PersistenceEpoch{}, err
	}
	globalCarrierClaims, err := carrierClaimsStore.LoadForSelectedAuthority(
		ctx,
		paths.StatefilePath,
		paths.ManifestPath,
	)
	if err != nil {
		return PersistenceEpoch{}, err
	}
	return NewPersistenceEpoch(currentState, globalCarrierClaims), nil
}

func (epoch PersistenceEpoch) facts() (
	durable.Snapshot,
	durablecarrier.GlobalCarrierClaims,
	error,
) {
	if !epoch.initialized {
		return durable.Snapshot{}, durablecarrier.GlobalCarrierClaims{}, fmt.Errorf(
			"readiness persistence epoch is not initialized",
		)
	}
	return epoch.currentState, epoch.globalCarrierClaims, nil
}
