package apply

import (
	"encoding/json"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
)

func legacyHandoffIdentity(t *testing.T, result reconcile.Result, retirements []carrierabsence.Action) mutation.OperationFingerprint {
	t.Helper()
	relations := relationFingerprintRows(result.Relations())
	for i := range relations {
		relations[i].Watchpoints = nil
	}
	orders := relationOrderFingerprintRows(result.RelationOrders())
	for i, order := range result.RelationOrders() {
		for _, action := range retirements {
			if order.Target() == action.Target() && order.Scope() == action.Scope() {
				orders[i].Revision = ""
			}
		}
	}
	payload, err := json.Marshal(struct {
		Managed    []managedPathFingerprintFacts
		Aggregates []aggregateFingerprintFacts
		Relations  []relationFingerprintFacts
		Absences   []carrierAbsenceFingerprintFacts
		Adoptions  []carrierAdoptionFingerprintFacts
		Orders     []relationOrderFingerprintFacts
		Delegates  []delegateFingerprintFacts
	}{
		managedPathFingerprintRows(result.ManagedPaths()), aggregateFingerprintRows(result.Aggregates()), relations,
		carrierAbsenceFingerprintRows(result.CarrierAbsences()), carrierAdoptionFingerprintRows(result.CarrierAdoptions()),
		orders, delegateFingerprintRows(result.Delegates()),
	})
	if err != nil {
		t.Fatal(err)
	}
	return mutation.NewOperationFingerprint(payload)
}

func checkHandoffOrderIdentity(t *testing.T, result reconcile.Result, actions []carrierabsence.Action) {
	t.Helper()
	a, b := applyOrderMember(t, "alpha", "npm:alpha"), applyOrderMember(t, "beta", "npm:beta")
	fingerprint := func(scope target.Scope, revision string, members ...hostrelation.RelationOrderMember) mutation.OperationFingerprint {
		decision := applyOrderDecisionInScope(t, scope, members, revision)
		withOrder, err := result.WithRelationOrders([]reconcile.RelationOrderDecision{decision})
		if err != nil {
			t.Fatal(err)
		}
		got, err := carrierHandoffRemainderFingerprint(withOrder, actions)
		if err != nil || !got.Equal(legacyHandoffIdentity(t, withOrder, actions)) {
			t.Fatal("handoff differs from legacy projection", err)
		}
		return got
	}

	before := fingerprint(target.ScopeGlobal, "sha256:before", a, b)
	if !before.Equal(fingerprint(target.ScopeGlobal, "sha256:after", a, b)) {
		t.Fatal("handoff included the retired inventory revision")
	}
	if before.Equal(fingerprint(target.ScopeGlobal, "sha256:before", b, a)) {
		t.Fatal("handoff ignored the desired order")
	}
	if fingerprint(target.ScopeProject, "sha256:before", a, b).Equal(fingerprint(target.ScopeProject, "sha256:after", a, b)) {
		t.Fatal("handoff ignored an unrelated inventory revision")
	}
}

func legacyFinalRouteIdentity(t *testing.T, routes []applyRouteScheduleFact, established, bound bool) mutation.OperationFingerprint {
	t.Helper()
	type row struct {
		Reference string                         `json:"reference"`
		Action    relationFingerprintFacts       `json:"action"`
		Work      operationplan.RouteWork        `json:"work"`
		Preflight applyRoutePreflightFingerprint `json:"preflight"`
	}
	rows := make([]row, 0, len(routes))
	for _, route := range routes {
		preflight, err := applyRoutePreflightFingerprintFor(route.preflight)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row{route.ref, relationFingerprintRows([]reconcile.RelationAction{route.action})[0], route.work, preflight})
	}
	payload, err := json.Marshal(struct {
		PhaseEstablished        bool  `json:"phase_established"`
		StatefileInitiallyBound bool  `json:"statefile_initially_bound"`
		Routes                  []row `json:"routes"`
	}{established, bound, rows})
	if err != nil {
		t.Fatal(err)
	}
	return mutation.NewOperationFingerprint(payload)
}
