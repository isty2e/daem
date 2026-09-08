package aggregate_test

import (
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestMCPPlacementsForTargetMatchesCatalogAndOwnsResults(t *testing.T) {
	catalog := aggregate.ImplementedMCPPlacements()
	for _, selectedTarget := range append(target.SupportedTargets(), target.Target("future-agent"), target.Target("")) {
		t.Run(string(selectedTarget), func(t *testing.T) {
			t.Parallel()
			want := make([]aggregate.MCPPlacement, 0)
			for _, placement := range catalog {
				if placement.Target() == selectedTarget {
					want = append(want, placement)
				}
			}
			for range 10 {
				got := aggregate.MCPPlacementsForTarget(selectedTarget)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("placements = %#v, want ordered catalog selection %#v", got, want)
				}
				for _, placement := range got {
					clear(placement.ComparedFields())
				}
				clear(got)
				if !reflect.DeepEqual(aggregate.MCPPlacementsForTarget(selectedTarget), want) ||
					!reflect.DeepEqual(aggregate.ImplementedMCPPlacements(), catalog) {
					t.Fatal("caller mutation changed catalog or subsequent selection")
				}
			}
		})
	}
}

func TestMCPPlacementsForTargetAllocationBudget(t *testing.T) {
	for _, selectedTarget := range append(target.SupportedTargets(), target.Target("future-agent"), target.Target("")) {
		t.Run(string(selectedTarget), func(t *testing.T) {
			want := aggregate.MCPPlacementsForTarget(selectedTarget)
			allocations := testing.AllocsPerRun(100, func() {
				if got := aggregate.MCPPlacementsForTarget(selectedTarget); len(got) != len(want) {
					t.Fatal("target selection changed")
				}
			})
			if allocations > 2 {
				t.Fatalf("target selection allocates %.0f times, want at most 2 without a full catalog copy", allocations)
			}
		})
	}
}
