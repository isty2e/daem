package carrieradoption

import (
	"path/filepath"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
)

func TestActionValidateRejectsForgedDerivedState(t *testing.T) {
	canonical := invariantTestAction(t)
	tests := []struct {
		name   string
		mutate func(*Action)
	}{
		{
			name: "identity",
			mutate: func(action *Action) {
				action.identity = durablecarrier.ManagedCarrierIdentity{}
			},
		},
		{
			name: "acquisition request",
			mutate: func(action *Action) {
				action.acquisition = realizationdelegate.Request{}
			},
		},
		{
			name: "occupancy",
			mutate: func(action *Action) {
				action.occupancy = durablecarrier.CarrierOccupancy{}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			forged := canonical
			test.mutate(&forged)
			if err := forged.Validate(); err == nil {
				t.Fatal("Validate accepted forged derived state")
			}
		})
	}
}

func invariantTestAction(t *testing.T) Action {
	t.Helper()
	desired := desiredtest.Extension(t, desiredextension.Spec{
		Name:    "alpha",
		Carrier: desiredextension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   target.ScopeProject,
		Source: desiredtest.ExtensionSource(
			t,
			desiredextension.SourceKindMarketplace,
			"alpha@official",
		),
	})
	file, relation := snapshottest.ExtensionCarrierFile(t, desired)
	locked := file.Locked.Subjects()[0]
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(locked)
	if err != nil || !admitted {
		t.Fatalf("ManagedCarrierIdentityFromLockedRecord = (%#v, %t, %v)", identity, admitted, err)
	}
	installRequest, err := lock.DelegatedOperationRequest(locked, lock.OperationInstall)
	if err != nil {
		t.Fatalf("DelegatedOperationRequest: %v", err)
	}
	root := t.TempDir()
	owner, err := stateauthority.New(
		filepath.Join(root, ".daem", "state.json"),
		filepath.Join(root, "daem.toml"),
	)
	if err != nil {
		t.Fatalf("stateauthority.New: %v", err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		installRequest,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatalf("NewManagedCarrierClaim: %v", err)
	}
	resolvedRemoval, admitted, err := lock.ResolveDelegatedCarrierRemoval(
		identity.Carrier().Key(),
		identity.RelationSubject(),
		identity.ExpectedRelation(),
		installRequest,
	)
	if err != nil || !admitted {
		t.Fatalf("ResolveDelegatedCarrierRemoval admitted = %t, err = %v", admitted, err)
	}
	removal, err := carrierabsence.NewRouteAdmission(carrierabsence.RouteAdmissionInput{
		Operation:              resolvedRemoval.Operation(),
		Request:                resolvedRemoval.Request(),
		PreservesSharedCarrier: resolvedRemoval.PreservesSharedCarrier(),
		RemovedEffects:         resolvedRemoval.RemovedEffects(),
		RetainedEffects:        resolvedRemoval.RetainedEffects(),
		NonClaims:              resolvedRemoval.NonClaims(),
	})
	if err != nil {
		t.Fatalf("NewRouteAdmission: %v", err)
	}
	lifecycle, err := NewLifecycle(LifecycleInput{
		Locked:         locked,
		InstallRoute:   InstallRouteAdmitted,
		RemovalRoute:   removal,
		ClaimStore:     ClaimStoreProjectStatefile,
		StoreAvailable: true,
	})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	expected := relation.ExpectedRelation()
	row, err := observerelation.NewRow(observerelation.RowSpec{
		SubjectKey:            string(expected.SubjectKey()),
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    string(expected.ManagedInstanceKey()),
	})
	if err != nil {
		t.Fatalf("NewRow: %v", err)
	}
	inventory, err := observerelation.NewInventory(observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows:         []observerelation.Row{row},
	})
	if err != nil {
		t.Fatalf("NewInventory: %v", err)
	}
	action, err := NewAction(ActionInput{
		Locked:       locked,
		Observation:  observerelation.Correlate(expected, inventory),
		CurrentOwner: owner,
		Claims:       []durablecarrier.ManagedCarrierClaim{claim},
		Lifecycle:    lifecycle,
	})
	if err != nil {
		t.Fatalf("NewAction: %v", err)
	}
	return action
}
