package apply

import (
	"fmt"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/operationplan"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func TestRelationOrderValidationCountUsesClassLevelMutation(t *testing.T) {
	alpha := applyOrderMember(t, "alpha", "npm:alpha")
	beta := applyOrderMember(t, "beta", "npm:beta")
	classID, err := hostrelation.NewOrderClassID("extension:pi:project:packages")
	if err != nil {
		t.Fatal(err)
	}

	exact := make([]reconcile.RelationOrderDecision, 0, 512)
	mutating := make([]reconcile.RelationOrderDecision, 0, 512)
	for index := range 512 {
		sequenceID, err := hostrelation.NewPhysicalSequenceID(
			fmt.Sprintf("pi:project:settings.packages.%03d", index),
		)
		if err != nil {
			t.Fatal(err)
		}
		exact = append(exact, relationOrderCapacityDecision(
			t,
			target.TargetPi,
			classID,
			sequenceID,
			"pi-package-load-identity-v1",
			hostrelation.RuntimePrecedence,
			[]hostrelation.RelationOrderMember{alpha, beta},
			[]hostrelation.RelationOrderMember{alpha, beta},
		))
		mutating = append(mutating, relationOrderCapacityDecision(
			t,
			target.TargetPi,
			classID,
			sequenceID,
			"pi-package-load-identity-v1",
			hostrelation.RuntimePrecedence,
			[]hostrelation.RelationOrderMember{alpha, beta},
			[]hostrelation.RelationOrderMember{beta, alpha},
		))
	}

	assertRelationOrderClassCount(t, exact, false, 0)
	assertRelationOrderClassCount(t, exact, true, 1)
	assertRelationOrderClassCount(t, mutating, false, 1)
}

func assertRelationOrderClassCount(
	t testing.TB,
	decisions []reconcile.RelationOrderDecision,
	mayChangeBeforeExecution bool,
	want int,
) {
	t.Helper()
	work := operationplan.ApplyWork{
		OrderClasses: []operationplan.OrderClassWork{{
			RequiresMutation: relationOrderMutationRequired(decisions),
		}},
	}
	if mayChangeBeforeExecution {
		work.CarrierRemovals = []operationplan.CarrierWork{{VerifiesPending: true}}
	}
	envelope, err := operationplan.CompileApply(work)
	if err != nil {
		t.Fatal(err)
	}
	got := 0
	for _, obligation := range envelope.Obligations() {
		if obligation.Kind() == operationplan.ObligationRelationOrderClass {
			got = obligation.Count()
		}
	}
	if got != want {
		t.Fatalf("relation-order class count = %d, want %d", got, want)
	}
}

func relationOrderCapacityDecision(
	t testing.TB,
	selectedTarget target.Target,
	classID hostrelation.OrderClassID,
	sequenceID hostrelation.PhysicalSequenceID,
	contractVersion string,
	runtimeMeaning hostrelation.RuntimeMeaning,
	desired []hostrelation.RelationOrderMember,
	observed []hostrelation.RelationOrderMember,
) reconcile.RelationOrderDecision {
	t.Helper()
	constraint, err := hostrelation.NewRelationOrderConstraint(
		classID,
		contractVersion,
		runtimeMeaning,
		desired,
	)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := observerelation.NewSequenceAuthority(string(sequenceID))
	if err != nil {
		t.Fatal(err)
	}
	revision, err := observerelation.NewSequenceRevision("sha256:capacity")
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]observerelation.ObservedRelationRow, 0, len(observed))
	for _, member := range observed {
		row, err := observerelation.NewCorrelatedObservedRelationRow(
			member.HostLoadIdentity(),
			member.Subject(),
		)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	sequence, err := observerelation.NewObservedRelationSequence(
		classID,
		sequenceID,
		authority,
		revision,
		rows,
	)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := reconcile.NewRelationOrderDecision(reconcile.RelationOrderDecisionInput{
		Target:     selectedTarget,
		Scope:      target.ScopeProject,
		Constraint: constraint,
		Sequence:   sequence,
	})
	if err != nil {
		t.Fatal(err)
	}
	return decision
}
