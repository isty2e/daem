package mutation

type pathIdentityObserver func(string, PathEffect) (canonicalPath, error)

// newPathIdentityObserver shares successful observations within one read-only
// pass. It must not survive a visibility effect or another validation call.
func newPathIdentityObserver() pathIdentityObserver {
	resolved := make(map[string]pathSelection)
	resolve := func(path string) (pathSelection, error) {
		if selection, ok := resolved[path]; ok {
			return selection, nil
		}
		selection, err := resolveDeepestExisting(path)
		if err == nil {
			resolved[path] = selection
		}
		return selection, err
	}
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
