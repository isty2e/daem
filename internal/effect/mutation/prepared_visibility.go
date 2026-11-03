package mutation

import (
	"context"
	"fmt"
)

// ReserveVisibilityAcceptance projects both namespace validations and the
// intervening domain pass without observing the filesystem.
func (prepared PreparedExclusiveRequests) ReserveVisibilityAcceptance(budget PhysicalObservationBudget) error {
	if prepared.leases == nil || budget == nil {
		return fmt.Errorf("prepared visibility allocation is required")
	}
	set := prepared.leases
	set.mu.RLock()
	defer set.mu.RUnlock()
	if set.released || len(set.held) == 0 || len(set.domains) == 0 {
		return fmt.Errorf("mutation lease set is not active")
	}
	namespace, supported := set.namespace.(projectedLeaseNamespace)
	if !supported {
		return fmt.Errorf("prepared mutation namespace is unavailable")
	}
	paths, err := namespace.preparedObservationPaths()
	if err != nil {
		return err
	}
	var namespaceKeys, domainKeys []pathObservationKey
	for _, path := range paths {
		namespaceKeys = append(namespaceKeys, pathObservationKey{path, PathEffectReferent})
	}
	for _, domain := range set.domains {
		if domain.kind != domainLogicalPath && domain.kind != domainPhysicalPath {
			continue
		}
		if !domain.namespaceLease.isZero() {
			domainKeys = append(domainKeys, pathObservationKey{domain.namespaceLease.accessPath, PathEffectReferent})
		}
		domainKeys = append(domainKeys, pathObservationKey{domain.requestedPath, domain.effect})
	}
	if err := prepared.reserveVisibilityPass(namespaceKeys, budget); err != nil {
		return err
	}
	if err := namespace.reserveCurrentObservation(prepared.depth, budget); err != nil {
		return err
	}
	if err := prepared.reserveVisibilityPass(domainKeys, budget); err != nil {
		return err
	}
	if err := prepared.reserveVisibilityPass(namespaceKeys, budget); err != nil {
		return err
	}
	return namespace.reserveCurrentObservation(prepared.depth, budget)
}

func (prepared PreparedExclusiveRequests) reserveVisibilityPass(keys []pathObservationKey, budget PhysicalObservationBudget) error {
	seen := make(map[pathObservationKey]bool)
	for _, key := range keys {
		if seen[key] {
			continue
		}
		layout, present := prepared.layouts[key]
		if !present {
			return fmt.Errorf("visibility path was not prepared")
		}
		if err := reservePreparedPath(layout, key.effect, prepared.depth, budget); err != nil {
			return err
		}
		seen[key] = true
	}
	return nil
}

// AcceptVisibilityChanges rebinds the borrowed domains after a successful effect,
// retaining their original namespace leases and consuming only supplied work.
func (prepared PreparedExclusiveRequests) AcceptVisibilityChanges(ctx context.Context, budget PhysicalObservationBudget) (bool, error) {
	if ctx == nil || prepared.leases == nil || budget == nil {
		return false, fmt.Errorf("prepared visibility context, leases and allocation are required")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	set := prepared.leases
	set.mu.Lock()
	defer set.mu.Unlock()
	if set.released || len(set.held) == 0 || len(set.domains) == 0 {
		return false, fmt.Errorf("mutation lease set is not active")
	}
	namespace, supported := set.namespace.(boundedLeaseNamespace)
	if !supported {
		return false, fmt.Errorf("bounded mutation namespace is unavailable")
	}
	if err := namespace.validateCurrentBounded(ctx, prepared.depth, budget, prepared.observer(ctx, budget)); err != nil {
		return false, err
	}
	observed := make([]canonicalPath, len(set.domains))
	matched, err := observeVisibilityDomainRange(ctx, set.domains, observed, prepared.observer(ctx, budget))
	if err != nil || !matched {
		return matched, err
	}
	accepted, matched, err := acceptVisibilityDomains(set.domains, observed)
	if err != nil || !matched {
		return matched, err
	}
	if err := namespace.validateCurrentBounded(ctx, prepared.depth, budget, prepared.observer(ctx, budget)); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	set.domains = accepted
	return true, nil
}
