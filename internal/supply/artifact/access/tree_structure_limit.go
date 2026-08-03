package access

import "fmt"

// TreeStructureLimit bounds the shape observed for one directory artifact.
// Entries count every descendant but exclude the selected root. Depth counts
// descendant directories, so zero depth permits only regular root children.
type TreeStructureLimit struct {
	maximumEntries int
	maximumDepth   int
}

// NewTreeStructureLimit constructs a finite directory-artifact shape bound.
func NewTreeStructureLimit(maximumEntries int, maximumDepth int) (TreeStructureLimit, error) {
	if maximumEntries <= 0 {
		return TreeStructureLimit{}, fmt.Errorf("artifact tree maximum entries must be positive")
	}
	if maximumDepth < 0 {
		return TreeStructureLimit{}, fmt.Errorf("artifact tree maximum depth must not be negative")
	}
	return TreeStructureLimit{
		maximumEntries: maximumEntries,
		maximumDepth:   maximumDepth,
	}, nil
}

func (limit TreeStructureLimit) validate() error {
	_, err := NewTreeStructureLimit(limit.maximumEntries, limit.maximumDepth)
	return err
}
