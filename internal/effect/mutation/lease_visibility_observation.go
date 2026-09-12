package mutation

import (
	"context"
	"fmt"
	"runtime"
	"sync"
)

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
	for worker := 0; worker < workers; worker++ {
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
				matches, err := domain.namespaceLease.matchesCurrent()
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
