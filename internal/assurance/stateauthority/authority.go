// Package stateauthority owns the stable identity of one manifest-selected
// durable state authority.
package stateauthority

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Key is the canonical statefile path that determines authority identity.
type Key struct {
	value string
}

// NewKey validates an already-canonical statefile authority key.
func NewKey(value string) (Key, error) {
	key := Key{value: value}
	if err := key.Validate(); err != nil {
		return Key{}, err
	}
	return key, nil
}

// Validate rejects an empty, relative, unclean, or NUL-bearing key.
func (key Key) Validate() error {
	return validateAbsoluteCleanPath("statefile authority key", key.value)
}

// String returns the canonical absolute statefile key.
func (key Key) String() string {
	return key.value
}

// Authority identifies one manifest-selected durable state authority.
// The statefile key is authoritative; the manifest path is diagnostic provenance.
type Authority struct {
	key          Key
	manifestPath string
}

// New validates an already-canonical statefile key and manifest provenance path.
func New(statefileKey string, manifestPath string) (Authority, error) {
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
