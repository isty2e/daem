package catalog

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/hostsurface"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/profile"
	topologyextension "github.com/isty2e/daem/internal/topology/extension"
)

func TestProductExtensionSurfacesMatchCarrierRouteNamespaceAndOrderOwners(t *testing.T) {
	t.Parallel()

	catalog := Product()
	carriers := desiredextension.SupportedCarriers()
	views := catalog.ExtensionSurfaces()
	wantCount := 0
	for _, carrier := range carriers {
		wantCount += len(carrier.AdmittedScopes())
	}
	if len(views) != wantCount || wantCount != 8 {
		t.Fatalf("Extension views = %d, want %d", len(views), wantCount)
	}

	ownerOrder := catalog.ExtensionsInOwnerOrder()
	ownerIndex := 0
	for _, carrier := range carriers {
		selectedTarget, ok := carrier.AdmittedTarget()
		if !ok {
			t.Fatalf("carrier %q has no target", carrier)
		}
		sourceKind, ok := carrier.RequiredSourceKind()
		if !ok {
			t.Fatalf("carrier %q has no source kind", carrier)
		}
		namespace, ok := topologyextension.CarrierNamespace(carrier)
		if !ok {
			t.Fatalf("carrier %q has no namespace", carrier)
		}
		ownerRoute, ok := profile.Profile(selectedTarget).DelegatedRoute(carrier)
		if !ok {
			t.Fatalf("carrier %q has no owner route profile", carrier)
		}
		targetViews := catalog.ExtensionViewsForTarget(selectedTarget)
		if len(targetViews) != len(carrier.AdmittedScopes()) {
			t.Fatalf("carrier %q target views = %d", carrier, len(targetViews))
		}
		variant, err := hostsurface.ParseVariantID(string(carrier))
		if err != nil {
			t.Fatal(err)
		}
		for _, scope := range carrier.AdmittedScopes() {
			key, err := hostsurface.NewSurfaceKey(
				selectedTarget,
				scope,
				entity.KindExtension,
				variant,
			)
			if err != nil {
				t.Fatal(err)
			}
			view, ok := catalog.LookupExtension(key)
			if !ok {
				t.Fatalf("missing Extension surface %s", hostsurface.MustSurfaceID(key))
			}
			byID, ok := catalog.ExtensionSurface(view.ID())
			if !ok || byID.ID() != view.ID() {
				t.Fatalf("Extension ID lookup mismatch for %s", view.ID())
			}
			byCell, ok := catalog.LookupExtensionCell(selectedTarget, scope, carrier)
			if !ok || byCell.ID() != view.ID() {
				t.Fatalf("Extension cell lookup mismatch for %s", view.ID())
			}
			if view.Carrier() != carrier || view.RequiredSourceKind() != sourceKind || view.Namespace() != namespace {
				t.Fatalf("Extension owner facts mismatch for %s", view.ID())
			}
			if view.RealizationKind() != realization.RealizationDelegatedRelation {
				t.Fatalf("Extension realization = %q", view.RealizationKind())
			}
			gotRoute := view.RouteProfile()
			if gotRoute.Carrier() != ownerRoute.Carrier() ||
				gotRoute.Target() != ownerRoute.Target() ||
				!slices.Equal(gotRoute.AdmittedScopes(), ownerRoute.AdmittedScopes()) ||
				!slices.Equal(gotRoute.OperationRoutes(), ownerRoute.OperationRoutes()) ||
				gotRoute.PartialRemovalCoverage() != ownerRoute.PartialRemovalCoverage() ||
				!slices.Equal(gotRoute.VerifiedRelationFields(), ownerRoute.VerifiedRelationFields()) {
				t.Fatalf("Extension route profile mismatch for %s", view.ID())
			}
			wantOrder, wantOrderOK := profile.Profile(selectedTarget).ExtensionOrder(carrier, scope)
			gotOrder, gotOrderOK := view.OrderCapability()
			if gotOrderOK != wantOrderOK {
				t.Fatalf("Extension order presence = %v, want %v for %s", gotOrderOK, wantOrderOK, view.ID())
			}
			if gotOrderOK && (gotOrder.Carrier() != wantOrder.Carrier() ||
				gotOrder.Scope() != wantOrder.Scope() ||
				gotOrder.ClassID() != wantOrder.ClassID() ||
				gotOrder.MemberIdentityContract() != wantOrder.MemberIdentityContract() ||
				gotOrder.SequenceMembership() != wantOrder.SequenceMembership() ||
				!slices.Equal(gotOrder.PhysicalSequenceIDs(), wantOrder.PhysicalSequenceIDs()) ||
				gotOrder.RuntimeMeaning() != wantOrder.RuntimeMeaning()) {
				t.Fatalf("Extension order mismatch for %s", view.ID())
			}
			if ownerOrder[ownerIndex].ID() != view.ID() {
				t.Fatalf("Extension owner order[%d] mismatch", ownerIndex)
			}
			ownerIndex++
		}
	}
}

func TestCompileExtensionSurfacesRejectsIncompleteOwnerJoins(t *testing.T) {
	t.Parallel()

	t.Run("duplicate carrier", func(t *testing.T) {
		seed := productExtensionSeed()
		seed.carriers = append(seed.carriers, seed.carriers[0])
		_, _, err := compileExtensionSurfaces(seed)
		if err == nil || !strings.Contains(err.Error(), "duplicate carrier") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("missing namespace", func(t *testing.T) {
		seed := productExtensionSeed()
		delete(seed.namespaces, desiredextension.CarrierClaudeCodePlugin)
		_, _, err := compileExtensionSurfaces(seed)
		if err == nil || !strings.Contains(err.Error(), "has no topology namespace") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("missing route profile", func(t *testing.T) {
		seed := productExtensionSeed()
		seed.routes = seed.routes[1:]
		_, _, err := compileExtensionSurfaces(seed)
		if err == nil || !strings.Contains(err.Error(), "has no exact delegated route profile") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("shared namespace", func(t *testing.T) {
		seed := productExtensionSeed()
		seed.namespaces[desiredextension.CarrierCodexPlugin] = seed.namespaces[desiredextension.CarrierClaudeCodePlugin]
		_, _, err := compileExtensionSurfaces(seed)
		if err == nil || !strings.Contains(err.Error(), "topology namespace") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("unreferenced order capability", func(t *testing.T) {
		seed := productExtensionSeed()
		seed.carriers = slices.DeleteFunc(seed.carriers, func(carrier desiredextension.Carrier) bool {
			return carrier == desiredextension.CarrierOpenCodePlugin
		})
		seed.routes = slices.DeleteFunc(seed.routes, func(routeProfile profile.DelegatedRouteProfile) bool {
			return routeProfile.Carrier() == desiredextension.CarrierOpenCodePlugin
		})
		delete(seed.namespaces, desiredextension.CarrierOpenCodePlugin)
		_, _, err := compileExtensionSurfaces(seed)
		if err == nil || !strings.Contains(err.Error(), "referenced 2 of 4 order capabilities") {
			t.Fatalf("error = %v", err)
		}
	})
}
