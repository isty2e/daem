// Package ownership models durable authority over managed host-output footprints.
package ownership

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// ManagedAddress identifies one canonical physical path or projection footprint.
// An empty content path owns the whole physical path.
type ManagedAddress struct {
	path        string
	contentPath string
}

// NewManagedAddress validates an already-canonical physical path and projection path.
func NewManagedAddress(path string, contentPath string) (ManagedAddress, error) {
	address := ManagedAddress{path: path, contentPath: contentPath}
	if err := address.Validate(); err != nil {
		return ManagedAddress{}, err
	}
	return address, nil
}

// Validate checks the canonical address invariants without performing filesystem I/O.
func (address ManagedAddress) Validate() error {
	if strings.TrimSpace(address.path) == "" {
		return fmt.Errorf("managed address path is required")
	}
	if strings.TrimSpace(address.path) != address.path {
		return fmt.Errorf("managed address path must not have surrounding whitespace")
	}
	if strings.ContainsRune(address.path, '\x00') {
		return fmt.Errorf("managed address path contains a NUL byte")
	}
	if !filepath.IsAbs(address.path) {
		return fmt.Errorf("managed address path %q must be absolute", address.path)
	}
	if filepath.Clean(address.path) != address.path {
		return fmt.Errorf("managed address path %q must be clean", address.path)
	}
	if err := validateContentPath(address.contentPath); err != nil {
		return err
	}
	return nil
}

// Path returns the canonical physical path key.
func (address ManagedAddress) Path() string {
	return address.path
}

// ContentPath returns the canonical projection path. Empty means whole-path ownership.
func (address ManagedAddress) ContentPath() string {
	return address.contentPath
}

// WholePath reports whether the address owns the complete physical path.
func (address ManagedAddress) WholePath() bool {
	return address.contentPath == ""
}

// Equal reports exact physical and projection identity equality.
func (address ManagedAddress) Equal(other ManagedAddress) bool {
	return address.path == other.path && address.contentPath == other.contentPath
}

// Overlaps reports whether two addresses claim intersecting mutation footprints.
func (address ManagedAddress) Overlaps(other ManagedAddress) bool {
	if address.path != other.path {
		return pathContains(address.path, other.path) || pathContains(other.path, address.path)
	}
	if address.WholePath() || other.WholePath() {
		return true
	}
	return contentPathContains(address.contentPath, other.contentPath) ||
		contentPathContains(other.contentPath, address.contentPath)
}

// Less reports deterministic canonical address order.
func (address ManagedAddress) Less(other ManagedAddress) bool {
	if address.path != other.path {
		return address.path < other.path
	}
	return address.contentPath < other.contentPath
}

func validateContentPath(contentPath string) error {
	if contentPath == "" {
		return nil
	}
	if strings.TrimSpace(contentPath) != contentPath {
		return fmt.Errorf("managed address content path must not have surrounding whitespace")
	}
	if contentPath == "/" || !strings.HasPrefix(contentPath, "/") {
		return fmt.Errorf("managed address content path %q must identify an absolute non-root projection", contentPath)
	}
	if strings.HasSuffix(contentPath, "/") || strings.Contains(contentPath, "//") {
		return fmt.Errorf("managed address content path %q must be canonical", contentPath)
	}
	if strings.IndexFunc(contentPath, func(r rune) bool { return r == '\x00' || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("managed address content path contains a control character")
	}
	for segment := range strings.SplitSeq(strings.TrimPrefix(contentPath, "/"), "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("managed address content path %q contains a relative segment", contentPath)
		}
	}
	return nil
}

func pathContains(parent string, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func contentPathContains(parent string, child string) bool {
	return parent == child || strings.HasPrefix(child, parent+"/")
}
