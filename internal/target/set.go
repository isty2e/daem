package target

import (
	"fmt"
	"slices"
)

// Set is a non-empty, duplicate-free target collection that preserves authored
// order.
type Set struct {
	values []Target
}

// NewSet validates and defensively copies target values.
func NewSet(values []Target) (Set, error) {
	if len(values) == 0 {
		return Set{}, fmt.Errorf("at least one target is required")
	}

	seen := make(map[Target]struct{}, len(values))
	canonical := make([]Target, 0, len(values))
	for _, value := range values {
		parsed, err := ParseTarget(string(value))
		if err != nil {
			return Set{}, err
		}
		if _, exists := seen[parsed]; exists {
			return Set{}, fmt.Errorf("duplicate target %q", parsed)
		}
		seen[parsed] = struct{}{}
		canonical = append(canonical, parsed)
	}

	return Set{values: canonical}, nil
}

// CanonicalSet validates, deduplicates, and sorts a target identity collection.
// Empty input returns a non-nil empty slice; invalid input returns no partial result.
func CanonicalSet(values []Target) ([]Target, error) {
	seen := make(map[Target]struct{}, len(values))
	canonical := make([]Target, 0, len(values))
	for index, value := range values {
		parsed, err := ParseTarget(string(value))
		if err != nil {
			return nil, fmt.Errorf("target[%d]: %w", index, err)
		}
		if _, duplicate := seen[parsed]; duplicate {
			continue
		}
		seen[parsed] = struct{}{}
		canonical = append(canonical, parsed)
	}
	slices.Sort(canonical)
	return canonical, nil
}

// Values returns a defensive copy in authored order.
func (set Set) Values() []Target {
	return append([]Target(nil), set.values...)
}

// Contains reports whether the set contains a target.
func (set Set) Contains(value Target) bool {
	return slices.Contains(set.values, value)
}

// Len returns the number of targets.
func (set Set) Len() int {
	return len(set.values)
}
