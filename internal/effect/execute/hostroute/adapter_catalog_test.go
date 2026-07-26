package hostroute

import (
	"slices"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
)

func TestCommandAdapterCatalogMatchesAdmittedDelegatedRouteProfiles(t *testing.T) {
	type adapterKey struct {
		carrier   desiredextension.Carrier
		operation lock.OperationKind
	}
	type expectedAdapter struct {
		target  target.Target
		scopes  []target.Scope
		routeID string
	}
	expected := map[adapterKey]expectedAdapter{
		{carrier: desiredextension.CarrierClaudeCodePlugin, operation: lock.OperationInstall}: {
			target:  target.TargetClaudeCode,
			scopes:  []target.Scope{target.ScopeGlobal, target.ScopeProject},
			routeID: "claude-code.plugin-carrier.install",
		},
		{carrier: desiredextension.CarrierClaudeCodePlugin, operation: lock.OperationRefresh}: {
			target:  target.TargetClaudeCode,
			scopes:  []target.Scope{target.ScopeGlobal, target.ScopeProject},
			routeID: "claude-code.plugin-carrier.refresh",
		},
		{carrier: desiredextension.CarrierClaudeCodePlugin, operation: lock.OperationRemove}: {
			target:  target.TargetClaudeCode,
			scopes:  []target.Scope{target.ScopeGlobal, target.ScopeProject},
			routeID: "claude-code.plugin-carrier.remove",
		},
		{carrier: desiredextension.CarrierCodexPlugin, operation: lock.OperationInstall}: {
			target:  target.TargetCodex,
			scopes:  []target.Scope{target.ScopeGlobal},
			routeID: "codex.plugin-carrier.install",
		},
		{carrier: desiredextension.CarrierCodexPlugin, operation: lock.OperationRefresh}: {
			target:  target.TargetCodex,
			scopes:  []target.Scope{target.ScopeGlobal},
			routeID: "codex.plugin-marketplace.refresh",
		},
		{carrier: desiredextension.CarrierCodexPlugin, operation: lock.OperationRemove}: {
			target:  target.TargetCodex,
			scopes:  []target.Scope{target.ScopeGlobal},
			routeID: "codex.plugin-carrier.remove",
		},
		{carrier: desiredextension.CarrierOpenCodePlugin, operation: lock.OperationInstall}: {
			target:  target.TargetOpenCode,
			scopes:  []target.Scope{target.ScopeGlobal, target.ScopeProject},
			routeID: "opencode.plugin-carrier.install",
		},
		{carrier: desiredextension.CarrierOpenCodePlugin, operation: lock.OperationRefresh}: {
			target:  target.TargetOpenCode,
			scopes:  []target.Scope{target.ScopeGlobal, target.ScopeProject},
			routeID: "opencode.plugin-carrier.refresh",
		},
		{carrier: desiredextension.CarrierPiPackage, operation: lock.OperationInstall}: {
			target:  target.TargetPi,
			scopes:  []target.Scope{target.ScopeGlobal, target.ScopeProject},
			routeID: "pi.package-carrier.install",
		},
		{carrier: desiredextension.CarrierPiPackage, operation: lock.OperationRefresh}: {
			target:  target.TargetPi,
			scopes:  []target.Scope{target.ScopeGlobal, target.ScopeProject},
			routeID: "pi.package-carrier.refresh",
		},
		{carrier: desiredextension.CarrierPiPackage, operation: lock.OperationRemove}: {
			target:  target.TargetPi,
			scopes:  []target.Scope{target.ScopeGlobal, target.ScopeProject},
			routeID: "pi.package-carrier.remove",
		},
		{carrier: desiredextension.CarrierAntigravityCLIPlugin, operation: lock.OperationInstall}: {
			target:  target.TargetAntigravityCLI,
			scopes:  []target.Scope{target.ScopeGlobal},
			routeID: "antigravity-cli.plugin-carrier.install",
		},
		{carrier: desiredextension.CarrierAntigravityCLIPlugin, operation: lock.OperationRefresh}: {
			target:  target.TargetAntigravityCLI,
			scopes:  []target.Scope{target.ScopeGlobal},
			routeID: "antigravity-cli.plugin-carrier.refresh",
		},
		{carrier: desiredextension.CarrierAntigravityCLIPlugin, operation: lock.OperationRemove}: {
			target:  target.TargetAntigravityCLI,
			scopes:  []target.Scope{target.ScopeGlobal},
			routeID: "antigravity-cli.plugin-carrier.remove",
		},
	}

	if err := validateCommandAdapterCatalog(commandAdapters); err != nil {
		t.Fatalf("validateCommandAdapterCatalog returned error: %v", err)
	}
	if len(commandAdapters) != len(expected) {
		t.Fatalf("command adapter count = %d, want %d admitted carrier profiles", len(commandAdapters), len(expected))
	}
	seen := make(map[adapterKey]struct{}, len(commandAdapters))
	for _, adapter := range commandAdapters {
		if adapter.label == "" || adapter.build == nil {
			t.Fatalf("command adapter = %#v, want non-empty label and builder", adapter)
		}
		if err := adapter.profile.Validate(); err != nil {
			t.Fatalf("command adapter %q profile is invalid: %v", adapter.label, err)
		}
		carrier := adapter.profile.Carrier()
		key := adapterKey{carrier: carrier, operation: adapter.operation}
		want, ok := expected[key]
		if !ok {
			t.Fatalf("command adapter %q has unexpected carrier/operation %q/%q",
				adapter.label, carrier, adapter.operation)
		}
		if _, duplicate := seen[key]; duplicate {
			t.Fatalf("duplicate command adapter for carrier/operation %q/%q", carrier, adapter.operation)
		}
		seen[key] = struct{}{}
		profileOperation, ok := profileOperationForHostOperation(adapter.operation)
		if !ok {
			t.Fatalf("command adapter %q operation %q has no profile mapping", adapter.label, adapter.operation)
		}
		operationRoute, ok := adapter.profile.OperationRoute(profileOperation)
		if !ok {
			t.Fatalf("command adapter %q %q route is missing", adapter.label, adapter.operation)
		}
		if adapter.profile.Target() != want.target ||
			!slices.Equal(carrier.AdmittedScopes(), want.scopes) ||
			operationRoute.RouteID() != want.routeID {
			t.Fatalf(
				"command adapter %q profile target/scopes/route = %q/%v/%q, want %q/%v/%q",
				adapter.label,
				adapter.profile.Target(),
				carrier.AdmittedScopes(),
				operationRoute.RouteID(),
				want.target,
				want.scopes,
				want.routeID,
			)
		}
	}
	for key := range expected {
		if _, ok := seen[key]; !ok {
			t.Errorf("admitted carrier/operation %q/%q has no command adapter", key.carrier, key.operation)
		}
	}
}

func TestCommandAdapterCatalogRejectsMissingDuplicateAndMalformedRows(t *testing.T) {
	tests := []struct {
		name     string
		adapters []commandAdapter
	}{
		{name: "missing carrier", adapters: func() []commandAdapter {
			adapters := make([]commandAdapter, 0, len(commandAdapters))
			for _, adapter := range commandAdapters {
				if adapter.profile.Carrier() != desiredextension.CarrierAntigravityCLIPlugin {
					adapters = append(adapters, adapter)
				}
			}
			return adapters
		}()},
		{name: "duplicate carrier", adapters: append(append([]commandAdapter(nil), commandAdapters[:len(commandAdapters)-1]...), commandAdapters[0])},
		{name: "missing label", adapters: func() []commandAdapter {
			adapters := append([]commandAdapter(nil), commandAdapters...)
			adapters[0].label = ""
			return adapters
		}()},
		{name: "missing builder", adapters: func() []commandAdapter {
			adapters := append([]commandAdapter(nil), commandAdapters...)
			adapters[0].build = nil
			return adapters
		}()},
		{name: "unsupported operation", adapters: func() []commandAdapter {
			adapters := append([]commandAdapter(nil), commandAdapters...)
			adapters[0].operation = lock.OperationKind("destroy")
			return adapters
		}()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateCommandAdapterCatalog(test.adapters); err == nil {
				t.Fatalf("validateCommandAdapterCatalog(%s) returned nil", test.name)
			}
		})
	}
}

func TestCommandAdapterCatalogAllowsDistinctOperationsForOneCarrier(t *testing.T) {
	if err := validateCommandAdapterCatalog(commandAdapters); err != nil {
		t.Fatalf("validateCommandAdapterCatalog rejected admitted refresh: %v", err)
	}
	if claudePluginCarrierCommandAdapter.operation != lock.OperationInstall ||
		claudePluginCarrierRemoveCommandAdapter.operation != lock.OperationRemove ||
		claudePluginCarrierRefreshCommandAdapter.operation != lock.OperationRefresh ||
		codexPluginCarrierRemoveCommandAdapter.operation != lock.OperationRemove ||
		piPackageCarrierRemoveCommandAdapter.operation != lock.OperationRemove ||
		antigravityCLIPluginCarrierRemoveCommandAdapter.operation != lock.OperationRemove {
		t.Fatalf(
			"Claude install/remove/refresh, Codex/Pi/Antigravity remove operations = %q/%q/%q/%q/%q/%q",
			claudePluginCarrierCommandAdapter.operation,
			claudePluginCarrierRemoveCommandAdapter.operation,
			claudePluginCarrierRefreshCommandAdapter.operation,
			codexPluginCarrierRemoveCommandAdapter.operation,
			piPackageCarrierRemoveCommandAdapter.operation,
			antigravityCLIPluginCarrierRemoveCommandAdapter.operation,
		)
	}
}

func TestCommandAdapterSelectionNeverFallsBackAcrossOperations(t *testing.T) {
	record, _ := mustCarrierRecordAndRelation(
		t,
		desiredextension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode,
		target.ScopeProject,
		desiredextension.SourceKindMarketplace,
		"context7@official",
		"claude-code.plugin-carrier",
		"context7",
		"context7",
	)
	carrier, admitted, err := lock.DelegatedRelationCarrier(record)
	if err != nil || !admitted {
		t.Fatalf("DelegatedRelationCarrier = (%q, %t, %v)", carrier, admitted, err)
	}
	install, ok := record.OperationContract(lock.OperationInstall)
	if !ok {
		t.Fatal("locked install operation is missing")
	}
	if _, ok := commandAdapterForRoute(carrier, lock.OperationInstall, install.Route()); !ok {
		t.Fatal("install operation did not select the admitted install adapter")
	}
	refreshContract, ok := record.OperationContract(lock.OperationRefresh)
	if !ok {
		t.Fatal("locked refresh operation is missing")
	}
	refresh, ok := commandAdapterForRoute(carrier, lock.OperationRefresh, refreshContract.Route())
	if !ok {
		t.Fatal("refresh operation did not select the admitted refresh adapter")
	}
	if refresh.operation != lock.OperationRefresh ||
		refresh.label == claudePluginCarrierCommandAdapter.label {
		t.Fatalf("refresh adapter = %#v, want distinct refresh operation", refresh)
	}
}
