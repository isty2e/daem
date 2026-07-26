package durable

import (
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/target"
)

// WithPreparedCarrierRemovals upserts exact write-ahead facts without
// replacing unrelated removals. Every project or global removal requires its
// exact active claim in the corresponding authority store.
func (snapshot Snapshot) WithPreparedCarrierRemovals(
	prepared []durablecarrier.PendingCarrierRemoval,
	globalClaims durablecarrier.GlobalCarrierClaims,
) (Snapshot, bool, error) {
	next := snapshot.PendingCarrierRemovals()
	changed := false
	for index, candidate := range prepared {
		if err := candidate.Validate(); err != nil {
			return Snapshot{}, false, fmt.Errorf(
				"prepared carrier removal[%d]: %w",
				index,
				err,
			)
		}
		switch candidate.Identity().Scope() {
		case target.ScopeProject:
			if exactManagedCarrierClaimIndex(
				snapshot.ManagedCarrierClaims(),
				candidate.Claim(),
			) < 0 {
				return Snapshot{}, false, fmt.Errorf(
					"prepared project carrier removal[%d] has no exact active claim",
					index,
				)
			}
		case target.ScopeGlobal:
			if exactManagedCarrierClaimIndex(
				globalClaims.Claims(),
				candidate.Claim(),
			) < 0 {
				return Snapshot{}, false, fmt.Errorf(
					"prepared global carrier removal[%d] has no exact active claim",
					index,
				)
			}
		}
		key := candidate.FactKey()
		matched := false
		for _, existing := range next {
			if existing.FactKey() != key {
				continue
			}
			matched = true
			if !existing.ExactEqual(candidate) {
				return Snapshot{}, false, fmt.Errorf(
					"prepared carrier removal[%d] conflicts with retained pending identity",
					index,
				)
			}
			break
		}
		if matched {
			continue
		}
		next = append(next, candidate)
		changed = true
	}
	result, err := snapshot.WithPendingCarrierRemovals(next)
	if err != nil {
		return Snapshot{}, false, err
	}
	return result, changed, nil
}

// WithoutPendingCarrierRemoval retires one exact completed write-ahead fact.
// Absence is idempotent; a different fact for the same owner relation is a
// contradiction and is never removed.
func (snapshot Snapshot) WithoutPendingCarrierRemoval(
	completed durablecarrier.PendingCarrierRemoval,
) (Snapshot, bool, error) {
	if err := completed.Validate(); err != nil {
		return Snapshot{}, false, fmt.Errorf("completed carrier removal: %w", err)
	}
	key := completed.FactKey()
	pending := snapshot.PendingCarrierRemovals()
	for index, existing := range pending {
		if existing.FactKey() != key {
			continue
		}
		if !existing.ExactEqual(completed) {
			return Snapshot{}, false, fmt.Errorf(
				"completed carrier removal conflicts with retained pending identity",
			)
		}
		next := append(
			append([]durablecarrier.PendingCarrierRemoval(nil), pending[:index]...),
			pending[index+1:]...,
		)
		result, err := snapshot.WithPendingCarrierRemovals(next)
		if err != nil {
			return Snapshot{}, false, err
		}
		return result, true, nil
	}
	return snapshot, false, nil
}

// WithRetiredProjectCarrierRemoval atomically retires one exact project claim
// and its write-ahead removal fact after verified host absence.
func (snapshot Snapshot) WithRetiredProjectCarrierRemoval(
	completed durablecarrier.PendingCarrierRemoval,
) (Snapshot, bool, error) {
	if err := completed.Validate(); err != nil {
		return Snapshot{}, false, fmt.Errorf("completed carrier removal: %w", err)
	}
	if completed.Identity().Scope() != target.ScopeProject {
		return Snapshot{}, false, fmt.Errorf(
			"completed carrier removal: global claims require the global carrier registry",
		)
	}
	pending := snapshot.PendingCarrierRemovals()
	pendingIndex := exactPendingCarrierRemovalIndex(pending, completed)
	if pendingIndex < 0 {
		return Snapshot{}, false, fmt.Errorf(
			"completed project carrier removal has no exact pending removal",
		)
	}
	claims := snapshot.ManagedCarrierClaims()
	claimIndex := exactManagedCarrierClaimIndex(claims, completed.Claim())
	if claimIndex < 0 {
		return Snapshot{}, false, fmt.Errorf(
			"completed project carrier removal has no exact managed claim",
		)
	}
	input := snapshot.input()
	input.PendingCarrierRemovals = append(
		append([]durablecarrier.PendingCarrierRemoval(nil), pending[:pendingIndex]...),
		pending[pendingIndex+1:]...,
	)
	input.ManagedCarrierClaims = append(
		append([]durablecarrier.ManagedCarrierClaim(nil), claims[:claimIndex]...),
		claims[claimIndex+1:]...,
	)
	result, err := NewSnapshot(input)
	if err != nil {
		return Snapshot{}, false, err
	}
	return result, true, nil
}

func exactPendingCarrierRemovalIndex(
	pending []durablecarrier.PendingCarrierRemoval,
	completed durablecarrier.PendingCarrierRemoval,
) int {
	for index, candidate := range pending {
		if candidate.ExactEqual(completed) {
			return index
		}
	}
	return -1
}
