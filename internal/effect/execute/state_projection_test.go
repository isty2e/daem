package execute

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/topology"
)

func TestPromotedProjectCarrierClaimsSelectOnlyExactPendingNoOp(t *testing.T) {
	action := testHostRelationAction(t)
	owner := testCarrierStateAuthority(t)
	pending, err := durablecarrier.NewPendingCarrierInstall(
		owner,
		action.CarrierIdentity(),
		action.RouteRequest(),
	)
	if err != nil {
		t.Fatal(err)
	}
	current := testStateProjectionSnapshot(t, durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending},
	})

	claims, err := promotedProjectCarrierClaims(
		current,
		[]reconciliation.RelationAction{action},
	)
	if err != nil {
		t.Fatalf("promotedProjectCarrierClaims returned error: %v", err)
	}
	if len(claims) != 1 ||
		!claims[0].Owner().ExactEqual(owner) ||
		!claims[0].Identity().ExactEqual(action.CarrierIdentity()) ||
		!claims[0].InstallRequest().Equal(action.RouteRequest()) {
		t.Fatalf("promoted claims = %#v, want exact pending acquisition", claims)
	}
	next, changed, err := current.WithPromotedCarrierClaims(claims)
	if err != nil || !changed ||
		len(next.PendingCarrierInstalls()) != 0 ||
		len(next.ManagedCarrierClaims()) != 1 {
		t.Fatalf("promote exact pending = (%#v, %t, %v)", next, changed, err)
	}
}

func TestPromotedProjectCarrierClaimsIgnoreRouteMismatch(t *testing.T) {
	action := testHostRelationAction(t)
	owner := testCarrierStateAuthority(t)
	staleRequest, err := realizationdelegate.NewRequest(
		action.RouteRequest().RouteID(),
		action.RouteRequest().ContractVersion(),
		"sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := durablecarrier.NewPendingCarrierInstall(
		owner,
		action.CarrierIdentity(),
		staleRequest,
	)
	if err != nil {
		t.Fatal(err)
	}
	current := testStateProjectionSnapshot(t, durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{stale},
	})

	claims, err := promotedProjectCarrierClaims(
		current,
		[]reconciliation.RelationAction{action},
	)
	if err != nil {
		t.Fatalf("promotedProjectCarrierClaims returned error: %v", err)
	}
	if len(claims) != 0 {
		t.Fatalf("route-mismatched pending produced claims: %#v", claims)
	}
	if got := current.PendingCarrierInstalls(); len(got) != 1 ||
		!got[0].ExactEqual(stale) {
		t.Fatalf("route-mismatched pending changed: %#v", got)
	}
}

func testCarrierStateAuthority(t *testing.T) durablecarrier.StateAuthority {
	t.Helper()
	root := t.TempDir()
	owner, err := durablecarrier.NewStateAuthority(
		filepath.Join(root, "state.json"),
		filepath.Join(root, "daem.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func testHostRelationAction(t *testing.T) reconciliation.RelationAction {
	t.Helper()
	subjectKey, err := hostrelation.NewSubjectKey("context7@market")
	if err != nil {
		t.Fatalf("NewSubjectKey: %v", err)
	}
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"claude-code.plugin-carrier",
		"context7-managed",
	)
	if err != nil {
		t.Fatalf("HostRelationSubjectID: %v", err)
	}
	identity, relation := testManagedCarrierIdentity(t, subject, subjectKey)
	row, err := observerelation.NewRow(observerelation.RowSpec{
		SubjectKey:            string(subjectKey),
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    string(relation.ManagedInstanceKey()),
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
	admission, err := reconciliation.NewRelationRouteAdmissionDecision(reconciliation.RelationRouteAdmissionSpec{
		Row:               reconciliation.RouteAdmissionRowInstallCarrier,
		RequestedOutcome:  reconciliation.AdmissionOutcomeOrdinaryMutation,
		SelectedOutcome:   reconciliation.AdmissionOutcomeHostDelegated,
		ObservationPolicy: reconciliation.ObservationRequireCurrent,
	})
	if err != nil {
		t.Fatalf("NewRouteAdmissionDecision: %v", err)
	}
	routeRequest, err := realizationdelegate.NewRequest(
		"claude-code.plugin-carrier.install",
		"claude-plugin-carrier-v1",
		"sha256:0000000000000000000000000000000000000000000000000000000000000000",
	)
	if err != nil {
		t.Fatalf("NewDelegatedRouteRequest: %v", err)
	}
	action, err := reconciliation.NewRelationAction(reconciliation.RelationActionInput{
		CarrierIdentity:       identity,
		RouteRequest:          routeRequest,
		Correlation:           observerelation.Correlate(relation, inventory),
		RouteAdmission:        admission,
		PendingInstallPresent: true,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return action
}

func testStateProjectionSnapshot(
	t *testing.T,
	input durable.SnapshotInput,
) durable.Snapshot {
	t.Helper()
	snapshot, err := durable.NewSnapshot(input)
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	return snapshot
}
