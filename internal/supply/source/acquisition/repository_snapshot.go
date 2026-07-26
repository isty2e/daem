package acquisition

import (
	"fmt"

	"github.com/isty2e/daem/internal/supply/source"
)

// RepositorySnapshotGroupID identifies repository work that may share one
// operation-local snapshot. Repository paths are intentionally excluded.
type RepositorySnapshotGroupID struct {
	locator      string
	canonicalRef string
}

// NewRepositorySnapshotGroupID constructs a group from canonical Git
// repository facts.
func NewRepositorySnapshotGroupID(gitSource source.GitSource) (RepositorySnapshotGroupID, error) {
	locator := gitSource.Locator()
	selector := gitSource.Ref()
	group := RepositorySnapshotGroupID{
		locator:      locator.String(),
		canonicalRef: selector.Canonical(),
	}
	if selector.String() == "" {
		group.canonicalRef = ""
	}
	if err := group.Validate(); err != nil {
		return RepositorySnapshotGroupID{}, err
	}
	return group, nil
}

// Validate rejects a zero or incomplete repository snapshot group.
func (group RepositorySnapshotGroupID) Validate() error {
	if group.locator == "" {
		return fmt.Errorf("repository snapshot group locator is required")
	}
	if group.canonicalRef == "" {
		return fmt.Errorf("repository snapshot group ref is required")
	}
	return nil
}
