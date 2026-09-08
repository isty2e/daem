package profile

import (
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

func TestConcurrentProfilesKeepCallerOwnedQueryResults(t *testing.T) {
	for _, selectedTarget := range append(target.SupportedTargets(), target.Target("future-agent")) {
		want := Profile(selectedTarget)
		for range 4 {
			t.Run(string(selectedTarget), func(t *testing.T) {
				t.Parallel()
				for range 10 {
					got := Profile(selectedTarget)
					clear(got.ResourceSupports())
					for _, kind := range []entity.Kind{entity.KindInstructions, entity.KindSkill} {
						for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
							clear(got.Placements(kind, scope))
							clear(got.PlacementAdmissions(kind, scope))
							clear(got.DiscoveryLocations(kind, scope))
							clear(got.RuntimeLocations(kind, scope))
						}
					}
					if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(Profile(selectedTarget), want) {
						t.Fatal("caller mutation changed this or another profile")
					}
				}
			})
		}
	}
}

func TestProfileConstructionAllocationBudget(t *testing.T) {
	for _, selectedTarget := range append(target.SupportedTargets(), target.Target("future-agent")) {
		t.Run(string(selectedTarget), func(t *testing.T) {
			allocations := testing.AllocsPerRun(100, func() {
				if got := Profile(selectedTarget); got.selectedTarget != selectedTarget {
					t.Fatal("profile target changed")
				}
			})
			if allocations > 80 {
				t.Fatalf("profile construction allocates %.0f times, want at most 80 without repeated catalog reconstruction", allocations)
			}
		})
	}
}

func BenchmarkProfile(b *testing.B) {
	for _, selectedTarget := range append(target.SupportedTargets(), target.Target("future-agent")) {
		b.Run(string(selectedTarget), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if got := Profile(selectedTarget); got.selectedTarget != selectedTarget {
					b.Fatal("profile target changed")
				}
			}
		})
	}
}

func BenchmarkTargetSupports(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if !TargetSupports(target.TargetCodex, entity.KindSkill) {
			b.Fatal("skill support changed")
		}
	}
}

func BenchmarkProfileParallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if got := Profile(target.TargetCodex); got.selectedTarget != target.TargetCodex {
				b.Error("profile target changed")
				return
			}
		}
	})
}
