// Package ownership models durable authority over managed host-output footprints.
package ownership

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/output"
)

// ManagedAddress identifies one canonical physical path or projection footprint.
// An empty content path owns the whole physical path.
type ManagedAddress struct {
	path        pathauthority.Exact
	contentPath output.ContentPath
}

// NewManagedAddress validates exact physical authority and a projection path.
func NewManagedAddress(path pathauthority.Exact, contentPath string) (ManagedAddress, error) {
	address := ManagedAddress{path: path, contentPath: output.ContentPath(contentPath)}
	if err := address.Validate(); err != nil {
		return ManagedAddress{}, err
	}
	return address, nil
}

// Validate checks the canonical address invariants without performing filesystem I/O.
func (address ManagedAddress) Validate() error {
	if err := address.path.Validate(); err != nil {
		return fmt.Errorf("managed address path authority: %w", err)
	}
	if err := address.contentPath.Validate(); err != nil {
		return err
	}
	return nil
}

// Path returns the canonical physical path key.
func (address ManagedAddress) Path() string {
	return address.path.Key()
}

// PathAuthority returns the exact physical path identity and semantics.
func (address ManagedAddress) PathAuthority() pathauthority.Exact {
	return address.path
}

// ContentPath returns the canonical projection path. Empty means whole-path ownership.
func (address ManagedAddress) ContentPath() string {
	return string(address.contentPath)
}

// WholePath reports whether the address owns the complete physical path.
func (address ManagedAddress) WholePath() bool {
	return address.contentPath == ""
}

// Equal reports exact physical and projection identity equality.
func (address ManagedAddress) Equal(other ManagedAddress) bool {
	return address.path.Equal(other.path) && address.contentPath == other.contentPath
}

// Overlaps reports whether two addresses claim intersecting mutation footprints.
func (address ManagedAddress) Overlaps(other ManagedAddress) bool {
	if !address.path.Equal(other.path) {
		return address.path.Contains(other.path) || other.path.Contains(address.path)
	}
	if address.WholePath() || other.WholePath() {
		return true
	}
	return address.contentPath.Overlaps(other.contentPath)
}

// Less reports deterministic canonical address order.
func (address ManagedAddress) Less(other ManagedAddress) bool {
	if !address.path.Equal(other.path) {
		return address.path.Compare(other.path) < 0
	}
	return address.contentPath < other.contentPath
}
