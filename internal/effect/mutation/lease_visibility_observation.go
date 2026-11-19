package mutation

import (
	"context"
	"fmt"
	"runtime"
	"sync"
)

// bindVisibilityDomains runs separately from lock selection. An exact
// referent may use its matching entry's already selected namespace, but it
// cannot rebind to a different entry within that namespace.
func bindVisibilityDomains(requested []Domain) []Domain {
	type pairKey struct {
		kind                          domainKind
		requested, key, target, scope string
		witness                       pathSemanticsWitness
	}
	pair := func(domain Domain) pairKey {
		return pairKey{domain.kind, domain.requestedPath, domain.canonicalPath, domain.target, domain.scope, domain.pathWitness}
	}
	var entries map[pairKey]namespaceLeaseIntent
	domains := make([]Domain, 0, len(requested))
	for _, domain := range requested {
		if domain.routeRead {
			continue
		}
		domains = append(domains, domain)
		if domain.access == AccessExclusive && domain.effect == PathEffectDirectoryEntry && domain.provisional.IsZero() && !domain.namespaceLease.isZero() {
			if entries == nil {
				entries = make(map[pairKey]namespaceLeaseIntent)
			}
			entries[pair(domain)] = domain.namespaceLease
		}
	}
	if len(entries) != 0 {
		for index, domain := range domains {
			if domain.access != AccessExclusive || domain.effect != PathEffectReferent || !domain.provisional.IsZero() || !domain.namespaceLease.isZero() {
				continue
			}
			if namespace, paired := entries[pair(domain)]; paired {
				domain.namespaceLease = namespace
				domain.pairedEntry = true
				domains[index] = domain
			}
		}
	}
	return domains
}

func observeVisibilityDomains(
	ctx context.Context,
	domains []Domain,
	newObserver func() pathIdentityObserver,
) ([]canonicalPath, bool, error) {
	observed := make([]canonicalPath, len(domains))
	workers := min(4, runtime.GOMAXPROCS(0))
	if len(domains) < 128 || workers == 1 {
		matches, err := observeVisibilityDomainRange(ctx, domains, observed, newObserver())
		if err != nil || !matches {
			return nil, matches, err
		}
		return observed, true, nil
	}

	var outcomes [4]struct {
		matches bool
		err     error
	}
	var group sync.WaitGroup
	for worker := range workers {
		start, end := worker*len(domains)/workers, (worker+1)*len(domains)/workers
		group.Go(func() {
			outcomes[worker].matches, outcomes[worker].err = observeVisibilityDomainRange(
				ctx, domains[start:end], observed[start:end], newObserver(),
			)
		})
	}
	group.Wait()

	// Drain every read before rebinding leases; completion order must not
	// change which domain's mismatch or error wins.
	for _, outcome := range outcomes[:workers] {
		if outcome.err != nil || !outcome.matches {
			return nil, outcome.matches, outcome.err
		}
	}
	return observed, true, nil
}

func observeVisibilityDomainRange(
	ctx context.Context,
	domains []Domain,
	observed []canonicalPath,
	observe pathIdentityObserver,
) (bool, error) {
	for index, domain := range domains {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		switch domain.kind {
		case domainLogicalPath, domainPhysicalPath:
			if !domain.namespaceLease.isZero() {
				matches, err := domain.namespaceLease.matchesCurrentUsing(observe)
				if err != nil {
					return false, err
				}
				if !matches {
					return false, nil
				}
			}
			identity, err := observe(domain.requestedPath, domain.effect)
			if err != nil {
				return false, err
			}
			observed[index] = identity
		case domainHostRoute:
		default:
			return false, fmt.Errorf("mutation domain is not initialized")
		}
	}

	return true, nil
}
