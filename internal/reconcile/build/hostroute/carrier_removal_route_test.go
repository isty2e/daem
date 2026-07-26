package hostroute

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
)

func TestResolveCurrentCarrierRemovalRouteDerivesPiSourceAndScopeDossiers(t *testing.T) {
	tests := []struct {
		name            string
		scope           target.Scope
		source          string
		wantRequirement effectpostcondition.Requirement
		wantRetained    string
		wantTrust       lock.TrustActivationRequirement
	}{
		{
			name:            "project npm",
			scope:           target.ScopeProject,
			source:          "npm:@acme/pi-tools@1.2.3",
			wantRequirement: effectpostcondition.CarrierArtifactsAbsent,
			wantRetained:    "npm_install_root_metadata",
			wantTrust:       lock.TrustActivationRequired,
		},
		{
			name:            "global git",
			scope:           target.ScopeGlobal,
			source:          "git:github.com/acme/pi-tools.git@v2",
			wantRequirement: effectpostcondition.CarrierArtifactsAbsent,
			wantRetained:    "git_install_root",
			wantTrust:       lock.TrustActivationNotRequired,
		},
		{
			name:            "project local",
			scope:           target.ScopeProject,
			source:          "./tools/pi-local",
			wantRequirement: effectpostcondition.LocalSourceUnchanged,
			wantRetained:    "local_source_directory",
			wantTrust:       lock.TrustActivationRequired,
		},
		{
			name:            "global local",
			scope:           target.ScopeGlobal,
			source:          "/opt/pi-local",
			wantRequirement: effectpostcondition.LocalSourceUnchanged,
			wantRetained:    "local_source_directory",
			wantTrust:       lock.TrustActivationNotRequired,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claim := currentCarrierClaim(
				t,
				desiredextension.CarrierPiPackage,
				target.TargetPi,
				test.scope,
				desiredextension.SourceKindHostSource,
				test.source,
			)
			route, err := ResolveCurrentCarrierRemovalRoute(claim)
			if err != nil {
				t.Fatalf("ResolveCurrentCarrierRemovalRoute returned error: %v", err)
			}
			if route.Status() != carrierabsence.RouteAdmitted {
				t.Fatalf("route status = %q, want admitted", route.Status())
			}
			operation := route.Operation()
			if operation.Operation() != lock.OperationRemove ||
				operation.Route().RouteID != "pi.package-carrier.remove" ||
				operation.TrustActivation() != test.wantTrust {
				t.Fatalf(
					"operation = %q/%#v/%q",
					operation.Operation(),
					operation.Route(),
					operation.TrustActivation(),
				)
			}
			if !slices.Equal(
				operation.EffectPostconditions().Requirements(),
				[]effectpostcondition.Requirement{test.wantRequirement},
			) {
				t.Fatalf(
					"effect postconditions = %#v, want %q",
					operation.EffectPostconditions().Requirements(),
					test.wantRequirement,
				)
			}
			if !slices.Contains(route.RetainedEffects(), test.wantRetained) {
				t.Fatalf("retained effects = %#v, want %q", route.RetainedEffects(), test.wantRetained)
			}
			if route.Request().RouteID() != operation.Route().RouteID ||
				route.Request().ContractVersion() != operation.Route().AdapterContractVersion {
				t.Fatal("route request does not match current remove operation")
			}
		})
	}
}

func TestResolveCurrentCarrierRemovalRouteAdmitsCodexCoupledCacheRemoval(t *testing.T) {
	claim := currentCarrierClaim(
		t,
		desiredextension.CarrierCodexPlugin,
		target.TargetCodex,
		target.ScopeGlobal,
		desiredextension.SourceKindMarketplace,
		"context7@market",
	)
	route, err := ResolveCurrentCarrierRemovalRoute(claim)
	if err != nil {
		t.Fatalf("ResolveCurrentCarrierRemovalRoute returned error: %v", err)
	}
	if route.Status() != carrierabsence.RouteAdmitted {
		t.Fatalf("route status = %q, want admitted", route.Status())
	}
	operation := route.Operation()
	if operation.Route().RouteID != "codex.plugin-carrier.remove" ||
		!slices.Equal(
			operation.EffectPostconditions().Requirements(),
			[]effectpostcondition.Requirement{effectpostcondition.CarrierArtifactsAbsent},
		) ||
		!slices.Contains(route.RetainedEffects(), "marketplace_snapshot") {
		t.Fatalf("Codex removal route = %#v", route)
	}
}

func TestResolveCurrentCarrierRemovalRouteRejectsStaleAcquisitionIdentity(t *testing.T) {
	current := currentCarrierClaim(
		t,
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeProject,
		desiredextension.SourceKindHostSource,
		"git:github.com/acme/pi-tools",
	)
	digest := sha256.Sum256([]byte("stale install route"))
	staleRequest, err := realizationdelegate.NewRequest(
		"pi.package-carrier.install",
		"pi-package-install-v0",
		"sha256:"+hex.EncodeToString(digest[:]),
	)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := durablecarrier.NewManagedCarrierClaim(
		current.Owner(),
		current.Identity(),
		staleRequest,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatal(err)
	}
	route, err := ResolveCurrentCarrierRemovalRoute(stale)
	if err != nil {
		t.Fatalf("ResolveCurrentCarrierRemovalRoute returned error: %v", err)
	}
	if route.Status() != carrierabsence.RouteUnavailable {
		t.Fatalf("route status = %q, want unavailable for stale acquisition identity", route.Status())
	}
}

func TestResolveCurrentCarrierRemovalRouteRejectsInvalidClaim(t *testing.T) {
	if _, err := ResolveCurrentCarrierRemovalRoute(durablecarrier.ManagedCarrierClaim{}); err == nil {
		t.Fatal("ResolveCurrentCarrierRemovalRoute accepted a zero claim")
	}
}

func TestBuildCarrierAbsenceActionsUsesCurrentPiRemovalRoute(t *testing.T) {
	claim := currentCarrierClaim(
		t,
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeProject,
		desiredextension.SourceKindHostSource,
		"npm:@acme/pi-tools@1.2.3",
	)
	expected := claim.Identity().ExpectedRelation()
	row, err := observerelation.NewRow(observerelation.RowSpec{
		SubjectKey:            string(expected.SubjectKey()),
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    string(expected.ManagedInstanceKey()),
	})
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := observerelation.NewInventory(observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows:         []observerelation.Row{row},
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := observerelation.NewCorrelationKey(
		claim.Identity().RelationSubject(),
		expected,
	)
	if err != nil {
		t.Fatal(err)
	}
	observations, err := observerelation.NewBatch(observerelation.BatchSpec{
		Correlations: []observerelation.Correlation{{
			Key:    key,
			Result: observerelation.Correlate(expected, inventory),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	actions, err := BuildCarrierAbsenceActions(CarrierAbsenceInput{
		Locked:          lock.File{Version: lock.CurrentVersion},
		SelectedTargets: statusClaudeSelection(t, string(target.TargetPi)),
		Observations:    observations,
		CurrentOwner:    claim.Owner(),
		AllClaims:       []durablecarrier.ManagedCarrierClaim{claim},
		ResolveRoute:    ResolveCurrentCarrierRemovalRoute,
	})
	if err != nil {
		t.Fatalf("BuildCarrierAbsenceActions returned error: %v", err)
	}
	if len(actions) != 1 ||
		actions[0].Decision() != carrierabsence.DecisionRemove ||
		!actions[0].InvokesHostRoute() {
		t.Fatalf("actions = %#v, want one Pi remove action", actions)
	}
}

func currentCarrierClaim(
	t *testing.T,
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	scope target.Scope,
	sourceKind desiredextension.SourceKind,
	sourceRef string,
) durablecarrier.ManagedCarrierClaim {
	t.Helper()
	value := desiredtest.Extension(t, desiredextension.Spec{
		Name:    "managed-package",
		Carrier: carrier,
		Target:  selectedTarget,
		Scope:   scope,
		Source:  desiredtest.ExtensionSource(t, sourceKind, sourceRef),
	})
	locked, _ := snapshottest.ExtensionCarrierFile(t, value)
	return retainedCarrierFixtureFor(t, locked).claim
}
