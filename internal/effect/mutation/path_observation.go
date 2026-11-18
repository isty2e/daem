package mutation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type pathIdentityObserver func(string, PathEffect) (canonicalPath, error)

type pathObservationKey struct {
	path   string
	effect PathEffect
}

// NewStableRouteReadDomain shares the entry lock with ordinary writers. A
// missing ancestor or provisional entry has no stable read domain; callers
// still need their own fresh route observation before publishing.
func NewStableRouteReadDomain(path string, depth int, budget rootedpath.PhysicalTraversalBudget) (Domain, bool, error) {
	if err := validatePathObservation(path, PathEffectDirectoryEntry); err != nil {
		return Domain{}, false, err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return Domain{}, false, nil
	}
	physical, missing, err := rootedpath.ResolveFutureReferentComponentsBounded(parent, depth, budget)
	if err != nil {
		return Domain{}, false, err
	}
	if len(missing) != 0 {
		return Domain{}, false, nil
	}
	selection := pathSelection{anchorPath: physical, missingComponents: []string{filepath.Base(path)}, finalEntryMayExist: true}
	identity, err := platformCanonicalPathBounded(selection, PathEffectDirectoryEntry, depth, budget)
	identity, err = validateCanonicalPathIdentity(path, identity, err)
	if err != nil || !identity.provisional.IsZero() {
		return Domain{}, false, err
	}
	domain := logicalPathDomain(LogicalPathRequest{Path: path, Access: AccessShared, Effect: PathEffectDirectoryEntry}, identity)
	domain.routeRead = true
	return domain, true, nil
}

// ObserveFutureReferentAuthorityBounded resolves a locator and observes the
// resolved entry's authority with one bounded traversal. It returns physical
// path data and exact or provisional evidence, never a mutation capability.
func ObserveFutureReferentAuthorityBounded(
	path string,
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) (string, DirectoryEntryAuthorityObservation, error) {
	canonicalize := func(selection pathSelection, effect PathEffect) (canonicalPath, error) {
		return platformCanonicalPathBounded(selection, effect, maximumPhysicalDepth, budget)
	}
	return observeFutureReferentAuthorityBounded(path, maximumPhysicalDepth, budget, canonicalize)
}

// NewFutureReferentAuthorityObserverBounded reuses successful platform metadata
// within one stable, single-writer namespace observation. It retains no handles
// and must not be reused across effects or fresh acquisition boundaries.
func NewFutureReferentAuthorityObserverBounded(
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) func(string) (string, DirectoryEntryAuthorityObservation, error) {
	canonicalize := newBoundedPlatformPathObservation(maximumPhysicalDepth, budget)
	return func(path string) (string, DirectoryEntryAuthorityObservation, error) {
		return observeFutureReferentAuthorityBounded(path, maximumPhysicalDepth, budget, canonicalize)
	}
}

func observeFutureReferentAuthorityBounded(
	path string,
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
	canonicalize func(pathSelection, PathEffect) (canonicalPath, error),
) (string, DirectoryEntryAuthorityObservation, error) {
	anchor, components, err := rootedpath.ResolveFutureReferentComponentsBounded(path, maximumPhysicalDepth, budget)
	if err != nil {
		return "", DirectoryEntryAuthorityObservation{}, err
	}
	resolved := filepath.Join(anchor, filepath.Join(components...))
	if _, err := rootedpath.CanonicalPathOnlyDestination(resolved); err != nil {
		return "", DirectoryEntryAuthorityObservation{}, fmt.Errorf("canonicalize mutation path %q: %w", resolved, err)
	}
	if len(components) == 0 {
		components = []string{filepath.Base(anchor)}
		anchor = filepath.Dir(anchor)
	}
	selection := pathSelection{
		anchorPath:         anchor,
		missingComponents:  components,
		finalEntryMayExist: len(components) == 1,
	}
	identity, err := canonicalize(selection, PathEffectDirectoryEntry)
	identity, err = validateCanonicalPathIdentity(resolved, identity, err)
	if err != nil {
		return "", DirectoryEntryAuthorityObservation{}, err
	}
	observation, err := directoryEntryAuthorityObservation(identity)
	if err != nil {
		return "", DirectoryEntryAuthorityObservation{}, err
	}
	return resolved, observation, nil
}

func newBoundedPathIdentityObserver(ctx context.Context, depth int, budget PhysicalObservationBudget) pathIdentityObserver {
	return cachePathIdentities(func(path string, effect PathEffect) (canonicalPath, error) {
		if err := ctx.Err(); err != nil {
			return canonicalPath{}, err
		}
		if err := validatePathObservation(path, effect); err != nil {
			return canonicalPath{}, err
		}
		var selection pathSelection
		if effect == PathEffectDirectoryEntry {
			value, err := selectDirectoryEntryPathBounded(path, depth, budget)
			if err != nil {
				return canonicalPath{}, err
			}
			selection = value
		} else {
			absolute, err := filepath.Abs(filepath.Clean(path))
			if err != nil {
				return canonicalPath{}, err
			}
			resolved, err := rootedpath.ResolveReferentPathBounded(absolute, depth, budget)
			if err != nil {
				return canonicalPath{}, err
			}
			if err := admitPlatformPathTraversal(resolved, depth, budget); err != nil {
				return canonicalPath{}, err
			}
			if err := budget.AdmitPhysicalWork(0, 1, 0); err != nil {
				return canonicalPath{}, err
			}
			_, err = os.Lstat(resolved)
			if err == nil {
				selection = pathSelection{anchorPath: resolved}
			} else if errors.Is(err, os.ErrNotExist) {
				selection, err = selectDirectoryEntryPathBounded(resolved, depth, budget)
				if err != nil {
					return canonicalPath{}, err
				}
			} else {
				return canonicalPath{}, err
			}
		}
		return canonicalPathIdentityFromSelectionBounded(path, selection, effect, depth, budget)
	})
}

func validatePathObservation(path string, effect PathEffect) error {
	if strings.TrimSpace(path) == "" || strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("mutation path is invalid")
	}
	return effect.validate()
}

// newPathIdentityObserver shares successful observations within one read-only
// pass. It must not survive a visibility effect or another validation call.
func newPathIdentityObserver() pathIdentityObserver {
	resolve := newPathSelectionResolver(filepath.EvalSymlinks, requireDirectoryAncestor)
	observe := newPlatformPathObservation()
	return func(path string, effect PathEffect) (canonicalPath, error) {
		selection, err := selectPathWithResolver(path, effect, resolve)
		if err != nil {
			return canonicalPath{}, err
		}
		identity, err := observe(selection, effect)
		return validateCanonicalPathIdentity(path, identity, err)
	}
}

func newPathSelectionResolver(
	resolveSymlinks func(string) (string, error),
	requireDirectory func(string) error,
) func(string) (pathSelection, error) {
	symlinks := make(map[string]string)
	resolveExisting := func(path string) (string, error) {
		if resolved, ok := symlinks[path]; ok {
			return resolved, nil
		}
		resolved, err := resolveSymlinks(path)
		if err == nil {
			symlinks[path] = resolved
		}
		return resolved, err
	}

	directories := make(map[string]struct{})
	requireExistingDirectory := func(path string) error {
		if _, ok := directories[path]; ok {
			return nil
		}
		if err := requireDirectory(path); err != nil {
			return err
		}
		directories[path] = struct{}{}
		return nil
	}

	resolved := make(map[string]pathSelection)
	return func(path string) (pathSelection, error) {
		if selection, ok := resolved[path]; ok {
			return selection, nil
		}
		selection, err := resolveDeepestExistingWith(path, resolveExisting, requireExistingDirectory)
		if err == nil {
			resolved[path] = selection
		}
		return selection, err
	}
}

// cachePathIdentities retains complete successful values within one read-only pass.
// Its lifetime must end before any effect or another validation invocation.
func cachePathIdentities(observe pathIdentityObserver) pathIdentityObserver {
	observed := make(map[pathObservationKey]canonicalPath)
	return func(path string, effect PathEffect) (canonicalPath, error) {
		key := pathObservationKey{path: path, effect: effect}
		if identity, exists := observed[key]; exists {
			return identity, nil
		}
		identity, err := observe(path, effect)
		if err == nil {
			observed[key] = identity
		}
		return identity, err
	}
}
