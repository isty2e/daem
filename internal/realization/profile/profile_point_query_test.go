package profile

import (
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

func TestProfilePointQueriesMatchEnumerations(t *testing.T) {
	paths := []string{"", "missing", "./AGENTS.md"}
	for _, placements := range [...][]ManagedPathPlacement{instructionPlacements, skillPlacements} {
		for _, placement := range placements {
			paths = append(paths, placement.Root().String())
		}
	}
	for _, selectedTarget := range append(target.SupportedTargets(), target.Target("future-agent"), target.Target("")) {
		t.Run(string(selectedTarget), func(t *testing.T) {
			selected := Profile(selectedTarget)
			for _, kind := range resourceKinds {
				for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal, "unknown"} {
					for _, path := range paths {
						var placement ManagedPathPlacement
						count := 0
						for _, candidate := range selected.Placements(kind, scope) {
							if candidate.Root().String() == path {
								placement = candidate
								count++
							}
						}
						var want SelectedManagedPathPlacement
						if count == 1 {
							var err error
							want, err = newSelectedManagedPathPlacement(placement, []target.Target{selectedTarget})
							if err != nil {
								t.Fatal(err)
							}
						}
						if got, ok := selected.PlacementAt(kind, scope, path); ok != (count == 1) || !reflect.DeepEqual(got, want) {
							t.Fatalf("placement %s/%s/%q = %#v/%t, want %#v/%t", kind, scope, path, got, ok, want, count == 1)
						}

						var wantAdmission PlacementAdmission
						found := false
						for _, admission := range selected.PlacementAdmissions(kind, scope) {
							candidate, ok := selected.placement(admission.PlacementID())
							if ok && candidate.Root().String() == path {
								wantAdmission, found = admission, true
								break
							}
						}
						if got, ok := selected.PlacementAdmissionAt(kind, scope, path); ok != found || got != wantAdmission {
							t.Fatalf("admission %s/%s/%q = %#v/%t, want %#v/%t", kind, scope, path, got, ok, wantAdmission, found)
						}
					}
				}
			}
		})
	}
}

func TestProfilePointQueriesPreserveMultiplicityAndAdmissionOrder(t *testing.T) {
	selected := Profile(target.TargetCodex)
	placement := selected.Placements(entity.KindSkill, target.ScopeProject)[0]
	selected.placements = []ManagedPathPlacement{placement, placement}
	if got, ok := selected.PlacementAt(entity.KindSkill, target.ScopeProject, placement.Root().String()); ok || !reflect.DeepEqual(got, SelectedManagedPathPlacement{}) {
		t.Fatalf("ambiguous placement = %#v/%t, want zero/false", got, ok)
	}

	selected.placements = []ManagedPathPlacement{placement}
	selected.admissions = []PlacementAdmission{
		mustPlacementAdmission(target.TargetCodex, placement.ID(), false),
		mustPlacementAdmission(target.TargetCodex, placement.ID(), true),
	}
	if got, ok := selected.PlacementAdmissionAt(entity.KindSkill, target.ScopeProject, placement.Root().String()); !ok || got != selected.admissions[0] {
		t.Fatalf("admission = %#v/%t, want first matching admission", got, ok)
	}
}

func TestPlacementAdmissionAtDoesNotAllocate(t *testing.T) {
	selected := Profile(target.TargetCodex)
	placement := selected.Placements(entity.KindSkill, target.ScopeProject)[0]
	for _, path := range []string{placement.Root().String(), "missing"} {
		t.Run(path, func(t *testing.T) {
			want, found := selected.PlacementAdmissionAt(entity.KindSkill, target.ScopeProject, path)
			allocations := testing.AllocsPerRun(100, func() {
				if got, ok := selected.PlacementAdmissionAt(entity.KindSkill, target.ScopeProject, path); ok != found || got != want {
					t.Fatal("admission changed")
				}
			})
			if allocations != 0 {
				t.Fatalf("point admission lookup allocates %.0f times, want no filtered-list construction", allocations)
			}
		})
	}
}

func BenchmarkProfilePlacementAt(b *testing.B) {
	selected := Profile(target.TargetCodex)
	path := selected.Placements(entity.KindSkill, target.ScopeProject)[0].Root().String()
	for _, path := range []string{path, "missing"} {
		b.Run(path, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, ok := selected.PlacementAt(entity.KindSkill, target.ScopeProject, path); ok != (path != "missing") {
					b.Fatal("placement admission changed")
				}
			}
		})
	}
}

func BenchmarkProfilePlacementAdmissionAt(b *testing.B) {
	selected := Profile(target.TargetCodex)
	path := selected.Placements(entity.KindSkill, target.ScopeProject)[0].Root().String()
	for _, path := range []string{path, "missing"} {
		b.Run(path, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, ok := selected.PlacementAdmissionAt(entity.KindSkill, target.ScopeProject, path); ok != (path != "missing") {
					b.Fatal("placement admission changed")
				}
			}
		})
	}
}
