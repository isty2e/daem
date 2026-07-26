package carrier

import (
	"fmt"
	"path/filepath"
	"strings"
)

// StateAuthority identifies one selected manifest's durable state authority.
// The statefile key is authoritative; the manifest path is diagnostic provenance.
type StateAuthority struct {
	statefileKey string
	manifestPath string
}

// NewStateAuthority validates an already-canonical statefile key and manifest path.
func NewStateAuthority(statefileKey string, manifestPath string) (StateAuthority, error) {
	authority := StateAuthority{statefileKey: statefileKey, manifestPath: manifestPath}
	if err := authority.Validate(); err != nil {
		return StateAuthority{}, err
	}
	return authority, nil
}

// Validate rejects zero, relative, unclean, or control-bearing authority paths.
func (authority StateAuthority) Validate() error {
	if err := validateAuthorityPath("statefile authority key", authority.statefileKey); err != nil {
		return err
	}
	if err := validateAuthorityPath("manifest provenance path", authority.manifestPath); err != nil {
		return err
	}
	return nil
}

// StatefileKey returns the canonical equality key for this state authority.
func (authority StateAuthority) StatefileKey() string {
	return authority.statefileKey
}

// ManifestPath returns non-authoritative manifest provenance for diagnostics.
func (authority StateAuthority) ManifestPath() string {
	return authority.manifestPath
}

// Equal reports whether two values name the same state authority.
func (authority StateAuthority) Equal(other StateAuthority) bool {
	return authority.statefileKey == other.statefileKey
}

// ExactEqual includes diagnostic provenance in the comparison.
func (authority StateAuthority) ExactEqual(other StateAuthority) bool {
	return authority == other
}

func validateAuthorityPath(label string, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s contains a NUL byte", label)
	}
	if !filepath.IsAbs(value) {
		return fmt.Errorf("%s %q must be absolute", label, value)
	}
	if filepath.Clean(value) != value {
		return fmt.Errorf("%s %q must be clean", label, value)
	}
	return nil
}
