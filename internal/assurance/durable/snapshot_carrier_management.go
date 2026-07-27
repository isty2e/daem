package durable

import (
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
)

// WithoutCarrierManagement retires every exact project-state management fact
// for one owner and carrier relation. It preserves attempt history and rejects
// same-relation drift instead of treating a replacement as the selected
// identity. Global active claims remain owned by GlobalCarrierClaims.
func (snapshot Snapshot) WithoutCarrierManagement(
	owner stateauthority.Authority,
	identity durablecarrier.ManagedCarrierIdentity,
) (Snapshot, bool, error) {
	if err := owner.Validate(); err != nil {
		return Snapshot{}, false, fmt.Errorf("carrier management owner: %w", err)
	}
	if err := identity.Validate(); err != nil {
		return Snapshot{}, false, fmt.Errorf("carrier management identity: %w", err)
	}

	pendingInstalls, installsChanged, err := withoutMatchingCarrierFacts(
		snapshot.PendingCarrierInstalls(),
		owner,
		identity,
		func(value durablecarrier.PendingCarrierInstall) stateauthority.Authority { return value.Owner() },
		func(value durablecarrier.PendingCarrierInstall) durablecarrier.ManagedCarrierIdentity {
			return value.Identity()
		},
	)
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("pending carrier install: %w", err)
	}
	pendingRemovals, removalsChanged, err := withoutMatchingCarrierFacts(
		snapshot.PendingCarrierRemovals(),
		owner,
		identity,
		func(value durablecarrier.PendingCarrierRemoval) stateauthority.Authority { return value.Owner() },
		func(value durablecarrier.PendingCarrierRemoval) durablecarrier.ManagedCarrierIdentity {
			return value.Identity()
		},
	)
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("pending carrier removal: %w", err)
	}
	claims, claimsChanged, err := withoutMatchingCarrierFacts(
		snapshot.ManagedCarrierClaims(),
		owner,
		identity,
		func(value durablecarrier.ManagedCarrierClaim) stateauthority.Authority { return value.Owner() },
		func(value durablecarrier.ManagedCarrierClaim) durablecarrier.ManagedCarrierIdentity {
			return value.Identity()
		},
	)
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("managed carrier claim: %w", err)
	}

	changed := installsChanged || removalsChanged || claimsChanged
	if !changed {
		return snapshot, false, nil
	}
	input := snapshot.input()
	input.PendingCarrierInstalls = pendingInstalls
	input.PendingCarrierRemovals = pendingRemovals
	input.ManagedCarrierClaims = claims
	next, err := NewSnapshot(input)
	if err != nil {
		return Snapshot{}, false, err
	}
	return next, true, nil
}

func withoutMatchingCarrierFacts[T any](
	values []T,
	owner stateauthority.Authority,
	identity durablecarrier.ManagedCarrierIdentity,
	ownerOf func(T) stateauthority.Authority,
	identityOf func(T) durablecarrier.ManagedCarrierIdentity,
) ([]T, bool, error) {
	next := make([]T, 0, len(values))
	changed := false
	for _, value := range values {
		candidateOwner := ownerOf(value)
		candidateIdentity := identityOf(value)
		if !candidateOwner.Equal(owner) ||
			candidateIdentity.RelationSubject() != identity.RelationSubject() {
			next = append(next, value)
			continue
		}
		if !candidateOwner.ExactEqual(owner) || !candidateIdentity.ExactEqual(identity) {
			return nil, false, fmt.Errorf(
				"selected owner relation conflicts with retained carrier identity",
			)
		}
		changed = true
	}
	return next, changed, nil
}
