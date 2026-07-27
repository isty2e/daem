package cli

import (
	"path/filepath"
	"testing"

	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/reconcile"
	reconcilehostroute "github.com/isty2e/daem/internal/reconcile/build/hostroute"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
	"github.com/isty2e/daem/internal/target"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	statusworkflow "github.com/isty2e/daem/internal/workflow/status"
)

func TestStatusCheckExitCodeFailsForBlockedCarrierRelationActionsWithClaudeFixture(t *testing.T) {
	locked, subject := cliClaudePluginCarrierFixture(t)
	tests := []struct {
		name string
		spec observeclaudeplugin.InventorySpec
	}{
		{
			name: "stale exact-looking relation",
			spec: observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceStale,
				Rows: []observeclaudeplugin.Row{
					cliClaudePluginCarrierManagedRow(t, "context7@market", string(subject.ExpectedRelation().ManagedInstanceKey())),
				},
			},
		},
		{
			name: "unmanaged same-name relation",
			spec: observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows: []observeclaudeplugin.Row{
					cliClaudePluginCarrierUnmanagedRow(t, "context7@market"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cliClaudePluginCarrierCommandResultForSubject(t, locked, subject, tt.spec)
			if got := statusCheckExitCode(result, true); got != 1 {
				t.Fatalf("statusCheckExitCode = %d, want 1", got)
			}
		})
	}
}

func TestStatusCheckExitCodeAllowsAdmittedMissingCarrierRelationWithClaudeFixture(t *testing.T) {
	result := cliClaudePluginCarrierCommandResult(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	if got := statusCheckExitCode(result, true); got != 0 {
		t.Fatalf("statusCheckExitCode = %d, want 0 for admitted host-delegated missing relation", got)
	}
	relations := result.Reconciliation.Relations()
	if len(relations) != 1 || !relations[0].InvokesHostRoute() {
		t.Fatalf("relation actions = %#v, want one host-route action", relations)
	}
}

func TestAdmittedHostRouteRequiresNormalApplyConfirmation(t *testing.T) {
	statusResult := cliClaudePluginCarrierCommandResult(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	planning := applyworkflow.CommandResult{Reconciliation: statusResult.Reconciliation}
	if !applyConfirmationRequired(planning) {
		t.Fatal("admitted host route did not require normal apply confirmation")
	}
}

func TestStateOnlyCarrierAdoptionRequiresNormalApplyConfirmation(t *testing.T) {
	action := cliClaudePluginCarrierAdoptionAction(t, true, true, nil)
	reconciliation, err := reconcile.NewResult(reconcile.ResultInput{
		Context:          reconcile.ContextApply,
		CarrierAdoptions: []carrieradoption.Action{action},
	})
	if err != nil {
		t.Fatalf("reconcile.NewResult: %v", err)
	}
	if !applyConfirmationRequired(applyworkflow.CommandResult{Reconciliation: reconciliation}) {
		t.Fatal("state-only carrier claim write did not require normal apply confirmation")
	}
}

func TestStatusCheckExitCodeFailsForCarrierAdoptionClaimConflict(t *testing.T) {
	locked, _ := cliClaudePluginCarrierFixture(t)
	contract := locked.Locked.Subjects()[0]
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contract)
	if err != nil || !admitted {
		t.Fatalf("ManagedCarrierIdentityFromLockedRecord = (%#v, %t, %v)", identity, admitted, err)
	}
	request, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
	if err != nil {
		t.Fatalf("DelegatedOperationRequest: %v", err)
	}
	otherRoot := t.TempDir()
	otherOwner, err := stateauthority.New(
		filepath.Join(otherRoot, ".daem", "state.json"),
		filepath.Join(otherRoot, "other.toml"),
	)
	if err != nil {
		t.Fatalf("stateauthority.New: %v", err)
	}
	conflict, err := durablecarrier.NewManagedCarrierClaim(
		otherOwner,
		identity,
		request,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatalf("NewManagedCarrierClaim: %v", err)
	}
	action := cliClaudePluginCarrierAdoptionAction(
		t,
		true,
		true,
		[]durablecarrier.ManagedCarrierClaim{conflict},
	)
	reconciliation, err := reconcile.NewResult(reconcile.ResultInput{
		Context:          reconcile.ContextInspect,
		CarrierAdoptions: []carrieradoption.Action{action},
	})
	if err != nil {
		t.Fatalf("reconcile.NewResult: %v", err)
	}
	result := statusworkflow.CommandResult{Reconciliation: reconciliation}
	if got := statusCheckExitCode(result, true); got != 1 {
		t.Fatalf("statusCheckExitCode = %d, want 1 for adoption claim conflict", got)
	}
}

func TestStatusCheckExitCodeAllowsObserveOnlyCarrierRelationActionWithClaudeFixture(t *testing.T) {
	result := cliClaudePluginCarrierCommandResult(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventoryUnsupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	if got := statusCheckExitCode(result, true); got != 0 {
		t.Fatalf("statusCheckExitCode = %d, want 0", got)
	}
}

func cliClaudePluginCarrierAdoptionAction(
	t *testing.T,
	manageExisting bool,
	storeAvailable bool,
	claims []durablecarrier.ManagedCarrierClaim,
) carrieradoption.Action {
	t.Helper()
	locked, subject := cliClaudePluginCarrierFixture(t)
	contract := locked.Locked.Subjects()[0]
	expected := subject.ExpectedRelation()
	inventory, err := observeclaudeplugin.NewInventory(observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			cliClaudePluginCarrierManagedRow(
				t,
				string(expected.SubjectKey()),
				string(expected.ManagedInstanceKey()),
			),
		},
	})
	if err != nil {
		t.Fatalf("NewInventory: %v", err)
	}
	key, err := observerelation.NewCorrelationKey(contract.SubjectID(), expected)
	if err != nil {
		t.Fatalf("NewCorrelationKey: %v", err)
	}
	observations, err := observerelation.NewBatch(observerelation.BatchSpec{
		Correlations: []observerelation.Correlation{{
			Key:    key,
			Result: observeclaudeplugin.Correlate(subject, inventory),
		}},
	})
	if err != nil {
		t.Fatalf("NewBatch: %v", err)
	}
	selection, err := reconcile.NewSelectedTargets([]target.Target{target.TargetClaudeCode})
	if err != nil {
		t.Fatalf("NewSelectedTargets: %v", err)
	}
	root := t.TempDir()
	owner, err := stateauthority.New(
		filepath.Join(root, ".daem", "state.json"),
		filepath.Join(root, "daem.toml"),
	)
	if err != nil {
		t.Fatalf("stateauthority.New: %v", err)
	}
	actions, err := reconcilehostroute.BuildCarrierAdoptionActions(
		reconcilehostroute.CarrierAdoptionInput{
			Locked:          locked,
			SelectedTargets: selection,
			Observations:    observations,
			CurrentOwner:    owner,
			AllClaims:       claims,
			ManageExisting:  manageExisting,
			StoreAvailable:  storeAvailable,
		},
	)
	if err != nil {
		t.Fatalf("BuildCarrierAdoptionActions: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("carrier adoption actions = %#v, want one", actions)
	}
	return actions[0]
}

func cliClaudePluginCarrierCommandResult(t *testing.T, inventorySpec observeclaudeplugin.InventorySpec) statusworkflow.CommandResult {
	t.Helper()
	locked, subject := cliClaudePluginCarrierFixture(t)
	return cliClaudePluginCarrierCommandResultForSubject(t, locked, subject, inventorySpec)
}

func cliClaudePluginCarrierCommandResultForSubject(
	t *testing.T,
	locked lock.File,
	subject realization.DelegatedRelation,
	inventorySpec observeclaudeplugin.InventorySpec,
) statusworkflow.CommandResult {
	t.Helper()
	if len(locked.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one Claude plugin relation", locked.Locked.Subjects())
	}
	contract := locked.Locked.Subjects()[0]
	inventory, err := observeclaudeplugin.NewInventory(inventorySpec)
	if err != nil {
		t.Fatalf("NewInventory returned error: %v", err)
	}
	key, err := observerelation.NewCorrelationKey(
		contract.SubjectID(),
		subject.ExpectedRelation(),
	)
	if err != nil {
		t.Fatalf("observerelation.NewCorrelationKey returned error: %v", err)
	}
	selection, err := reconcile.NewSelectedTargets([]target.Target{target.TargetClaudeCode})
	if err != nil {
		t.Fatalf("NewSelectedTargets returned error: %v", err)
	}
	observations, err := observerelation.NewBatch(observerelation.BatchSpec{
		Correlations: []observerelation.Correlation{{
			Key:    key,
			Result: observeclaudeplugin.Correlate(subject, inventory),
		}},
	})
	if err != nil {
		t.Fatalf("observerelation.NewBatch returned error: %v", err)
	}
	root := t.TempDir()
	owner, err := stateauthority.New(
		filepath.Join(root, ".daem", "state.json"),
		filepath.Join(root, "daem.toml"),
	)
	if err != nil {
		t.Fatalf("stateauthority.New returned error: %v", err)
	}
	actions, err := reconcilehostroute.BuildRelationActions(reconcilehostroute.RelationInput{
		Locked:          locked,
		SelectedTargets: selection,
		Observations:    observations,
		CurrentOwner:    owner,
	})
	if err != nil {
		t.Fatalf("BuildRelationActions returned error: %v", err)
	}
	reconciliation, err := reconcile.NewResult(reconcile.ResultInput{
		Context:   reconcile.ContextInspect,
		Relations: actions,
	})
	if err != nil {
		t.Fatalf("NewResult returned error: %v", err)
	}
	return statusworkflow.CommandResult{Reconciliation: reconciliation}
}

func cliClaudePluginCarrierManagedRow(t *testing.T, subjectKey string, managedKey string) observeclaudeplugin.Row {
	t.Helper()
	row, err := observeclaudeplugin.NewRow(observeclaudeplugin.RowSpec{
		SubjectKey:            subjectKey,
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    managedKey,
		Scope:                 observeclaudeplugin.HostScopeProject,
	})
	if err != nil {
		t.Fatalf("NewRow returned error: %v", err)
	}
	return row
}

func cliClaudePluginCarrierUnmanagedRow(t *testing.T, subjectKey string) observeclaudeplugin.Row {
	t.Helper()
	row, err := observeclaudeplugin.NewRow(observeclaudeplugin.RowSpec{
		SubjectKey: subjectKey,
		Scope:      observeclaudeplugin.HostScopeProject,
	})
	if err != nil {
		t.Fatalf("NewRow returned error: %v", err)
	}
	return row
}

func cliClaudePluginCarrierFixture(t *testing.T) (lock.File, realization.DelegatedRelation) {
	t.Helper()
	value := desiredtest.Extension(t, desiredextension.Spec{
		Name:    "context7",
		Carrier: desiredextension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   target.ScopeProject,
		Source:  desiredtest.ExtensionSource(t, desiredextension.SourceKindMarketplace, "context7@market"),
	})
	return snapshottest.ExtensionCarrierFile(t, value)
}
