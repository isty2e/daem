package refresh

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
)

func requireNoPendingPinRefresh(ctx context.Context, paths daempaths.Paths, state durable.Snapshot, contract lock.LockedSubjectContract) (bool, error) {
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contract)
	if err != nil {
		return false, err
	}
	if !admitted || !durablecarrier.SharesPiGitRepository(identity, identity) {
		return false, nil
	}
	store, err := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
	if err != nil {
		return true, err
	}
	registry, err := store.LoadForSelectedAuthority(ctx, paths.StatefilePath, paths.ManifestPath)
	if err != nil {
		return true, err
	}
	for _, claim := range append(state.ManagedCarrierClaims(), registry.Claims()...) {
		if claim.RequireStable() != nil && durablecarrier.SharesPiGitRepository(claim.Identity(), identity) {
			return true, fmt.Errorf("Pi refresh overlaps a pending pin transition; restore its original manifest target and authorize a new apply")
		}
	}
	return true, nil
}
