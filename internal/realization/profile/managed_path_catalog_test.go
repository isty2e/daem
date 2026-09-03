package profile

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

func TestStaticManagedPathFacetsPreserveOwnerCatalogs(t *testing.T) {
	t.Parallel()

	catalog := StaticManagedPathFacets()
	if got, want := len(catalog.Placements()), len(instructionPlacements)+len(skillPlacements); got != want {
		t.Fatalf("placements = %d, want %d", got, want)
	}
	if got, want := len(catalog.Admissions()), len(instructionPlacementAdmissions)+len(skillPlacementAdmissions); got != want {
		t.Fatalf("admissions = %d, want %d", got, want)
	}
	if got, want := len(catalog.Discoveries()), len(instructionDiscoveries)+len(skillDiscoveries); got != want {
		t.Fatalf("discoveries = %d, want %d", got, want)
	}
	if got, want := len(catalog.RuntimeLocations()), len(instructionRuntimeLocations)+len(skillRuntimeLocations); got != want {
		t.Fatalf("runtime locations = %d, want %d", got, want)
	}
	if got, want := len(catalog.OperationRoutes()), 2*(len(instructionPlacements)+len(skillPlacements)); got != want {
		t.Fatalf("operation routes = %d, want %d", got, want)
	}

	placements := catalog.Placements()
	placements[0] = ManagedPathPlacement{}
	if StaticManagedPathFacets().Placements()[0] == (ManagedPathPlacement{}) {
		t.Fatal("placement catalog shares caller-owned backing storage")
	}
	admissions := catalog.Admissions()
	admissions[0] = PlacementAdmission{}
	if StaticManagedPathFacets().Admissions()[0] == (PlacementAdmission{}) {
		t.Fatal("admission catalog shares caller-owned backing storage")
	}
}

func TestResourceSupportFactsMatchDirectOwnerQueries(t *testing.T) {
	t.Parallel()

	facts := ResourceSupportFacts()
	if len(facts) != len(target.SupportedTargets())*len(resourceKinds) {
		t.Fatalf("support facts = %d", len(facts))
	}
	index := 0
	for _, selectedTarget := range target.SupportedTargets() {
		for _, resourceKind := range []entity.Kind{entity.KindInstructions, entity.KindSkill, entity.KindHook} {
			fact, ok := TargetSupport(selectedTarget, resourceKind)
			if !ok || facts[index] != fact {
				t.Fatalf("support fact[%d] mismatch for %s/%s", index, selectedTarget, resourceKind)
			}
			index++
		}
	}
	if _, ok := TargetSupport(target.Target("unknown"), entity.KindSkill); ok {
		t.Fatal("unknown target returned a support fact")
	}
}
