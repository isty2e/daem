package profile

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
)

func TestDelegatedRouteProfilesSeparateOperationIdentity(t *testing.T) {
	if len(delegatedRouteProfiles) != len(desiredextension.SupportedCarriers()) {
		t.Fatalf("delegated profiles = %d, supported carriers = %d",
			len(delegatedRouteProfiles), len(desiredextension.SupportedCarriers()))
	}
	for _, routeProfile := range delegatedRouteProfiles {
		t.Run(string(routeProfile.Carrier()), func(t *testing.T) {
			if err := routeProfile.Validate(); err != nil {
				t.Fatalf("Validate returned error: %v", err)
			}
			routes := routeProfile.OperationRoutes()
			wantRemove := routeProfile.Carrier() == desiredextension.CarrierClaudeCodePlugin ||
				routeProfile.Carrier() == desiredextension.CarrierCodexPlugin ||
				routeProfile.Carrier() == desiredextension.CarrierOpenCodePlugin ||
				routeProfile.Carrier() == desiredextension.CarrierPiPackage ||
				routeProfile.Carrier() == desiredextension.CarrierAntigravityCLIPlugin
			wantRouteCount := 2
			if wantRemove {
				wantRouteCount++
			}
			if len(routes) != wantRouteCount {
				t.Fatalf("operation routes = %#v, want %d routes", routes, wantRouteCount)
			}
			install, hasInstall := routeProfile.OperationRoute(OperationInstall)
			refresh, hasRefresh := routeProfile.OperationRoute(OperationRefresh)
			if !hasInstall || !hasRefresh {
				t.Fatalf("routes = %#v, want unique install and refresh", routes)
			}
			if install.ResourceKind() != entity.KindExtension ||
				refresh.ResourceKind() != entity.KindExtension ||
				install.CorrelationID() != refresh.CorrelationID() {
				t.Fatalf("install/refresh resource and correlation = %#v/%#v", install, refresh)
			}
			if install.RouteID() == refresh.RouteID() ||
				install.AdapterContractVersion() == refresh.AdapterContractVersion() {
				t.Fatalf("install/refresh identity = %#v/%#v, want distinct route and adapter contracts", install, refresh)
			}
			remove, hasRemove := routeProfile.OperationRoute(OperationRemove)
			if hasRemove != wantRemove {
				t.Fatalf("remove route present = %t, want %t", hasRemove, wantRemove)
			}
			if hasRemove {
				if remove.ResourceKind() != entity.KindExtension ||
					remove.CorrelationID() != install.CorrelationID() ||
					remove.RouteID() == install.RouteID() ||
					remove.RouteID() == refresh.RouteID() ||
					remove.AdapterContractVersion() == install.AdapterContractVersion() ||
					remove.AdapterContractVersion() == refresh.AdapterContractVersion() {
					t.Fatalf("remove route identity = %#v, install/refresh = %#v/%#v", remove, install, refresh)
				}
			}

			targetProfile := Profile(routeProfile.Target())
			selectedInstall, ok := targetProfile.OperationRoute(
				entity.KindExtension,
				install.CorrelationID(),
				OperationInstall,
			)
			if !ok || selectedInstall != install {
				t.Fatalf("target install lookup = %#v/%t, want %#v", selectedInstall, ok, install)
			}
			selectedRefresh, ok := targetProfile.OperationRoute(
				entity.KindExtension,
				refresh.CorrelationID(),
				OperationRefresh,
			)
			if !ok || selectedRefresh != refresh {
				t.Fatalf("target refresh lookup = %#v/%t, want %#v", selectedRefresh, ok, refresh)
			}
			selectedRemove, selected := targetProfile.OperationRoute(
				entity.KindExtension,
				install.CorrelationID(),
				OperationRemove,
			)
			if selected != wantRemove || (selected && selectedRemove != remove) {
				t.Fatalf(
					"target remove lookup = %#v/%t, want %#v/%t",
					selectedRemove,
					selected,
					remove,
					wantRemove,
				)
			}
		})
	}
}

func TestAntigravityRemovalProfileAdmitsOnlyExactMarketplaceSelectorClass(t *testing.T) {
	routeProfile, ok := Profile(target.TargetAntigravityCLI).DelegatedRoute(
		desiredextension.CarrierAntigravityCLIPlugin,
	)
	if !ok {
		t.Fatal("Antigravity delegated route profile is missing")
	}
	tests := []struct {
		name     string
		source   string
		admitted bool
	}{
		{name: "selector", source: "modern-web-guidance@google", admitted: true},
		{name: "local path", source: "./plugins/guidance", admitted: false},
		{name: "path with at sign", source: "./plugins/guidance@local", admitted: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, err := desiredextension.NewSourceRef(
				desiredextension.SourceKindHostSource,
				test.source,
			)
			if err != nil {
				t.Fatal(err)
			}
			key, err := desiredextension.NewCarrierKey(
				desiredextension.CarrierAntigravityCLIPlugin,
				target.TargetAntigravityCLI,
				target.ScopeGlobal,
				source,
			)
			if err != nil {
				t.Fatal(err)
			}
			dossier, admitted, err := routeProfile.RemovalDossier(key)
			if err != nil {
				t.Fatal(err)
			}
			if admitted != test.admitted {
				t.Fatalf("admitted = %t, want %t", admitted, test.admitted)
			}
			if admitted &&
				!slices.Contains(
					dossier.RemovedEffects(),
					"selected_antigravity_cli_plugin_import_relation",
				) {
				t.Fatalf("Antigravity removal dossier = %#v", dossier)
			}
		})
	}
}

func TestDelegatedRouteProfileValidationRejectsOperationAmbiguity(t *testing.T) {
	valid, ok := Profile(target.TargetCodex).DelegatedRoute(desiredextension.CarrierCodexPlugin)
	if !ok {
		t.Fatal("Codex delegated route profile is missing")
	}
	valid = cloneDelegatedRouteProfile(valid)
	install, _ := valid.OperationRoute(OperationInstall)
	refresh, _ := valid.OperationRoute(OperationRefresh)

	for _, routes := range [][]OperationRoute{{install}, {refresh}} {
		candidate := cloneDelegatedRouteProfile(valid)
		candidate.operationRoutes = routes
		candidate.removalDossiers = nil
		if err := candidate.Validate(); err != nil {
			t.Fatalf("operation-independent profile %v is invalid: %v", routes, err)
		}
	}

	tests := []struct {
		name    string
		profile DelegatedRouteProfile
	}{
		{name: "missing operation coverage", profile: func() DelegatedRouteProfile {
			candidate := cloneDelegatedRouteProfile(valid)
			candidate.operationRoutes = nil
			return candidate
		}()},
		{name: "scope mismatch", profile: func() DelegatedRouteProfile {
			candidate := cloneDelegatedRouteProfile(valid)
			candidate.allowedScopes = []target.Scope{target.ScopeProject}
			return candidate
		}()},
		{name: "duplicate operation", profile: func() DelegatedRouteProfile {
			candidate := cloneDelegatedRouteProfile(valid)
			candidate.operationRoutes = append(candidate.operationRoutes, install)
			return candidate
		}()},
		{name: "conflicting correlation", profile: func() DelegatedRouteProfile {
			candidate := cloneDelegatedRouteProfile(valid)
			candidate.operationRoutes[1] = mustOperationRoute(
				entity.KindExtension,
				OperationRefresh,
				"foreign-correlation",
				refresh.RouteID(),
				refresh.AdapterContractVersion(),
			)
			return candidate
		}()},
		{name: "wrong resource kind", profile: func() DelegatedRouteProfile {
			candidate := cloneDelegatedRouteProfile(valid)
			candidate.operationRoutes[1] = mustOperationRoute(
				entity.KindSkill,
				OperationRefresh,
				refresh.CorrelationID(),
				refresh.RouteID(),
				refresh.AdapterContractVersion(),
			)
			return candidate
		}()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.profile.Validate(); err == nil {
				t.Fatalf("Validate(%s) returned nil", test.name)
			}
		})
	}
}

func TestDelegatedRouteCatalogRejectsCrossCarrierIdentityCollisions(t *testing.T) {
	first := cloneDelegatedRouteProfile(delegatedRouteProfiles[0])
	second := cloneDelegatedRouteProfile(delegatedRouteProfiles[1])
	firstRefresh, _ := first.OperationRoute(OperationRefresh)
	secondRefresh, _ := second.OperationRoute(OperationRefresh)

	duplicateRoute := cloneDelegatedRouteProfile(second)
	duplicateRoute.operationRoutes[1] = mustOperationRoute(
		entity.KindExtension,
		OperationRefresh,
		secondRefresh.CorrelationID(),
		firstRefresh.RouteID(),
		secondRefresh.AdapterContractVersion(),
	)
	if err := validateDelegatedRouteProfileCatalog([]DelegatedRouteProfile{first, duplicateRoute}); err == nil {
		t.Fatal("catalog accepted a route id shared by carriers")
	}

	duplicateCorrelation := cloneDelegatedRouteProfile(second)
	duplicateCorrelation.operationRoutes = []OperationRoute{
		mustOperationRoute(
			entity.KindExtension,
			OperationInstall,
			firstRefresh.CorrelationID(),
			duplicateCorrelation.operationRoutes[0].RouteID(),
			duplicateCorrelation.operationRoutes[0].AdapterContractVersion(),
		),
		mustOperationRoute(
			entity.KindExtension,
			OperationRefresh,
			firstRefresh.CorrelationID(),
			secondRefresh.RouteID(),
			secondRefresh.AdapterContractVersion(),
		),
	}
	if err := validateDelegatedRouteProfileCatalog([]DelegatedRouteProfile{first, duplicateCorrelation}); err == nil {
		t.Fatal("catalog accepted a correlation id shared by carriers")
	}
}
