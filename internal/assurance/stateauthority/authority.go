// Package stateauthority owns the stable identity of one manifest-selected
// durable state authority.
package stateauthority

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

// Key is the exact statefile path authority that determines state identity.
type Key struct {
	value pathauthority.Exact
}

// NewKey validates an exact statefile path authority.
func NewKey(value pathauthority.Exact) (Key, error) {
	key := Key{value: value}
	if err := key.Validate(); err != nil {
		return Key{}, err
	}
	return key, nil
}

// Validate rejects an empty, relative, unclean, or NUL-bearing key.
func (key Key) Validate() error {
	if err := key.value.Validate(); err != nil {
		return fmt.Errorf("statefile authority key: %w", err)
	}
	return nil
}

// String returns the canonical absolute statefile key.
func (key Key) String() string {
	return key.value.Key()
}

// PathAuthority returns the exact path identity and semantics witness.
func (key Key) PathAuthority() pathauthority.Exact {
	return key.value
}

// Compare returns the deterministic order of exact statefile authorities.
func (key Key) Compare(other Key) int {
	return key.value.Compare(other.value)
}

// Authority identifies one manifest-selected durable state authority.
// The statefile key is authoritative; the manifest path is diagnostic provenance.
type Authority struct {
	key          Key
	manifestPath string
}

// New validates an already-canonical statefile key and manifest provenance path.
func New(statefileKey pathauthority.Exact, manifestPath string) (Authority, error) {
	key, err := NewKey(statefileKey)
	if err != nil {
		return Authority{}, err
	}
	if err := validateAbsoluteCleanPath("manifest provenance path", manifestPath); err != nil {
		return Authority{}, err
	}
	return Authority{key: key, manifestPath: manifestPath}, nil
}

// Validate checks authority identity without inspecting the referenced files.
func (authority Authority) Validate() error {
	if err := authority.key.Validate(); err != nil {
		return err
	}
	if err := validateAbsoluteCleanPath("manifest provenance path", authority.manifestPath); err != nil {
		return err
	}
	return nil
}

// StatefileKey returns the canonical equality key for the state authority.
func (authority Authority) StatefileKey() string {
	return authority.key.String()
}

// StatefileAuthority returns the exact path identity of the statefile.
func (authority Authority) StatefileAuthority() pathauthority.Exact {
	return authority.key.PathAuthority()
}

// Key returns the canonical identity subvalue.
func (authority Authority) Key() Key {
	return authority.key
}

// ManifestPath returns non-authoritative provenance used for diagnostics.
func (authority Authority) ManifestPath() string {
	return authority.manifestPath
}

// Equal reports whether two values identify the same state authority.
func (authority Authority) Equal(other Authority) bool {
	return authority.key == other.key
}

// ExactEqual includes diagnostic provenance in the comparison.
func (authority Authority) ExactEqual(other Authority) bool {
	return authority == other
}

// IsZero reports whether no authority identity was initialized.
func (authority Authority) IsZero() bool {
	return authority.key == (Key{}) && authority.manifestPath == ""
}

func validateAbsoluteCleanPath(name string, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s contains a NUL byte", name)
	}
	if !filepath.IsAbs(value) {
		return fmt.Errorf("%s %q must be absolute", name, value)
	}
	if filepath.Clean(value) != value {
		return fmt.Errorf("%s %q must be clean", name, value)
	}
	return nil
}
