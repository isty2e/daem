package apply

import (
	"context"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/realization/relation"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func applyClaudePluginCarrierLockfileForScope(t *testing.T, scope target.Scope) lock.File {
	t.Helper()
	file, _ := applyExtensionDerivedClaudePluginCarrierLockfileForScope(t, scope)
	return file
}

func applyExtensionDerivedClaudePluginCarrierAction(
	t *testing.T,
	inventorySpec observeclaudeplugin.InventorySpec,
) reconciliation.RelationAction {
	t.Helper()
	file, subject := applyExtensionDerivedClaudePluginCarrierLockfile(t)
	return applyClaudePluginCarrierRelationAction(t, file.Locked.Subjects()[0], subject, inventorySpec)
}

func applyExtensionDerivedClaudePluginCarrierLockfile(
	t *testing.T,
) (lock.File, realization.DelegatedRelation) {
	t.Helper()
	return applyExtensionDerivedClaudePluginCarrierLockfileForScope(t, target.ScopeProject)
}

func applyExtensionDerivedClaudePluginCarrierLockfileForScope(
	t *testing.T,
	scope target.Scope,
) (lock.File, realization.DelegatedRelation) {
	t.Helper()
	environment := applyCarrierEnvironment(
		t,
		"context7",
		extension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode,
		scope,
		extension.SourceKindMarketplace,
		"context7@market",
	)
	file, err := lockbuild.BuildWithOptions(context.Background(), environment, nil, lockbuild.Options{})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one extension-derived carrier", file.Locked.Subjects())
	}
	return file, snapshottest.DelegatedRelation(t, file.Locked.Subjects()[0])
}

func applyExtensionDerivedCodexPluginCarrierLockfile(
	t *testing.T,
) (lock.File, realization.DelegatedRelation) {
	t.Helper()
	environment := applyCarrierEnvironment(
		t,
		"documents-managed",
		extension.CarrierCodexPlugin,
		target.TargetCodex,
		target.ScopeGlobal,
		extension.SourceKindMarketplace,
		"documents@openai-primary-runtime",
	)
	file, err := lockbuild.BuildWithOptions(context.Background(), environment, nil, lockbuild.Options{})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one extension-derived carrier", file.Locked.Subjects())
	}
	return file, snapshottest.DelegatedRelation(t, file.Locked.Subjects()[0])
}

func applyExtensionDerivedOpenCodePluginCarrierLockfile(
	t *testing.T,
) (lock.File, realization.DelegatedRelation) {
	t.Helper()
	return applyExtensionDerivedOpenCodePluginCarrierLockfileForScope(t, target.ScopeGlobal)
}

func applyExtensionDerivedOpenCodePluginCarrierLockfileForScope(
	t *testing.T,
	scope target.Scope,
) (lock.File, realization.DelegatedRelation) {
	t.Helper()
	environment := applyCarrierEnvironment(
		t,
		"formatter-managed",
		extension.CarrierOpenCodePlugin,
		target.TargetOpenCode,
		scope,
		extension.SourceKindHostSource,
		"@acme/opencode-formatter",
	)
	file, err := lockbuild.BuildWithOptions(context.Background(), environment, nil, lockbuild.Options{})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one extension-derived carrier", file.Locked.Subjects())
	}
	return file, snapshottest.DelegatedRelation(t, file.Locked.Subjects()[0])
}

func applyExtensionDerivedPiPackageCarrierLockfile(
	t *testing.T,
) (lock.File, realization.DelegatedRelation) {
	t.Helper()
	return applyExtensionDerivedPiPackageCarrierLockfileForScope(t, target.ScopeProject)
}

func applyExtensionDerivedPiPackageCarrierLockfileForScope(
	t *testing.T,
	scope target.Scope,
) (lock.File, realization.DelegatedRelation) {
	t.Helper()
	environment := applyCarrierEnvironment(
		t,
		"tools-managed",
		extension.CarrierPiPackage,
		target.TargetPi,
		scope,
		extension.SourceKindHostSource,
		"github:acme/pi-tools",
	)
	file, err := lockbuild.BuildWithOptions(context.Background(), environment, nil, lockbuild.Options{})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one extension-derived carrier", file.Locked.Subjects())
	}
	return file, snapshottest.DelegatedRelation(t, file.Locked.Subjects()[0])
}

func applyExtensionDerivedAntigravityCLIPluginCarrierLockfile(
	t *testing.T,
) (lock.File, realization.DelegatedRelation) {
	t.Helper()
	environment := applyCarrierEnvironment(
		t,
		"guidance-managed",
		extension.CarrierAntigravityCLIPlugin,
		target.TargetAntigravityCLI,
		target.ScopeGlobal,
		extension.SourceKindHostSource,
		"modern-web-guidance@google",
	)
	file, err := lockbuild.BuildWithOptions(context.Background(), environment, nil, lockbuild.Options{})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if len(file.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one extension-derived carrier", file.Locked.Subjects())
	}
	return file, snapshottest.DelegatedRelation(t, file.Locked.Subjects()[0])
}

func applyCarrierEnvironment(
	t *testing.T,
	name string,
	carrier extension.Carrier,
	selected target.Target,
	scope target.Scope,
	sourceKind extension.SourceKind,
	ref string,
) desired.Environment {
	t.Helper()
	value := desiredtest.Extension(t, extension.Spec{
		Name:    name,
		Carrier: carrier,
		Target:  selected,
		Scope:   scope,
		Source:  desiredtest.ExtensionSource(t, sourceKind, ref),
	})
	return desiredtest.Environment(t, desired.Spec{
		Targets:    []target.Target{selected},
		Defaults:   desiredtest.Defaults(t, target.ScopeProject, skill.InstallModeCopy),
		Extensions: []extension.Extension{value},
	})
}

func applyClaudePluginCarrierActionForSubject(
	t *testing.T,
	record lock.LockedSubjectContract,
	subject realization.DelegatedRelation,
	inventorySpec observeclaudeplugin.InventorySpec,
) reconciliation.RelationAction {
	t.Helper()
	return applyClaudePluginCarrierRelationAction(t, record, subject, inventorySpec)
}

func applyClaudePluginCarrierRelationAction(
	t *testing.T,
	record lock.LockedSubjectContract,
	subject realization.DelegatedRelation,
	inventorySpec observeclaudeplugin.InventorySpec,
) reconciliation.RelationAction {
	t.Helper()
	inventory, err := observeclaudeplugin.NewInventory(inventorySpec)
	if err != nil {
		t.Fatalf("NewInventory returned error: %v", err)
	}
	realization, ok := record.Realization()
	if !ok {
		t.Fatal("test fixture missing delegated relation realization")
	}
	relation, ok := realization.DelegatedRelation()
	if !ok {
		t.Fatal("test fixture realization is not a delegated relation")
	}
	admission, err := applyClaudePluginCarrierAdmission()
	if err != nil {
		t.Fatalf("applyClaudePluginCarrierAdmission returned error: %v", err)
	}
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(record)
	if err != nil || !admitted {
		t.Fatalf("ManagedCarrierIdentityFromLockedRecord = (%#v, %t, %v)", identity, admitted, err)
	}
	action, err := reconciliation.NewRelationAction(reconciliation.RelationActionInput{
		CarrierIdentity:     identity,
		RouteRequest:        relation.RouteRequest(),
		Correlation:         observeclaudeplugin.Correlate(subject, inventory),
		RouteAdmission:      admission,
		ManagedClaimPresent: true,
	})
	if err != nil {
		t.Fatalf("relation.Plan returned error: %v", err)
	}
	return action
}

func applyClaudePluginCarrierAdmission() (reconciliation.RelationRouteAdmissionDecision, error) {
	return reconciliation.NewRelationRouteAdmissionDecision(reconciliation.RelationRouteAdmissionSpec{
		Row:               reconciliation.RouteAdmissionRowInstallCarrier,
		RequestedOutcome:  reconciliation.AdmissionOutcomeOrdinaryMutation,
		SelectedOutcome:   reconciliation.AdmissionOutcomeHostDelegated,
		ObservationPolicy: reconciliation.ObservationRequireCurrent,
	})
}

func applyRelationInventory(
	t *testing.T,
	spec observerelation.InventorySpec,
) observerelation.Inventory {
	t.Helper()
	inventory, err := observerelation.NewInventory(spec)
	if err != nil {
		t.Fatalf("NewInventory returned error: %v", err)
	}
	return inventory
}

func applyClaudePluginCarrierInventory(
	t *testing.T,
	spec observeclaudeplugin.InventorySpec,
) observeclaudeplugin.Inventory {
	t.Helper()
	inventory, err := observeclaudeplugin.NewInventory(spec)
	if err != nil {
		t.Fatalf("NewInventory returned error: %v", err)
	}
	return inventory
}

func applyClaudeObservationBatch(
	t *testing.T,
	locked lock.File,
	subject realization.DelegatedRelation,
	inventory observeclaudeplugin.Inventory,
) observerelation.Batch {
	t.Helper()
	if len(locked.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one delegated relation", locked.Locked.Subjects())
	}
	return applyObservationBatch(
		t,
		locked.Locked.Subjects()[0].SubjectID(),
		subject,
		observeclaudeplugin.Correlate(subject, inventory),
	)
}

func applyClaudeObservationBatchForLocked(
	t *testing.T,
	locked lock.File,
	inventory observeclaudeplugin.Inventory,
) observerelation.Batch {
	t.Helper()
	correlations := make([]observerelation.Correlation, 0)
	for _, contract := range locked.Locked.Subjects() {
		_, ok, err := lock.DelegatedRelationCarrier(contract)
		if err != nil {
			t.Fatalf("DelegatedRelationCarrier returned error: %v", err)
		}
		if !ok {
			continue
		}
		subject := snapshottest.DelegatedRelation(t, contract)
		key := applyObservationCorrelationKey(t, contract.SubjectID(), subject)
		correlations = append(correlations, observerelation.Correlation{
			Key:    key,
			Result: observeclaudeplugin.Correlate(subject, inventory),
		})
	}
	batch, err := observerelation.NewBatch(observerelation.BatchSpec{Correlations: correlations})
	if err != nil {
		t.Fatalf("observerelation.NewBatch returned error: %v", err)
	}
	return batch
}

func applyRelationObservationBatch(
	t *testing.T,
	locked lock.File,
	subject realization.DelegatedRelation,
	inventory observerelation.Inventory,
) observerelation.Batch {
	t.Helper()
	if len(locked.Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one delegated relation", locked.Locked.Subjects())
	}
	return applyObservationBatch(
		t,
		locked.Locked.Subjects()[0].SubjectID(),
		subject,
		observerelation.Correlate(subject.ExpectedRelation(), inventory),
	)
}

func applyObservationBatch(
	t *testing.T,
	subject topology.SubjectID,
	relation realization.DelegatedRelation,
	correlation observerelation.CorrelationResult,
) observerelation.Batch {
	t.Helper()
	key := applyObservationCorrelationKey(t, subject, relation)
	batch, err := observerelation.NewBatch(observerelation.BatchSpec{
		Correlations: []observerelation.Correlation{{
			Key:    key,
			Result: correlation,
		}},
	})
	if err != nil {
		t.Fatalf("observerelation.NewBatch returned error: %v", err)
	}
	return batch
}

func applyObservationCorrelationKey(
	t *testing.T,
	subject topology.SubjectID,
	relation realization.DelegatedRelation,
) observerelation.CorrelationKey {
	t.Helper()
	key, err := observerelation.NewCorrelationKey(
		subject,
		relation.ExpectedRelation(),
	)
	if err != nil {
		t.Fatalf("observerelation.NewCorrelationKey returned error: %v", err)
	}
	return key
}

func applyRelationManagedRow(t *testing.T, subjectKey string, managedKey string) observerelation.Row {
	t.Helper()
	row, err := observerelation.NewRow(observerelation.RowSpec{
		SubjectKey:            subjectKey,
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    managedKey,
	})
	if err != nil {
		t.Fatalf("NewRow returned error: %v", err)
	}
	return row
}

func applyRelationUnmanagedRow(t *testing.T, subjectKey string) observerelation.Row {
	t.Helper()
	row, err := observerelation.NewRow(observerelation.RowSpec{SubjectKey: subjectKey})
	if err != nil {
		t.Fatalf("NewRow returned error: %v", err)
	}
	return row
}

func applyClaudePluginCarrierManagedRow(t *testing.T, subjectKey string, managedKey string) observeclaudeplugin.Row {
	t.Helper()
	return applyClaudePluginCarrierManagedRowWithScope(t, subjectKey, managedKey, observeclaudeplugin.HostScopeProject)
}

func applyClaudePluginCarrierManagedRowWithScope(
	t *testing.T,
	subjectKey string,
	managedKey string,
	scope observeclaudeplugin.HostScope,
) observeclaudeplugin.Row {
	t.Helper()
	row, err := observeclaudeplugin.NewRow(observeclaudeplugin.RowSpec{
		SubjectKey:            subjectKey,
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    managedKey,
		Scope:                 scope,
	})
	if err != nil {
		t.Fatalf("NewRow returned error: %v", err)
	}
	return row
}

func applyClaudePluginCarrierUnmanagedRow(t *testing.T, subjectKey string) observeclaudeplugin.Row {
	t.Helper()
	return applyClaudePluginCarrierUnmanagedRowWithScope(t, subjectKey, observeclaudeplugin.HostScopeProject)
}

func applyClaudePluginCarrierUnmanagedRowWithScope(
	t *testing.T,
	subjectKey string,
	scope observeclaudeplugin.HostScope,
) observeclaudeplugin.Row {
	t.Helper()
	row, err := observeclaudeplugin.NewRow(observeclaudeplugin.RowSpec{
		SubjectKey: subjectKey,
		Scope:      scope,
	})
	if err != nil {
		t.Fatalf("NewRow returned error: %v", err)
	}
	return row
}

func applyClaudePluginCarrierSubject(t *testing.T) realization.DelegatedRelation {
	t.Helper()
	return applyClaudePluginCarrierSubjectWithDeclarationID(t, target.ScopeProject, "context7")
}

func applyClaudePluginCarrierSubjectWithDeclarationID(
	t *testing.T,
	scope target.Scope,
	declarationID string,
) realization.DelegatedRelation {
	t.Helper()
	_, relation := applyClaudePluginCarrierContractWithDeclarationID(t, scope, declarationID)
	return relation
}

func applyClaudePluginCarrierContractWithDeclarationID(
	t *testing.T,
	scope target.Scope,
	declarationID string,
) (lock.LockedSubjectContract, realization.DelegatedRelation) {
	t.Helper()
	source, err := extension.NewSourceRef(extension.SourceKindMarketplace, "context7@market")
	if err != nil {
		t.Fatalf("NewSourceRef returned error: %v", err)
	}
	carrier, err := extension.NewCarrierKey(
		extension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode,
		scope,
		source,
	)
	if err != nil {
		t.Fatalf("NewCarrierKey returned error: %v", err)
	}
	subjectID, err := topology.NewSubjectID(topology.SubjectHostRelation, "claude-code.plugin-carrier", declarationID)
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	subjectKey, err := hostrelation.NewSubjectKey("context7@market")
	if err != nil {
		t.Fatalf("NewSubjectKey returned error: %v", err)
	}
	entityID, err := entity.New(entity.KindExtension, declarationID)
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	record, err := lock.NewDelegatedRelationCarrierContract(entityID, carrier, subjectID, subjectKey)
	if err != nil {
		t.Fatalf("NewDelegatedRelationCarrierContract returned error: %v", err)
	}
	return record, snapshottest.DelegatedRelation(t, record)
}
