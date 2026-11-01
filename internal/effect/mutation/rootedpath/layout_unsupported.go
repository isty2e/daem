//go:build !darwin && !linux

package rootedpath

func capturePathLayoutPlatform(path string, _ bool, _ int, _ PhysicalTraversalBudget) (PathLayout, error) {
	return PathLayout{}, newFailure(FailureUnsupportedPlatform, path, "prepared path layout is unavailable", nil)
}

func resolvePathLayoutPlatform(layout PathLayout, _ int, _ PathObservationBudget) (string, []string, error) {
	return "", nil, newFailure(FailureUnsupportedPlatform, layout.path, "prepared path layout is unavailable", nil)
}

func validateCapturedLayoutAnchorPlatform(_ capturedRootPlatform, anchor layoutAnchor) error {
	return newFailure(FailureUnsupportedPlatform, anchor.path, "prepared path layout is unavailable", nil)
}
