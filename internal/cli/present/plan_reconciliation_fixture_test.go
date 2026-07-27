package clipresent

import (
	"path/filepath"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	reconcilehostroute "github.com/isty2e/daem/internal/reconcile/build/hostroute"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func newPresentCarrierAdoptionFixture(t *testing.T) presentCarrierAdoptionFixture {
	t.Helper()
	contract, relation := claudePluginCarrierFixture(t)
	identity := presentManagedCarrierIdentity(t, contract)
	root := t.TempDir()
	owner, err := stateauthority.New(
		filepath.Join(root, ".daem", "state.json"),
		filepath.Join(root, "daem.toml"),
	)
	if err != nil {
		t.Fatalf("stateauthority.New: %v", err)
	}
	request, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
	if err != nil {
		t.Fatalf("DelegatedOperationRequest: %v", err)
	}
	candidate, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		request,
		durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved,
	)
	if err != nil {
		t.Fatalf("NewManagedCarrierClaim: %v", err)
	}
	removal, err := reconcilehostroute.ResolveCurrentCarrierRemovalRoute(candidate)
	if err != nil {
		t.Fatalf("ResolveCurrentCarrierRemovalRoute: %v", err)
	}
	lifecycle, err := carrieradoption.NewLifecycle(carrieradoption.LifecycleInput{
		Locked:         contract,
		InstallRoute:   carrieradoption.InstallRouteAdmitted,
		RemovalRoute:   removal,
		ClaimStore:     carrieradoption.ClaimStoreProjectStatefile,
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
	return presentCarrierAdoptionFixture{
		contract:  contract,
		owner:     owner,
		exact:     observerelation.Correlate(expected, inventory),
		lifecycle: lifecycle,
	}
}

func newManagedPathPlanFixture(
	t *testing.T,
	name string,
	installName string,
	scope target.Scope,
	consumers []target.Target,
	hashSeed string,
) managedPathPlanFixture {
	t.Helper()
	supply := snapshottest.ExactSupply(t, snapshottest.ExactSupplyInput{
		Kind:         entity.KindSkill,
		Name:         name,
		SourceID:     artifact.SourceID("local:skills/" + name + "?mode=vendor"),
		ArtifactKind: artifact.ArtifactKindDirectory,
		ContentHash:  artifact.HashFileContent([]byte(hashSeed)),
	})
	placements, err := profile.ManagedPathPlacementsFor(entity.KindSkill, scope, consumers)
	if err != nil || len(placements) != 1 {
		t.Fatalf("ManagedPathPlacementsFor = %#v, %v", placements, err)
	}
	destination, err := placements[0].ChildDestination(installName)
	if err != nil {
		t.Fatal(err)
	}
	writeRoute, err := profile.ManagedPathOperationRoute(placements[0], profile.OperationWrite)
	if err != nil {
		t.Fatal(err)
	}
	removeRoute, err := profile.ManagedPathOperationRoute(placements[0], profile.OperationRemove)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := placements[0].Realize(destination, realization.PathProjectionCopy, writeRoute)
	if err != nil {
		t.Fatal(err)
	}
	entityID, err := entity.New(entity.KindSkill, name)
	if err != nil {
		t.Fatal(err)
	}
	subjectID, err := topologyprojection.Subject(entityID, placements[0].ID())
	if err != nil {
		t.Fatal(err)
	}
	subject, err := lock.NewManagedPathSubjectContract(lock.ManagedPathSubjectInput{
		EntityID:      entityID,
		SubjectID:     subjectID,
		Realization:   spec,
		WriteRouteID:  writeRoute.RouteID(),
		RemoveRouteID: removeRoute.RouteID(),
	})
	if err != nil {
		t.Fatal(err)
	}
	locked := snapshottest.Section(t, supply, subject)
	admittedSupply, ok := locked.Subject(supply.SubjectID())
	if !ok {
		t.Fatalf("locked section omitted exact Supply %q", supply.SubjectID())
	}
	admittedSubject, ok := locked.Subject(subject.SubjectID())
	if !ok {
		t.Fatalf("locked section omitted managed path %q", subject.SubjectID())
	}
	expectation, err := reconcileprojection.NewManagedPathExpectation(admittedSubject)
	if err != nil {
		t.Fatalf("NewManagedPathExpectation returned error: %v", err)
	}
	pathProjection, ok := spec.ManagedPathProjection()
	if !ok {
		t.Fatal("fixture realization does not carry a managed path")
	}
	return managedPathPlanFixture{
		locked:      locked,
		supply:      admittedSupply,
		expectation: expectation,
		subject:     admittedSubject,
		destination: pathProjection.Destination(),
		scope:       pathProjection.Scope(),
		contentKind: pathProjection.ContentKind(),
		permissions: pathProjection.PermissionPolicy(),
	}
}

func mustReconciliationPlan(
	t testing.TB,
	managedPaths []reconcile.ManagedPathDecision,
	aggregates []reconcile.AggregateDecision,
) reconcile.Result {
	t.Helper()
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:      reconcile.ContextInspect,
		ManagedPaths: managedPaths,
		Aggregates:   aggregates,
	})
	if err != nil {
		t.Fatalf("NewResult returned error: %v", err)
	}
	return result
}

func reconciliationWithFamilies(
	t testing.TB,
	context reconcile.OperationContext,
	base reconcile.Result,
	relations []reconcile.RelationAction,
	delegates []reconcile.DelegateAction,
) reconcile.Result {
	t.Helper()
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:      context,
		ManagedPaths: base.ManagedPaths(),
		Aggregates:   base.Aggregates(),
		Relations:    relations,
		Delegates:    delegates,
	})
	if err != nil {
		t.Fatalf("NewResult returned error: %v", err)
	}
	return result
}

func reconciliationWithCarrierAbsences(
	t testing.TB,
	context reconcile.OperationContext,
	base reconcile.Result,
	absences []carrierabsence.Action,
) reconcile.Result {
	t.Helper()
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:         context,
		ManagedPaths:    base.ManagedPaths(),
		Aggregates:      base.Aggregates(),
		CarrierAbsences: absences,
	})
	if err != nil {
		t.Fatalf("NewResult returned error: %v", err)
	}
	return result
}
