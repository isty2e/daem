package profile

import (
	"slices"
	"testing"
)

func TestManagedRouteSequencePreservesOrderAndStops(t *testing.T) {
	routes := instructionOperationRoutes()
	var want []OperationRoute
	for _, placement := range instructionPlacements {
		want = append(
			want,
			mustOperationRoute(placement.ResourceKind(), OperationWrite, placement.ID(), managedInstructionWriteRoute, managedInstructionAdapterVersion),
			mustOperationRoute(placement.ResourceKind(), OperationRemove, placement.ID(), instructionManagedPathRemoveRoute, managedInstructionAdapterVersion),
		)
	}
	for limit := 1; limit <= len(want); limit++ {
		var got []OperationRoute
		routes(func(route OperationRoute) bool {
			got = append(got, route)
			return len(got) < limit
		})
		if !slices.Equal(got, want[:limit]) {
			t.Fatalf("stopped after %d: got %#v, want %#v", limit, got, want[:limit])
		}
		if got := slices.Collect(routes); !slices.Equal(got, want) {
			t.Fatalf("repeated traversal changed routes: %#v", got)
		}
	}
}

func TestManagedRouteCatalogCollectsIndependentOrderedValues(t *testing.T) {
	want := slices.AppendSeq(slices.Collect(instructionOperationRoutes()), skillOperationRoutes())
	catalog := StaticManagedPathFacets()
	got := catalog.OperationRoutes()
	if !slices.Equal(got, want) {
		t.Fatalf("catalog routes = %#v, want ordered routes %#v", got, want)
	}
	clear(got)
	if !slices.Equal(catalog.OperationRoutes(), want) || !slices.Equal(StaticManagedPathFacets().OperationRoutes(), want) {
		t.Fatal("caller mutation changed catalog routes")
	}
}

func TestManagedRouteIterationDoesNotAllocate(t *testing.T) {
	allocations := testing.AllocsPerRun(100, func() {
		count := 0
		for range instructionOperationRoutes() {
			count++
		}
		for range skillOperationRoutes() {
			count++
		}
		if count != 2*(len(instructionPlacements)+len(skillPlacements)) {
			t.Fatal("route count changed")
		}
	})
	if allocations != 0 {
		t.Fatalf("route iteration allocates %.0f times, want no intermediate route arrays", allocations)
	}
}
