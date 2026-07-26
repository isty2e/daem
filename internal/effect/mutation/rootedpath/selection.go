package rootedpath

import (
	"path/filepath"
	"strings"
)

// bindSelectedEntry validates one selected root alias and binds an exact child
// path to the retained physical-root authority.
func (root *CapturedRoot) bindSelectedEntry(
	selectedRoot string,
	selectedPath string,
) (Destination, error) {
	if root == nil {
		return Destination{}, newFailure(
			FailureRootUnavailable,
			selectedRoot,
			"captured root is required",
			nil,
		)
	}
	if err := root.ValidateSelection(selectedRoot); err != nil {
		return Destination{}, err
	}
	rootPath, err := filepath.Abs(filepath.Clean(selectedRoot))
	if err != nil {
		return Destination{}, newFailure(
			FailureInvalidDestination,
			selectedRoot,
			"resolve selected root",
			err,
		)
	}
	entryPath, err := filepath.Abs(filepath.Clean(selectedPath))
	if err != nil {
		return Destination{}, newFailure(
			FailureInvalidDestination,
			selectedPath,
			"resolve selected entry",
			err,
		)
	}
	relativePath, err := filepath.Rel(rootPath, entryPath)
	if err != nil {
		return Destination{}, newFailure(
			FailureInvalidDestination,
			selectedPath,
			"derive selected-root-relative entry",
			err,
		)
	}
	if relativePath == "." ||
		filepath.IsAbs(relativePath) ||
		relativePath == ".." ||
		strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return Destination{}, newFailure(
			FailureInvalidDestination,
			selectedPath,
			"selected entry must be a child of the selected root",
			nil,
		)
	}
	relative, err := NewRelativeDestination(filepath.ToSlash(relativePath))
	if err != nil {
		return Destination{}, err
	}
	authority, err := root.Authority()
	if err != nil {
		return Destination{}, err
	}
	return authority.Bind(relative)
}
