package durable

import (
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/target"
)

func validateSnapshotCarrierFacts(
	pending []durablecarrier.PendingCarrierInstall,
	removals []durablecarrier.PendingCarrierRemoval,
	claims []durablecarrier.ManagedCarrierClaim,
) error {
	pendingByKey := make(map[durablecarrier.CarrierFactKey]durablecarrier.PendingCarrierInstall, len(pending))
	removalsByKey := make(map[durablecarrier.CarrierFactKey]durablecarrier.PendingCarrierRemoval, len(removals))
	claimsByKey := make(map[durablecarrier.CarrierFactKey]durablecarrier.ManagedCarrierClaim, len(claims))
	var owner durablecarrier.StateAuthority
	haveOwner := false
	validateOwner := func(candidate durablecarrier.StateAuthority, label string) error {
		if !haveOwner {
			owner = candidate
			haveOwner = true
			return nil
		}
		if !owner.ExactEqual(candidate) {
			return fmt.Errorf("%s belongs to a foreign state authority", label)
		}
		return nil
	}
	for index, value := range pending {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("pending carrier install[%d]: %w", index, err)
		}
		if err := validateOwner(value.Owner(), fmt.Sprintf("pending carrier install[%d]", index)); err != nil {
			return err
		}
		key := value.FactKey()
		if _, duplicate := pendingByKey[key]; duplicate {
			return fmt.Errorf(
				"pending carrier install[%d] duplicates one owner relation",
				index,
			)
		}
		pendingByKey[key] = value
	}
	for index, value := range removals {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("pending carrier removal[%d]: %w", index, err)
		}
		if err := validateOwner(value.Owner(), fmt.Sprintf("pending carrier removal[%d]", index)); err != nil {
			return err
		}
		key := value.FactKey()
		if _, duplicate := removalsByKey[key]; duplicate {
			return fmt.Errorf(
				"pending carrier removal[%d] duplicates one owner relation",
				index,
			)
		}
		if _, installing := pendingByKey[key]; installing {
			return fmt.Errorf(
				"pending carrier removal[%d] conflicts with pending install for the same owner relation",
				index,
			)
		}
		removalsByKey[key] = value
	}
	for index, value := range claims {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("managed carrier claim[%d]: %w", index, err)
		}
		if value.Identity().Scope() != target.ScopeProject {
			return fmt.Errorf(
				"managed carrier claim[%d]: global claims require the global carrier registry",
				index,
			)
		}
		if err := validateOwner(value.Owner(), fmt.Sprintf("managed carrier claim[%d]", index)); err != nil {
			return err
		}
		key := value.FactKey()
		if _, duplicate := claimsByKey[key]; duplicate {
			return fmt.Errorf(
				"managed carrier claim[%d] duplicates one owner relation",
				index,
			)
		}
		claimsByKey[key] = value
	}
	for key, removal := range removalsByKey {
		claim, exists := claimsByKey[key]
		switch removal.Identity().Scope() {
		case target.ScopeProject:
			if !exists || !removal.Claim().ExactEqual(claim) {
				return fmt.Errorf(
					"pending project carrier removal requires its exact managed claim",
				)
			}
		case target.ScopeGlobal:
			if exists {
				return fmt.Errorf(
					"pending global carrier removal claim must remain in the global registry",
				)
			}
		}
	}
	for key, pendingValue := range pendingByKey {
		claim, overlap := claimsByKey[key]
		if !overlap {
			continue
		}
		if !pendingValue.Owner().ExactEqual(claim.Owner()) ||
			!pendingValue.Identity().ExactEqual(claim.Identity()) ||
			!pendingValue.InstallRequest().Equal(claim.InstallRequest()) {
			return fmt.Errorf(
				"pending carrier install conflicts with managed carrier claim for the same owner relation",
			)
		}
	}
	return nil
}

// WithPreparedCarrierInstalls upserts exact write-ahead facts without replacing
// unrelated pending installs. A different fact for the same authority and
// relation is a contradiction, not an update.
func (snapshot Snapshot) WithPreparedCarrierInstalls(
	prepared []durablecarrier.PendingCarrierInstall,
) (Snapshot, bool, error) {
	next := snapshot.PendingCarrierInstalls()
	changed := false
	for index, candidate := range prepared {
		if err := candidate.Validate(); err != nil {
			return Snapshot{}, false, fmt.Errorf(
				"prepared carrier install[%d]: %w",
				index,
				err,
			)
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
					"prepared carrier install[%d] conflicts with retained pending identity",
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
	result, err := snapshot.WithPendingCarrierInstalls(next)
	if err != nil {
		return Snapshot{}, false, err
	}
	return result, changed, nil
}

// WithoutPendingCarrierInstall retires one exact completed write-ahead fact.
// Absence is idempotent; a different fact for the same owner relation is a
// contradiction and is never removed.
func (snapshot Snapshot) WithoutPendingCarrierInstall(
	completed durablecarrier.PendingCarrierInstall,
) (Snapshot, bool, error) {
	if err := completed.Validate(); err != nil {
		return Snapshot{}, false, fmt.Errorf("completed carrier install: %w", err)
	}
	key := completed.FactKey()
	pending := snapshot.PendingCarrierInstalls()
	for index, existing := range pending {
		if existing.FactKey() != key {
			continue
		}
		if !existing.ExactEqual(completed) {
			return Snapshot{}, false, fmt.Errorf(
				"completed carrier install conflicts with retained pending identity",
			)
		}
		next := append(
			append([]durablecarrier.PendingCarrierInstall(nil), pending[:index]...),
			pending[index+1:]...,
		)
		result, err := snapshot.WithPendingCarrierInstalls(next)
		if err != nil {
			return Snapshot{}, false, err
		}
		return result, true, nil
	}
	return snapshot, false, nil
}

// WithPromotedCarrierClaims atomically replaces matching pending project
// installs with exact observed claims. A claim without its exact pending
// acquisition is rejected.
func (snapshot Snapshot) WithPromotedCarrierClaims(
	promotions []durablecarrier.ManagedCarrierClaim,
) (Snapshot, bool, error) {
	pending := snapshot.PendingCarrierInstalls()
	claims := snapshot.ManagedCarrierClaims()
	changed := false
	for index, claim := range promotions {
		if err := claim.Validate(); err != nil {
			return Snapshot{}, false, fmt.Errorf(
				"promoted carrier claim[%d]: %w",
				index,
				err,
			)
		}
		if claim.Identity().Scope() != target.ScopeProject {
			return Snapshot{}, false, fmt.Errorf(
				"promoted carrier claim[%d]: global claims require the global carrier registry",
				index,
			)
		}
		pendingIndex := exactPendingCarrierIndex(pending, claim)
		if pendingIndex < 0 {
			if exactManagedCarrierClaimIndex(claims, claim) >= 0 {
				continue
			}
			return Snapshot{}, false, fmt.Errorf(
				"promoted carrier claim[%d] has no exact pending acquisition",
				index,
			)
		}
		pending = append(pending[:pendingIndex], pending[pendingIndex+1:]...)
		if exactManagedCarrierClaimIndex(claims, claim) < 0 {
			claims = append(claims, claim)
		}
		changed = true
	}
	input := snapshot.input()
	input.PendingCarrierInstalls = pending
	input.ManagedCarrierClaims = claims
	result, err := NewSnapshot(input)
	if err != nil {
		return Snapshot{}, false, err
	}
	return result, changed, nil
}

// WithAdoptedCarrierClaims atomically acquires exact project claim authority
// without requiring or consuming a pending install. Existing exact adopted
// claims are idempotent; pending-only or conflicting owner relations are
// rejected.
func (snapshot Snapshot) WithAdoptedCarrierClaims(
	adoptions []durablecarrier.ManagedCarrierClaim,
) (Snapshot, bool, error) {
	claims := snapshot.ManagedCarrierClaims()
	pending := snapshot.PendingCarrierInstalls()
	seen := make(map[durablecarrier.CarrierFactKey]struct{}, len(adoptions))
	changed := false
	for index, claim := range adoptions {
		if err := claim.Validate(); err != nil {
			return Snapshot{}, false, fmt.Errorf(
				"adopted carrier claim[%d]: %w",
				index,
				err,
			)
		}
		if claim.Provenance() != durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved {
			return Snapshot{}, false, fmt.Errorf(
				"adopted carrier claim[%d] requires explicit-adoption provenance",
				index,
			)
		}
		if claim.Identity().Scope() != target.ScopeProject {
			return Snapshot{}, false, fmt.Errorf(
				"adopted carrier claim[%d]: global claims require the global carrier registry",
				index,
			)
		}
		key := claim.FactKey()
		if _, duplicate := seen[key]; duplicate {
			return Snapshot{}, false, fmt.Errorf(
				"adopted carrier claim[%d] duplicates one owner relation",
				index,
			)
		}
		seen[key] = struct{}{}

		matched := false
		for _, existing := range claims {
			if existing.FactKey() != key {
				continue
			}
			matched = true
			if !existing.ExactEqual(claim) {
				return Snapshot{}, false, fmt.Errorf(
					"adopted carrier claim[%d] conflicts with retained owner relation",
					index,
				)
			}
			break
		}
		if matched {
			continue
		}
		for _, existing := range pending {
			if existing.FactKey() == key {
				return Snapshot{}, false, fmt.Errorf(
					"adopted carrier claim[%d] conflicts with pending acquisition",
					index,
				)
			}
		}
		claims = append(claims, claim)
		changed = true
	}
	if !changed {
		return snapshot, false, nil
	}
	result, err := snapshot.WithManagedCarrierClaims(claims)
	if err != nil {
		return Snapshot{}, false, err
	}
	return result, true, nil
}

func exactPendingCarrierIndex(
	pending []durablecarrier.PendingCarrierInstall,
	claim durablecarrier.ManagedCarrierClaim,
) int {
	for index, candidate := range pending {
		if candidate.Owner().ExactEqual(claim.Owner()) &&
			candidate.Identity().ExactEqual(claim.Identity()) &&
			candidate.InstallRequest().Equal(claim.InstallRequest()) {
			return index
		}
	}
	return -1
}

func exactManagedCarrierClaimIndex(
	claims []durablecarrier.ManagedCarrierClaim,
	claim durablecarrier.ManagedCarrierClaim,
) int {
	for index, candidate := range claims {
		if candidate.ExactEqual(claim) {
			return index
		}
	}
	return -1
}

// WithoutManagedCarrierClaim removes only one exact project claim. Absence is
// idempotent; a different claim for the same owner relation is a contradiction.
func (snapshot Snapshot) WithoutManagedCarrierClaim(
	claim durablecarrier.ManagedCarrierClaim,
) (Snapshot, bool, error) {
	if err := claim.Validate(); err != nil {
		return Snapshot{}, false, fmt.Errorf("retired managed carrier claim: %w", err)
	}
	if claim.Identity().Scope() != target.ScopeProject {
		return Snapshot{}, false, fmt.Errorf(
			"retired managed carrier claim: global claims require the global carrier registry",
		)
	}
	key := claim.FactKey()
	claims := snapshot.ManagedCarrierClaims()
	for index, existing := range claims {
		if existing.FactKey() != key {
			continue
		}
		if !existing.ExactEqual(claim) {
			return Snapshot{}, false, fmt.Errorf(
				"retired managed carrier claim conflicts with retained owner relation",
			)
		}
		next := append(
			append([]durablecarrier.ManagedCarrierClaim(nil), claims[:index]...),
			claims[index+1:]...,
		)
		result, err := snapshot.WithManagedCarrierClaims(next)
		if err != nil {
			return Snapshot{}, false, err
		}
		return result, true, nil
	}
	return snapshot, false, nil
}

// WithConvergedGlobalCarrierClaims removes only local pending installs whose
// exact global claim has already committed. This completes interrupted
// registry-first promotion without inferring ownership from partial identity.
func (snapshot Snapshot) WithConvergedGlobalCarrierClaims(
	registry durablecarrier.GlobalCarrierClaims,
) (Snapshot, bool, error) {
	globalClaims := registry.Claims()
	pending := snapshot.PendingCarrierInstalls()
	next := make([]durablecarrier.PendingCarrierInstall, 0, len(pending))
	changed := false
	for _, candidate := range pending {
		matched := false
		for _, claim := range globalClaims {
			if candidate.Owner().ExactEqual(claim.Owner()) &&
				candidate.Identity().ExactEqual(claim.Identity()) &&
				candidate.InstallRequest().Equal(claim.InstallRequest()) {
				matched = true
				break
			}
		}
		if matched {
			changed = true
			continue
		}
		next = append(next, candidate)
	}
	if !changed {
		return snapshot, false, nil
	}
	result, err := snapshot.WithPendingCarrierInstalls(next)
	if err != nil {
		return Snapshot{}, false, err
	}
	return result, true, nil
}
