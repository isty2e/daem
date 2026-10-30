package mutation

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type boundedLeaseNamespace interface {
	validateCurrentBounded(context.Context, int, PhysicalObservationBudget, pathIdentityObserver) error
}

type PhysicalObservationBudget interface {
	rootedpath.PhysicalTraversalBudget
	AdmitPhysicalWork(pathComponents int, entries int, bytes int64) error
}

// MatchCurrentExclusiveRequestsBounded checks exact logical controls and physical
// documents in one active-set observation. It borrows, but never releases or
// reacquires, the outer owner's leases; the result grants no lifetime extension.
func (set *LeaseSet) MatchCurrentExclusiveRequestsBounded(ctx context.Context, controls []string, documents []PhysicalAuthorityRequest, depth int, budget PhysicalObservationBudget) (bool, error) {
	if ctx == nil || set == nil || budget == nil || depth <= 0 {
		return false, fmt.Errorf("bounded exclusive authority requires context, active leases and traversal allocation")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	set.mu.RLock()
	defer set.mu.RUnlock()
	return set.matchCurrentExclusiveRequestsBounded(ctx, controls, documents, depth, budget, newBoundedPathIdentityObserver(ctx, depth, budget))
}

func (set *LeaseSet) matchCurrentExclusiveRequestsBounded(ctx context.Context, controls []string, documents []PhysicalAuthorityRequest, depth int, budget PhysicalObservationBudget, observe pathIdentityObserver) (bool, error) {
	if set.released || len(set.held) == 0 || len(set.domains) == 0 {
		return false, fmt.Errorf("mutation lease set is not active")
	}
	namespace, supported := set.namespace.(boundedLeaseNamespace)
	if !supported {
		return false, fmt.Errorf("bounded mutation lease namespace is unavailable")
	}
	if err := namespace.validateCurrentBounded(ctx, depth, budget, observe); err != nil {
		return false, err
	}
	for _, domain := range set.domains {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		matches, err := domain.matchesCurrentPath(observe)
		if err != nil || !matches {
			return matches, err
		}
	}

	required := make([]Domain, 0, len(controls)*2+len(documents)*2)
	for _, path := range controls {
		for _, effect := range []PathEffect{PathEffectDirectoryEntry, PathEffectReferent} {
			identity, err := observe(path, effect)
			if err != nil {
				return false, err
			}
			required = append(required, Domain{kind: domainLogicalPath, access: AccessExclusive, canonicalPath: identity.keyPath, pathWitness: identity.witness, effect: effect})
		}
	}
	physical, err := newPhysicalAuthoritySet(observe, documents...)
	if err != nil {
		return false, err
	}
	required = append(required, physical.domains...)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return coversExclusiveDomains(set.domains, required)
}
