package reconcile

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/target"
)

// SelectedTargets is a validated, duplicate-free reconciliation selection.
// An empty value is valid and means that no target is selected.
type SelectedTargets struct {
	values []target.Target
}

// NewSelectedTargets validates and defensively copies a reconciliation selection.
func NewSelectedTargets(values []target.Target) (SelectedTargets, error) {
	seen := make(map[target.Target]struct{}, len(values))
	canonical := make([]target.Target, 0, len(values))
	for _, value := range values {
		parsed, err := target.ParseTarget(string(value))
		if err != nil {
			return SelectedTargets{}, err
		}
		if _, exists := seen[parsed]; exists {
			return SelectedTargets{}, fmt.Errorf("duplicate target %q", parsed)
		}
		seen[parsed] = struct{}{}
		canonical = append(canonical, parsed)
	}
	return SelectedTargets{values: canonical}, nil
}

// Values returns a defensive copy in stable selection order.
func (selected SelectedTargets) Values() []target.Target {
	return append([]target.Target(nil), selected.values...)
}

// Contains reports whether the target is selected.
func (selected SelectedTargets) Contains(value target.Target) bool {
	return slices.Contains(selected.values, value)
}

// Len returns the number of selected targets.
func (selected SelectedTargets) Len() int {
	return len(selected.values)
}
