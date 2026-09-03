package hostsurface

import (
	"fmt"
	"strings"
)

// SurfaceID is an opaque internal linkage key for one host-surface cell.
// It is not persisted and is not a placement, codec, route, or topology ID.
type SurfaceID struct {
	key SurfaceKey
}

// NewSurfaceID returns the unique internal ID for a validated key.
func NewSurfaceID(key SurfaceKey) (SurfaceID, error) {
	if err := key.Validate(); err != nil {
		return SurfaceID{}, err
	}
	return SurfaceID{key: key}, nil
}

// Key returns the semantic key bound to this ID.
func (id SurfaceID) Key() SurfaceKey {
	return id.key
}

// Validate rejects a zero or malformed ID.
func (id SurfaceID) Validate() error {
	return id.key.Validate()
}

// Equal reports whether two IDs name the same surface.
func (id SurfaceID) Equal(other SurfaceID) bool {
	return id.key.Equal(other.key)
}

// String returns a debug label that cannot collide with durable placement IDs.
func (id SurfaceID) String() string {
	if id.key.Validate() != nil {
		return ""
	}
	return strings.Join([]string{
		"surface",
		string(id.key.kind),
		string(id.key.target),
		string(id.key.scope),
		string(id.key.variant),
	}, "/")
}

// CompareIDs orders IDs by their keys.
func CompareIDs(left SurfaceID, right SurfaceID) int {
	return CompareKeys(left.key, right.key)
}

// MustSurfaceID returns a SurfaceID or panics. Tests and sealed catalogs only.
func MustSurfaceID(key SurfaceKey) SurfaceID {
	id, err := NewSurfaceID(key)
	if err != nil {
		panic(fmt.Sprintf("hostsurface: %v", err))
	}
	return id
}
