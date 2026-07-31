package clipresent

import (
	"path/filepath"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
)

func claudePluginCarrierAction(t *testing.T, inventorySpec observeclaudeplugin.InventorySpec) reconcile.RelationAction {
	t.Helper()
	record, subject := claudePluginCarrierFixture(t)
	return claudePluginCarrierActionForSubject(t, record, subject, inventorySpec)
}

func claudePluginCarrierActionForSubject(
	t *testing.T,
	record lock.LockedSubjectContract,
	subject realization.DelegatedRelation,
	inventorySpec observeclaudeplugin.InventorySpec,
) reconcile.RelationAction {
	t.Helper()
	return claudePluginCarrierActionForSubjectWithSelectedOutcome(
		t,
		record,
		subject,
		inventorySpec,
		reconcile.AdmissionOutcomeBlocked,
	)
}

func claudePluginCarrierActionWithSelectedOutcome(
	t *testing.T,
	inventorySpec observeclaudeplugin.InventorySpec,
	selectedOutcome reconcile.RelationAdmissionOutcome,
) reconcile.RelationAction {
	t.Helper()
	record, subject := claudePluginCarrierFixture(t)
	return claudePluginCarrierActionForSubjectWithSelectedOutcome(t, record, subject, inventorySpec, selectedOutcome)
}

func claudePluginCarrierActionForSubjectWithSelectedOutcome(
	t *testing.T,
	record lock.LockedSubjectContract,
	subject realization.DelegatedRelation,
	inventorySpec observeclaudeplugin.InventorySpec,
	selectedOutcome reconcile.RelationAdmissionOutcome,
) reconcile.RelationAction {
	t.Helper()
	return claudePluginCarrierActionForSubjectWithClaimPresence(
		t,
		record,
		subject,
		inventorySpec,
		selectedOutcome,
		true,
	)
}

func claudePluginCarrierActionForSubjectWithClaimPresence(
	t *testing.T,
	record lock.LockedSubjectContract,
	subject realization.DelegatedRelation,
	inventorySpec observeclaudeplugin.InventorySpec,
	selectedOutcome reconcile.RelationAdmissionOutcome,
	managedClaimPresent bool,
) reconcile.RelationAction {
	t.Helper()
	inventory, err := observeclaudeplugin.NewInventory(inventorySpec)
	if err != nil {
		t.Fatalf("NewInventory returned error: %v", err)
	}
	realization, ok := record.Realization()
	if !ok {
		t.Fatal("locked subject contract does not carry a realization")
	}
	relation, ok := realization.DelegatedRelation()
	if !ok {
		t.Fatal("locked subject contract does not carry a delegated relation")
	}
	admission, err := reconcile.NewRelationRouteAdmissionDecision(reconcile.RelationRouteAdmissionSpec{
		Row:               reconcile.RouteAdmissionRowInstallCarrier,
		RequestedOutcome:  reconcile.AdmissionOutcomeOrdinaryMutation,
		SelectedOutcome:   selectedOutcome,
		ObservationPolicy: reconcile.ObservationRequireCurrent,
	})
	if err != nil {
		t.Fatalf("NewRelationRouteAdmissionDecision returned error: %v", err)
	}
	identity := presentManagedCarrierIdentity(t, record)
	action, err := reconcile.NewRelationAction(reconcile.RelationActionInput{
		CarrierIdentity:     identity,
		RouteRequest:        relation.RouteRequest(),
		Correlation:         observeclaudeplugin.Correlate(subject, inventory),
		RouteAdmission:      admission,
		ManagedClaimPresent: managedClaimPresent,
	})
	if err != nil {
		t.Fatalf("NewRelationAction returned error: %v", err)
	}
	return action
}

func presentManagedCarrierIdentity(
	t *testing.T,
	record lock.LockedSubjectContract,
) durablecarrier.ManagedCarrierIdentity {
	t.Helper()
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(record)
	if err != nil || !admitted {
		t.Fatalf("ManagedCarrierIdentityFromLockedRecord = (%#v, %t, %v)", identity, admitted, err)
	}
	return identity
}

func claudePluginManagedRow(t *testing.T, subjectKey string, managedKey string) observeclaudeplugin.Row {
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

func claudePluginCarrierFixture(
	t *testing.T,
) (lock.LockedSubjectContract, realization.DelegatedRelation) {
	t.Helper()
	value := desiredtest.Extension(t, desiredextension.Spec{
		Name:    "context7",
		Carrier: desiredextension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   target.ScopeProject,
		Source:  desiredtest.ExtensionSource(t, desiredextension.SourceKindMarketplace, "context7@market"),
	})
	file, relation := snapshottest.ExtensionCarrierFile(t, value)
	return file.Locked.Subjects()[0], relation
}

func presentCarrierAbsenceAction(t *testing.T) carrierabsence.Action {
	t.Helper()
	record, relation := claudePluginCarrierFixture(t)
	identity := presentManagedCarrierIdentity(t, record)
	root := t.TempDir()
	owner, err := stateauthority.New(pathtest.Exact(
		filepath.Join(root, ".daem", "state.json"),
	),

		filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatalf("stateauthority.New: %v", err)
	}
	install, err := lock.DelegatedOperationRequest(record, lock.OperationInstall)
	if err != nil {
		t.Fatalf("DelegatedOperationRequest: %v", err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		install,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatalf("NewManagedCarrierClaim: %v", err)
	}
	occupancy, err := durablecarrier.NewCarrierOccupancy(identity.Carrier(), []durablecarrier.ManagedCarrierClaim{claim})
	if err != nil {
		t.Fatalf("NewCarrierOccupancy: %v", err)
	}
	expected := relation.ExpectedRelation()
	inventory, err := observeclaudeplugin.NewInventory(observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			claudePluginManagedRow(t, string(expected.SubjectKey()), string(expected.ManagedInstanceKey())),
		},
	})
	if err != nil {
		t.Fatalf("NewInventory: %v", err)
	}
	key, err := observerelation.NewCorrelationKey(identity.RelationSubject(), expected)
	if err != nil {
		t.Fatalf("NewCorrelationKey: %v", err)
	}
	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim:   claim,
		Desired: carrierabsence.DesiredAbsent,
		Observation: observerelation.Correlation{
			Key:    key,
			Result: observeclaudeplugin.Correlate(relation, inventory),
		},
		Occupancy: occupancy,
		Route:     carrierabsence.UnavailableRoute(),
	})
	if err != nil {
		t.Fatalf("NewAction: %v", err)
	}
	return action
}
