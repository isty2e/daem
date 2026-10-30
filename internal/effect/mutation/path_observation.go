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
