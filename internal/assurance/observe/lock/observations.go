package lock

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

// ObservationSet contains subject-keyed exact-Supply evidence.
type ObservationSet struct {
	exactSupplies []observe.ExactSupplyObservation
}

// ExactSupplies returns subject-keyed exact-Supply observations.
func (set ObservationSet) ExactSupplies() []observe.ExactSupplyObservation {
	return append([]observe.ExactSupplyObservation(nil), set.exactSupplies...)
}

func lockObservationsWithResolver(
	ctx context.Context,
	resolver acquisition.BatchResolver,
	environment desired.Environment,
	locked lock.File,
	selection targetselection.Selection,
) (ObservationSet, error) {
	epoch, err := resolveSourceEpochWithResolver(ctx, resolver, environment, locked, selection)
	if err != nil {
		return ObservationSet{}, err
	}
	return epoch.Observations(ctx)
}
