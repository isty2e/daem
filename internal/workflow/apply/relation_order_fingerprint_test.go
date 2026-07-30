package apply

import (
	"errors"
	"reflect"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestRelationOrderFingerprintBindsDesiredOrderAndObservedRevision(t *testing.T) {
	alpha := applyOrderMember(t, "alpha", "npm:alpha")
	beta := applyOrderMember(t, "beta", "npm:beta")
	first := applyOrderDecision(t, []hostrelation.RelationOrderMember{alpha, beta}, "sha256:first")
	same := applyOrderDecision(t, []hostrelation.RelationOrderMember{alpha, beta}, "sha256:first")
	reordered := applyOrderDecision(t, []hostrelation.RelationOrderMember{beta, alpha}, "sha256:first")
	revised := applyOrderDecision(t, []hostrelation.RelationOrderMember{alpha, beta}, "sha256:second")

	firstRows := relationOrderFingerprintRows([]reconcile.RelationOrderDecision{first})
	if !reflect.DeepEqual(firstRows, relationOrderFingerprintRows(
		[]reconcile.RelationOrderDecision{same},
	)) {
		t.Fatal("equal relation order facts produced different fingerprint rows")
	}
	if reflect.DeepEqual(firstRows, relationOrderFingerprintRows(
		[]reconcile.RelationOrderDecision{reordered},
	)) {
		t.Fatal("relation order fingerprint ignored desired member order")
	}
	if reflect.DeepEqual(firstRows, relationOrderFingerprintRows(
		[]reconcile.RelationOrderDecision{revised},
	)) {
		t.Fatal("relation order fingerprint ignored observed sequence revision")
	}
}

func TestRejectBlockedRelationOrdersReturnsTypedApplyError(t *testing.T) {
	alpha := applyOrderMember(t, "alpha", "npm:alpha")
	beta := applyOrderMember(t, "beta", "npm:beta")
	classID, err := hostrelation.NewOrderClassID("extension:pi:project:packages")
	if err != nil {
		t.Fatal(err)
	}
	constraint, err := hostrelation.NewRelationOrderConstraint(
		classID,
		"pi-package-load-identity-v1",
		hostrelation.RuntimePrecedence,
		[]hostrelation.RelationOrderMember{alpha, beta},
	)
	if err != nil {
		t.Fatal(err)
	}
	sequenceID, err := hostrelation.NewPhysicalSequenceID("pi:project:settings.packages")
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := reconcile.NewBlockedRelationOrderDecision(
		reconcile.BlockedRelationOrderDecisionInput{
			Target:     target.TargetPi,
			Scope:      target.ScopeProject,
			Constraint: constraint,
			SequenceID: sequenceID,
			Reason:     reconcile.OrderReasonObservationUnavailable,
			Detail:     "settings file is malformed",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:        reconcile.ContextApply,
		RelationOrders: []reconcile.RelationOrderDecision{blocked},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectBlockedRelationOrders(result); !errors.Is(err, ErrRelationOrderBlock) {
		t.Fatalf("rejectBlockedRelationOrders error = %v", err)
	}
}

func applyOrderDecision(
	t testing.TB,
	members []hostrelation.RelationOrderMember,
	revisionValue string,
) reconcile.RelationOrderDecision {
	t.Helper()
	classID, err := hostrelation.NewOrderClassID("extension:pi:project:packages")
	if err != nil {
		t.Fatal(err)
	}
	constraint, err := hostrelation.NewRelationOrderConstraint(
		classID,
		"pi-package-load-identity-v1",
		hostrelation.RuntimePrecedence,
		members,
	)
	if err != nil {
		t.Fatal(err)
	}
	sequenceID, err := hostrelation.NewPhysicalSequenceID("pi:project:settings.packages")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := observerelation.NewSequenceAuthority(
		"pi:project:settings.packages",
	)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := observerelation.NewSequenceRevision(revisionValue)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]observerelation.ObservedRelationRow, 0, len(members))
	for _, member := range members {
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
		Target:     target.TargetPi,
		Scope:      target.ScopeProject,
		Constraint: constraint,
		Sequence:   sequence,
	})
	if err != nil {
		t.Fatal(err)
	}
	return decision
}

func applyOrderMember(
	t testing.TB,
	key string,
	loadIdentityValue string,
) hostrelation.RelationOrderMember {
	t.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"test.pi-package",
		key,
	)
	if err != nil {
		t.Fatal(err)
	}
	loadIdentity, err := hostrelation.NewHostLoadIdentity(loadIdentityValue)
	if err != nil {
		t.Fatal(err)
	}
	member, err := hostrelation.NewRelationOrderMember(subject, loadIdentity)
	if err != nil {
		t.Fatal(err)
	}
	return member
}
