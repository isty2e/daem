package profile

import (
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestSurfaceOwnerSnapshotsAreCompleteAndDefensive(t *testing.T) {
	t.Parallel()

	delegated := DelegatedRouteProfiles()
	if len(delegated) != len(desiredextension.SupportedCarriers()) {
		t.Fatalf("delegated route profiles = %d", len(delegated))
	}
	delegated[0] = DelegatedRouteProfile{}
	if DelegatedRouteProfiles()[0].Carrier() == "" {
		t.Fatal("delegated route catalog shares caller-owned storage")
	}
	routes := DelegatedRouteProfiles()[0].OperationRoutes()
	routes[0] = OperationRoute{}
	if DelegatedRouteProfiles()[0].OperationRoutes()[0] == (OperationRoute{}) {
		t.Fatal("delegated route profile shares operation-route storage")
	}

	orders := ExtensionOrderAdmissions()
	if len(orders) != 4 {
		t.Fatalf("extension order admissions = %d", len(orders))
	}
	sequenceIDs := orders[0].Capability().PhysicalSequenceIDs()
	sequenceIDs[0] = ""
	if ExtensionOrderAdmissions()[0].Capability().PhysicalSequenceIDs()[0] == "" {
		t.Fatal("extension order snapshot shares physical-sequence storage")
	}

	assets := ImplementedHookAssetPlacements()
	if len(assets) != 2 || len(assets[0].ConsumerTargets()) != 2 {
		t.Fatalf("HookAsset placement catalog = %#v", assets)
	}
	consumers := assets[0].ConsumerTargets()
	consumers[0] = ""
	if ImplementedHookAssetPlacements()[0].ConsumerTargets()[0] == "" {
		t.Fatal("HookAsset placement shares consumer-target storage")
	}

	for _, placement := range aggregate.ImplementedHookPlacements() {
		write, remove, ok := HookAggregateRouteIDs(placement.ID())
		if !ok || write == "" || remove == "" {
			t.Fatalf("Hook placement %q has no route pair", placement.ID())
		}
	}
}
