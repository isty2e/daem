//go:build !darwin

package mutation

import "github.com/isty2e/daem/internal/effect/mutation/rootedpath"

func newPlatformPathObservation() func(pathSelection, PathEffect) (canonicalPath, error) {
	return platformCanonicalPath
}

func newBoundedPlatformPathObservation(
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) func(pathSelection, PathEffect) (canonicalPath, error) {
	return func(selection pathSelection, effect PathEffect) (canonicalPath, error) {
		return platformCanonicalPathBounded(selection, effect, maximumPhysicalDepth, budget)
	}
}
