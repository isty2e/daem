package rootedpath

import (
	"fmt"
	"path/filepath"
	"strings"
)

const maximumPathSymlinkExpansions = 255

// ChargeDestinationPath admits the complete physical-root and relative-entry
// chain traversed by one destination-bound filesystem operation. The caller
// must charge immediately before the operation that consumes the capability.
func ChargeDestinationPath(
	destination Destination,
	maximumPhysicalDepth int,
	budget PhysicalTraversalBudget,
) error {
	if budget == nil {
		return fmt.Errorf("destination traversal budget is required")
	}
	if maximumPhysicalDepth <= 0 {
		return fmt.Errorf("destination maximum physical depth must be positive")
	}
	value, err := destination.LexicalPath()
	if err != nil {
		return err
	}
	depth, err := absolutePathDepth(value)
	if err != nil {
		return err
	}
	if depth > maximumPhysicalDepth {
		return fmt.Errorf(
			"destination physical path depth %d exceeds maximum %d",
			depth,
			maximumPhysicalDepth,
		)
	}
	return budget.AdmitPathComponents(depth)
}

type physicalTraversal struct {
	maximumDepth int
	budget       PhysicalTraversalBudget
}

func newPhysicalTraversal(
	maximumDepth int,
	budget PhysicalTraversalBudget,
) (*physicalTraversal, error) {
	if maximumDepth <= 0 {
		return nil, fmt.Errorf("maximum physical path depth must be positive")
	}
	if budget == nil {
		return nil, fmt.Errorf("physical path traversal budget is required")
	}
	return &physicalTraversal{maximumDepth: maximumDepth, budget: budget}, nil
}

func (traversal *physicalTraversal) visitComponent() error {
	if traversal == nil {
		return nil
	}
	if err := traversal.budget.AdmitPathComponents(1); err != nil {
		return fmt.Errorf("admit physical path component visit: %w", err)
	}
	return nil
}

func (traversal *physicalTraversal) validateResolvedDepth(depth int) error {
	if traversal == nil {
		return nil
	}
	return traversal.validateDepth(depth)
}

func (traversal *physicalTraversal) validateDepth(depth int) error {
	if depth <= 0 {
		return fmt.Errorf("physical path component depth must be positive")
	}
	if depth > traversal.maximumDepth {
		return fmt.Errorf(
			"physical path depth %d exceeds maximum %d",
			depth,
			traversal.maximumDepth,
		)
	}
	return nil
}

func captureDestinationBounded(
	selectedPath string,
	traversal *physicalTraversal,
) (*CapturedRoot, Destination, error) {
	absolute, err := canonicalDestinationPath(selectedPath)
	if err != nil {
		return nil, Destination{}, err
	}
	parent := filepath.Dir(absolute)
	physicalRoot, platform, object, mount, missingParent, err := resolveDirectoryPathPlatform(
		parent,
		true,
		traversal,
	)
	if err != nil {
		return nil, Destination{}, err
	}
	if filepath.Dir(physicalRoot) == physicalRoot {
		_ = closeCapturedRootPlatform(&platform)
		return nil, Destination{}, newFailure(
			FailureUnsupportedPlatform,
			absolute,
			"destination has no capturable directory below the filesystem root",
			nil,
		)
	}

	relativeComponents := append(append([]string(nil), missingParent...), filepath.Base(absolute))
	relativePath := filepath.Join(relativeComponents...)
	physicalPath := filepath.Join(physicalRoot, relativePath)
	physicalDepth, err := absolutePathDepth(physicalPath)
	if err != nil {
		_ = closeCapturedRootPlatform(&platform)
		return nil, Destination{}, err
	}
	if err := traversal.validateResolvedDepth(physicalDepth); err != nil {
		_ = closeCapturedRootPlatform(&platform)
		return nil, Destination{}, err
	}
	root, err := newCapturedRoot(
		physicalRoot,
		platform,
		object,
		mount,
		rootSelectionNoFollow,
	)
	if err != nil {
		return nil, Destination{}, err
	}
	relative, err := NewRelativeDestination(filepath.ToSlash(relativePath))
	if err != nil {
		_ = root.Close()
		return nil, Destination{}, err
	}
	destination, err := root.authority.Bind(relative)
	if err != nil {
		_ = root.Close()
		return nil, Destination{}, err
	}
	return root, destination, nil
}

func canonicalDestinationPath(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", newFailure(FailureInvalidDestination, value, "destination is required", nil)
	}
	if strings.IndexFunc(value, isForbiddenPathRune) >= 0 {
		return "", newFailure(
			FailureInvalidDestination,
			value,
			"destination contains a control character",
			nil,
		)
	}
	if !filepath.IsAbs(value) {
		return "", newFailure(FailureInvalidDestination, value, "destination must be absolute", nil)
	}
	absolute := filepath.Clean(value)
	if value != absolute {
		return "", newFailure(
			FailureInvalidDestination,
			value,
			"destination must use canonical lexical spelling",
			nil,
		)
	}
	if filepath.Dir(absolute) == absolute {
		return "", newFailure(
			FailureInvalidDestination,
			absolute,
			"destination must name an entry below a capturable directory",
			nil,
		)
	}
	return absolute, nil
}

func splitAbsolutePath(value string) (string, []string, error) {
	clean := filepath.Clean(value)
	if !filepath.IsAbs(clean) {
		return "", nil, newFailure(FailureInvalidRoot, value, "path must be absolute", nil)
	}
	volume := filepath.VolumeName(clean)
	root := volume + string(filepath.Separator)
	relative := strings.TrimPrefix(clean, root)
	if relative == "" {
		return root, nil, nil
	}
	return root, strings.Split(relative, string(filepath.Separator)), nil
}

func absolutePathDepth(value string) (int, error) {
	_, components, err := splitAbsolutePath(value)
	return len(components), err
}
