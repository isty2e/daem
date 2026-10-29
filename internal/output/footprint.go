package output

import (
	"cmp"
	"errors"
	"fmt"
	"strings"
)

// FootprintKind distinguishes whole-path authority from one native key subtree.
type FootprintKind uint8

const (
	FootprintPath FootprintKind = iota + 1
	FootprintKey
)

func (kind FootprintKind) String() string {
	switch kind {
	case FootprintPath:
		return "path"
	case FootprintKey:
		return "key"
	default:
		return ""
	}
}

var (
	ErrInvalidFootprint   = errors.New("invalid output footprint")
	ErrNotLegacyFootprint = errors.New("output footprint has no lossless legacy content path")
)

// Footprint is comparable mutation scope within one physical destination. Its
// zero value is invalid; an empty RFC pointer never grants whole-path authority.
type Footprint struct {
	kind FootprintKind
	key  KeyPath
}

func WholePathFootprint() Footprint { return Footprint{kind: FootprintPath} }

// NewKeyFootprint excludes the root selector but admits an empty property name.
func NewKeyFootprint(key KeyPath) (Footprint, error) {
	if key.IsRoot() {
		return Footprint{}, fmt.Errorf("%w: root selection is not key ownership", ErrInvalidFootprint)
	}
	return Footprint{kind: FootprintKey, key: key}, nil
}

// FootprintFromLegacy interprets slash-delimited legacy segments literally.
// It does not RFC-unescape or URI-decode the legacy content path.
func FootprintFromLegacy(path ContentPath) (Footprint, error) {
	if err := path.Validate(); err != nil {
		return Footprint{}, fmt.Errorf("%w: %v", ErrInvalidFootprint, err)
	}
	if path == "" {
		return WholePathFootprint(), nil
	}
	key, err := NewKeyPath(strings.Split(string(path)[1:], "/")...)
	if err != nil {
		return Footprint{}, fmt.Errorf("%w: legacy key must be valid UTF-8", ErrInvalidFootprint)
	}
	return NewKeyFootprint(key)
}

func (footprint Footprint) Kind() FootprintKind { return footprint.kind }

// Key returns the native path only for key authority, not whole-path authority.
func (footprint Footprint) Key() (KeyPath, bool) {
	return footprint.key, footprint.kind == FootprintKey
}

func (footprint Footprint) Validate() error {
	switch footprint.kind {
	case FootprintPath:
		if footprint.key.IsRoot() {
			return nil
		}
	case FootprintKey:
		if !footprint.key.IsRoot() {
			return nil
		}
	}
	return ErrInvalidFootprint
}

func (footprint Footprint) Equal(other Footprint) bool {
	return footprint.Validate() == nil && other.Validate() == nil && footprint == other
}

func (footprint Footprint) Contains(other Footprint) bool {
	if footprint.Validate() != nil || other.Validate() != nil {
		return false
	}
	if footprint.kind == FootprintPath {
		return true
	}
	return other.kind == FootprintKey && footprint.key.Contains(other.key)
}

func (footprint Footprint) Overlaps(other Footprint) bool {
	return footprint.Contains(other) || other.Contains(footprint)
}

// LegacyContentPath is a checked conversion for legacy-shaped boundaries. Native slashes,
// empty names and other unrepresentable keys must not be silently flattened.
func (footprint Footprint) LegacyContentPath() (ContentPath, error) {
	if err := footprint.Validate(); err != nil {
		return "", err
	}
	if footprint.kind == FootprintPath {
		return "", nil
	}
	legacy := ContentPath("/" + strings.Join(footprint.key.Segments(), "/"))
	converted, err := FootprintFromLegacy(legacy)
	if err != nil || converted != footprint {
		return "", ErrNotLegacyFootprint
	}
	return legacy, nil
}

// CompareFootprints defines deterministic identity order, not ancestry order.
func CompareFootprints(left, right Footprint) int {
	if order := cmp.Compare(left.kind, right.kind); order != 0 {
		return order
	}
	return strings.Compare(left.key.String(), right.key.String())
}
