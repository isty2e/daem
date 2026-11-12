package readiness

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	daempaths "github.com/isty2e/daem/internal/paths"
)

// PersistenceEpoch binds the canonical state and shared carrier claims observed
// for one readiness decision. The zero value is intentionally not an epoch.
type PersistenceEpoch struct {
	currentState        durable.Snapshot
	physicalStateCodec  statefile.PhysicalCodec
	globalCarrierClaims durablecarrier.GlobalCarrierClaims
	physicalBudget      *physicalwork.Budget
	initialized         bool
}

// NewPersistenceEpochWithBudget retains the allocation used by the selected
// state reader; it does not admit removals or replenish that allocation.
func NewPersistenceEpochWithBudget(currentState durable.Snapshot, globalCarrierClaims durablecarrier.GlobalCarrierClaims, budget *physicalwork.Budget) PersistenceEpoch {
	epoch := NewPersistenceEpoch(currentState, globalCarrierClaims)
	epoch.physicalBudget = budget
	return epoch
}

// NewPersistenceEpochWithPhysicalState retains the physical selection from the
// same State read as the canonical facts, without rereading or replenishing work.
func NewPersistenceEpochWithPhysicalState(state statefile.PhysicalState, claims durablecarrier.GlobalCarrierClaims, budget *physicalwork.Budget) PersistenceEpoch {
	epoch := NewPersistenceEpochWithBudget(state.Snapshot(), claims, budget)
	epoch.physicalStateCodec = state.Codec()
	return epoch
}

func (epoch PersistenceEpoch) PhysicalStateCodec(template statefile.PhysicalCodec) (statefile.PhysicalCodec, error) {
	if !epoch.initialized {
		return statefile.PhysicalCodec{}, fmt.Errorf("readiness persistence epoch is not initialized")
	}
	return template.ForObservedState(epoch.physicalStateCodec)
}

func (epoch PersistenceEpoch) Snapshot() (durable.Snapshot, error) {
	state, _, err := epoch.facts()
	return state, err
}

func (epoch PersistenceEpoch) CarrierClaims() (durablecarrier.GlobalCarrierClaims, error) {
	_, claims, err := epoch.facts()
	return claims, err
}

func (epoch PersistenceEpoch) PhysicalBudget() (*physicalwork.Budget, error) {
	if !epoch.initialized || epoch.physicalBudget == nil {
		return nil, fmt.Errorf("selected persistence epoch requires its original physical allocation")
	}
	return epoch.physicalBudget, epoch.physicalBudget.RequireCapturePending()
}

// NewPersistenceEpoch binds already-loaded canonical persistence facts.
func NewPersistenceEpoch(
	currentState durable.Snapshot,
	globalCarrierClaims durablecarrier.GlobalCarrierClaims,
) PersistenceEpoch {
	return PersistenceEpoch{
		currentState:        currentState,
		physicalStateCodec:  statefile.DefaultPhysicalCodec(),
		globalCarrierClaims: globalCarrierClaims,
		initialized:         true,
	}
}

func loadPersistenceEpoch(
	ctx context.Context,
	paths daempaths.Paths,
) (PersistenceEpoch, error) {
	state, err := statefile.LoadOptionalPhysical(ctx, paths.StatefilePath)
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
	return NewPersistenceEpochWithPhysicalState(state, globalCarrierClaims, nil), nil
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
