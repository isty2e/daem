package mutation

import "path/filepath"

type pathIdentityObserver func(string, PathEffect) (canonicalPath, error)

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
	type observationKey struct {
		path   string
		effect PathEffect
	}
	observed := make(map[observationKey]canonicalPath)
	return func(path string, effect PathEffect) (canonicalPath, error) {
		key := observationKey{path: path, effect: effect}
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
