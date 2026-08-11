package recovery

import (
	"fmt"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

// RemovalDemandSet is the deterministic complete set of logical-removal
// reachability facts for one executable operation. It is constructed once at
// the transition boundary and reused by journal capture and coverage checks.
type RemovalDemandSet struct {
	demands []RemovalDemand
}

type removalDemandKey struct {
	scope       target.Scope
	destination output.Destination
}

// NewRemovalDemandSet constructs one duplicate-free canonical demand set.
func NewRemovalDemandSet(demands []RemovalDemand) (RemovalDemandSet, error) {
	if len(demands) > MaximumRemovalIntents {
		return RemovalDemandSet{}, fmt.Errorf(
			"removal demand count %d exceeds operation maximum %d",
			len(demands),
			MaximumRemovalIntents,
		)
	}
	copy := append([]RemovalDemand(nil), demands...)
	seen := make(map[removalDemandKey]struct{}, len(copy))
	for index := range copy {
		if err := copy[index].validate(); err != nil {
			return RemovalDemandSet{}, fmt.Errorf("removal demand[%d]: %w", index, err)
		}
		key := removalDemandKey{scope: copy[index].scope, destination: copy[index].destination}
		if _, duplicate := seen[key]; duplicate {
			return RemovalDemandSet{}, fmt.Errorf("removal demand set contains duplicate relation %q", copy[index].destination)
		}
		seen[key] = struct{}{}
	}
	slices.SortFunc(copy, func(left, right RemovalDemand) int {
		if left.scope != right.scope {
			if left.scope < right.scope {
				return -1
			}
			return 1
		}
		return strings.Compare(left.destination.String(), right.destination.String())
	})
	return RemovalDemandSet{demands: copy}, nil
}

// Demands returns an owned copy of the canonical operation demand set.
func (set RemovalDemandSet) Demands() []RemovalDemand {
	return slices.Clone(set.demands)
}

// Len returns the number of rooted relations with reachable logical removal.
func (set RemovalDemandSet) Len() int { return len(set.demands) }

// Equal reports whether two sets carry the same sorted demand authority.
func (set RemovalDemandSet) Equal(other RemovalDemandSet) bool {
	if len(set.demands) != len(other.demands) {
		return false
	}
	for index := range set.demands {
		if !set.demands[index].equal(other.demands[index]) {
			return false
		}
	}
	return true
}

// Validate checks the complete canonical demand set.
func (set RemovalDemandSet) Validate() error {
	_, err := NewRemovalDemandSet(set.demands)
	return err
}
