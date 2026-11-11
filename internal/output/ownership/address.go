// Package ownership models durable authority over managed host-output footprints.
package ownership

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/output"
)

// ManagedAddress binds exact physical path authority to one explicit footprint.
type ManagedAddress struct {
	path      pathauthority.Exact
	footprint output.Footprint
}

func NewManagedAddress(path pathauthority.Exact, footprint output.Footprint) (ManagedAddress, error) {
	address := ManagedAddress{path: path, footprint: footprint}
	if err := address.Validate(); err != nil {
		return ManagedAddress{}, err
	}
	return address, nil
}

// NewManagedAddressFromLegacy preserves literal slash-segment interpretation at
// existing producer and old metadata boundaries; it never parses an RFC pointer.
func NewManagedAddressFromLegacy(path pathauthority.Exact, contentPath string) (ManagedAddress, error) {
	footprint, err := output.FootprintFromLegacy(output.ContentPath(contentPath))
	if err != nil {
		return ManagedAddress{}, err
	}
	return NewManagedAddress(path, footprint)
}

// Validate checks the canonical address invariants without performing filesystem I/O.
func (address ManagedAddress) Validate() error {
	if err := address.path.Validate(); err != nil {
		return fmt.Errorf("managed address path authority: %w", err)
	}
	if err := address.footprint.Validate(); err != nil {
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

func (address ManagedAddress) Footprint() output.Footprint { return address.footprint }

// WholePath reports whether the address owns the complete physical path.
func (address ManagedAddress) WholePath() bool {
	return address.footprint.Kind() == output.FootprintPath
}

// Equal reports exact physical and projection identity equality.
func (address ManagedAddress) Equal(other ManagedAddress) bool {
	return address.path.Equal(other.path) && address.footprint.Equal(other.footprint)
}

// Overlaps reports whether two addresses claim intersecting mutation footprints.
func (address ManagedAddress) Overlaps(other ManagedAddress) bool {
	if !address.path.Equal(other.path) {
		return address.path.Contains(other.path) || other.path.Contains(address.path)
	}
	return address.footprint.Overlaps(other.footprint)
}

// MayOverlap also retains possible legacy namespace collisions for exclusion.
// Unlike Overlaps, it is not evidence of a proven footprint intersection.
func (address ManagedAddress) MayOverlap(other ManagedAddress) bool {
	if address.path.Equal(other.path) {
		return address.footprint.Overlaps(other.footprint)
	}
	return address.path.MayOverlap(other.path)
}

// Less reports deterministic canonical address order.
func (address ManagedAddress) Less(other ManagedAddress) bool {
	if !address.path.Equal(other.path) {
		return address.path.Compare(other.path) < 0
	}
	return output.CompareFootprints(address.footprint, other.footprint) < 0
}
