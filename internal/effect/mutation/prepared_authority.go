package mutation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type projectedLeaseNamespace interface {
	preparedObservationPaths() ([]string, error)
	reserveCurrentObservation(int, PhysicalObservationBudget) error
}

// PreparedExclusiveRequests borrows the original lease lifetime. Its layouts
// permit bounded fresh checks, not additional paths or independent grants.
type PreparedExclusiveRequests struct {
	leases    *LeaseSet
	controls  []string
	documents []PhysicalAuthorityRequest
	depth     int
	keys      []pathObservationKey
	layouts   map[pathObservationKey]rootedpath.PathLayout
}

func (set *LeaseSet) PrepareCurrentExclusiveRequestsBounded(ctx context.Context, controls []string, documents []PhysicalAuthorityRequest, depth int, budget PhysicalObservationBudget) (PreparedExclusiveRequests, error) {
	if ctx == nil || set == nil || budget == nil || depth <= 0 {
		return PreparedExclusiveRequests{}, fmt.Errorf("prepared exclusive authority requires context, active leases and allocation")
	}
	if err := ctx.Err(); err != nil {
		return PreparedExclusiveRequests{}, err
	}
	set.mu.RLock()
	defer set.mu.RUnlock()
	if set.released || len(set.held) == 0 || len(set.domains) == 0 {
		return PreparedExclusiveRequests{}, fmt.Errorf("mutation lease set is not active")
	}
	namespace, supported := set.namespace.(projectedLeaseNamespace)
	if !supported {
		return PreparedExclusiveRequests{}, fmt.Errorf("prepared mutation namespace is unavailable")
	}
	paths, err := namespace.preparedObservationPaths()
	if err != nil {
		return PreparedExclusiveRequests{}, err
	}
	prepared := PreparedExclusiveRequests{leases: set, controls: slices.Clone(controls), documents: slices.Clone(documents), depth: depth, layouts: make(map[pathObservationKey]rootedpath.PathLayout)}
	add := func(path string, effect PathEffect) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := validatePathObservation(path, effect); err != nil {
			return err
		}
		key := pathObservationKey{path, effect}
		if _, present := prepared.layouts[key]; present {
			return nil
		}
		absolute, err := filepath.Abs(filepath.Clean(path))
		if err != nil {
			return err
		}
		var layout rootedpath.PathLayout
		if effect == PathEffectReferent {
			layout, err = rootedpath.CaptureReferentLayoutBounded(absolute, depth, budget)
		} else {
			layout, err = rootedpath.CaptureDirectoryEntryLayoutBounded(absolute, depth, budget)
		}
		if err != nil {
			return err
		}
		prepared.keys = append(prepared.keys, key)
		prepared.layouts[key] = layout
		return nil
	}
	for _, path := range paths {
		if err := add(path, PathEffectReferent); err != nil {
			return PreparedExclusiveRequests{}, err
		}
	}
	for _, domain := range set.domains {
		if domain.kind != domainLogicalPath && domain.kind != domainPhysicalPath {
			continue
		}
		if err := add(domain.requestedPath, domain.effect); err != nil {
			return PreparedExclusiveRequests{}, err
		}
		if !domain.namespaceLease.isZero() {
			if err := add(domain.namespaceLease.accessPath, PathEffectReferent); err != nil {
				return PreparedExclusiveRequests{}, err
			}
		}
	}
	for _, path := range controls {
		for _, effect := range []PathEffect{PathEffectDirectoryEntry, PathEffectReferent} {
			if err := add(path, effect); err != nil {
				return PreparedExclusiveRequests{}, err
			}
		}
	}
	for _, document := range documents {
		for _, effect := range []PathEffect{PathEffectDirectoryEntry, PathEffectReferent} {
			if err := add(document.Path, effect); err != nil {
				return PreparedExclusiveRequests{}, err
			}
		}
	}
	matched, err := set.matchCurrentExclusiveRequestsBounded(ctx, controls, documents, depth, budget, prepared.observer(ctx, budget))
	if err != nil || !matched {
		return PreparedExclusiveRequests{}, errors.Join(fmt.Errorf("prepared requests are not exclusively held"), err)
	}
	return prepared, nil
}

// ReserveCheck projects one complete successful check without filesystem I/O.
func (prepared PreparedExclusiveRequests) ReserveCheck(budget PhysicalObservationBudget) error {
	if prepared.leases == nil || budget == nil {
		return fmt.Errorf("prepared exclusive authority is unavailable")
	}
	set := prepared.leases
	set.mu.RLock()
	defer set.mu.RUnlock()
	if set.released {
		return fmt.Errorf("mutation lease set is not active")
	}
	for _, key := range prepared.keys {
		layout := prepared.layouts[key]
		if err := reservePreparedPath(layout, key.effect, prepared.depth, budget); err != nil {
			return err
		}
	}
	namespace, supported := set.namespace.(projectedLeaseNamespace)
	if !supported {
		return fmt.Errorf("prepared mutation namespace is unavailable")
	}
	return namespace.reserveCurrentObservation(prepared.depth, budget)
}

func (prepared PreparedExclusiveRequests) MatchCurrent(ctx context.Context, budget PhysicalObservationBudget) (bool, error) {
	return prepared.matchCurrentDocuments(ctx, nil, budget)
}

// MatchCurrentExclusiveDocuments also requires each exact document to be a
// requested referent disjoint from controls. ReserveCheck covers this same pass.
func (prepared PreparedExclusiveRequests) MatchCurrentExclusiveDocuments(ctx context.Context, documents []pathauthority.Exact, budget PhysicalObservationBudget) (bool, error) {
	for _, document := range documents {
		if err := document.Validate(); err != nil {
			return false, err
		}
	}
	return prepared.matchCurrentDocuments(ctx, documents, budget)
}

func (prepared PreparedExclusiveRequests) matchCurrentDocuments(ctx context.Context, documents []pathauthority.Exact, budget PhysicalObservationBudget) (bool, error) {
	if ctx == nil || prepared.leases == nil || budget == nil {
		return false, fmt.Errorf("prepared exclusive authority is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	set := prepared.leases
	set.mu.RLock()
	defer set.mu.RUnlock()
	observe := prepared.observer(ctx, budget)
	matched, err := set.matchCurrentExclusiveRequestsBounded(ctx, prepared.controls, prepared.documents, prepared.depth, budget, observe)
	if err != nil || !matched || len(documents) == 0 {
		return matched, err
	}
	covered := make(map[pathauthority.Exact]bool)
	for _, request := range prepared.documents {
		identity, err := observe(request.Path, PathEffectReferent)
		if err != nil {
			return false, err
		}
		observation, err := directoryEntryAuthorityObservation(identity)
		if err != nil {
			return false, err
		}
		if exact, present := observation.Exact(); present {
			covered[exact] = true
		}
	}
	var controls []pathauthority.Exact
	for _, path := range prepared.controls {
		for _, effect := range []PathEffect{PathEffectDirectoryEntry, PathEffectReferent} {
			identity, err := observe(path, effect)
			if err != nil {
				return false, err
			}
			observation, err := directoryEntryAuthorityObservation(identity)
			if err != nil {
				return false, err
			}
			exact, present := observation.Exact()
			if !present {
				return false, fmt.Errorf("control namespace is not exact")
			}
			controls = append(controls, exact)
		}
	}
	for _, document := range documents {
		if !covered[document] {
			return false, fmt.Errorf("document has no admitted physical request")
		}
		for _, control := range controls {
			if document.Contains(control) || control.Contains(document) {
				return false, fmt.Errorf("document overlaps a control artifact")
			}
		}
	}
	return true, ctx.Err()
}

func (prepared PreparedExclusiveRequests) observer(ctx context.Context, budget PhysicalObservationBudget) pathIdentityObserver {
	return cachePathIdentities(func(path string, effect PathEffect) (canonicalPath, error) {
		if err := ctx.Err(); err != nil {
			return canonicalPath{}, err
		}
		layout, present := prepared.layouts[pathObservationKey{path, effect}]
		if !present {
			return canonicalPath{}, fmt.Errorf("mutation path was not prepared")
		}
		return observePreparedPath(ctx, path, effect, layout, prepared.depth, budget)
	})
}
