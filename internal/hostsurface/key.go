package hostsurface

import (
	"cmp"
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

// SurfaceKey is the semantic identity of one host-surface cell.
type SurfaceKey struct {
	target  target.Target
	scope   target.Scope
	kind    entity.Kind
	variant VariantID
}

// NewSurfaceKey constructs a validated host-surface key.
func NewSurfaceKey(
	selectedTarget target.Target,
	scope target.Scope,
	kind entity.Kind,
	variant VariantID,
) (SurfaceKey, error) {
	parsedTarget, err := target.ParseTarget(string(selectedTarget))
	if err != nil {
		return SurfaceKey{}, fmt.Errorf("host-surface target: %w", err)
	}
	parsedScope, err := target.ParseScope(string(scope))
	if err != nil {
		return SurfaceKey{}, fmt.Errorf("host-surface scope: %w", err)
	}
	parsedKind, err := entity.ParseKind(string(kind))
	if err != nil {
		return SurfaceKey{}, fmt.Errorf("host-surface kind: %w", err)
	}
	parsedVariant, err := ParseVariantID(string(variant))
	if err != nil {
		return SurfaceKey{}, fmt.Errorf("host-surface variant: %w", err)
	}
	return SurfaceKey{
		target:  parsedTarget,
		scope:   parsedScope,
		kind:    parsedKind,
		variant: parsedVariant,
	}, nil
}

// MCPSurfaceKey returns the MCP host-surface key for target and scope.
func MCPSurfaceKey(selectedTarget target.Target, scope target.Scope) (SurfaceKey, error) {
	return NewSurfaceKey(selectedTarget, scope, entity.KindMCPServer, VariantDefault)
}

// Target returns the host target.
func (key SurfaceKey) Target() target.Target { return key.target }

// Scope returns the manifest scope.
func (key SurfaceKey) Scope() target.Scope { return key.scope }

// Kind returns the desired entity family.
func (key SurfaceKey) Kind() entity.Kind { return key.kind }

// Variant returns the independently selectable host-contract variant.
func (key SurfaceKey) Variant() VariantID { return key.variant }

// Validate rejects a zero or malformed key.
func (key SurfaceKey) Validate() error {
	_, err := NewSurfaceKey(key.target, key.scope, key.kind, key.variant)
	return err
}

// Equal reports whether two keys name the same surface cell.
func (key SurfaceKey) Equal(other SurfaceKey) bool {
	return CompareKeys(key, other) == 0
}

// CompareKeys orders keys by target, scope, kind, then variant.
func CompareKeys(left SurfaceKey, right SurfaceKey) int {
	if order := cmp.Compare(left.target, right.target); order != 0 {
		return order
	}
	if order := cmp.Compare(left.scope, right.scope); order != 0 {
		return order
	}
	if order := cmp.Compare(left.kind, right.kind); order != 0 {
		return order
	}
	return cmp.Compare(left.variant, right.variant)
}
