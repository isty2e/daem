//go:build !darwin

package mutation

func newPlatformPathObservation() func(pathSelection, PathEffect) (canonicalPath, error) {
	return platformCanonicalPath
}
