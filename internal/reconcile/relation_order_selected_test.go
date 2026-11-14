package reconcile

import (
	"slices"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestSelectedOrderDecisionClassifiesSurvivorsWithoutReplacingCASEvidence(t *testing.T) {
	alpha := orderTestMember(t, "alpha", "alpha")
	beta := orderTestMember(t, "beta", "beta")
	retired := orderTestMember(t, "retired", "retired")
	capability, constraint, original := selectedOrderTestInputs(t, []observerelation.ObservedRelationRow{
		orderTestRow(t, beta), orderTestForeignRow(t, "foreign"), orderTestRow(t, retired), orderTestRow(t, alpha),
	}, alpha, beta)
	selected, err := observerelation.NewSelectedSequenceEdit(capability, original, constraint, []int{2})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := NewSelectedRelationOrderDecision(SelectedRelationOrderDecisionInput{
		Target: target.TargetOpenCode, Scope: target.ScopeProject, Constraint: constraint, Selected: selected,
	})
	if err != nil || decision.Validate() != nil {
		t.Fatal("selected order was not canonical", err)
	}
	if decision.Kind() != OrderNormalize || decision.ForeignRowCount() != 1 ||
		!slices.Equal(decision.PrecedenceChanges(), selected.PrecedenceChanges()) || len(decision.PrecedenceChanges()) != 2 {
		t.Fatal("risk was not computed from the surviving foreign row", decision)
	}
	if decision.Authority() != original.Authority() || decision.Revision() != original.Revision() ||
		decision.SequenceID() != original.SequenceID() || !decision.sequence.SameOrdering(original) {
		t.Fatal("survivor baseline replaced original CAS evidence")
	}

	ordinary, err := NewRelationOrderDecision(RelationOrderDecisionInput{
		Target: target.TargetOpenCode, Scope: target.ScopeProject, Constraint: constraint, Sequence: original,
		PendingRemovals: []topology.SubjectID{retired.Subject()},
	})
	if err != nil || ordinary.Kind() != OrderConditionalAfterCarrierChange || len(ordinary.PrecedenceChanges()) != 0 {
		t.Fatal("ordinary removal-before-order semantics changed", ordinary, err)
	}
}

func TestSelectedOrderDecisionSeparatesRemovalFromOrderAndPendingInstallation(t *testing.T) {
	alpha := orderTestMember(t, "alpha", "alpha")
	beta := orderTestMember(t, "beta", "beta")
	retired := orderTestMember(t, "retired", "retired")
	for _, test := range []struct {
		name    string
		rows    []observerelation.ObservedRelationRow
		pending []topology.SubjectID
		kind    RelationOrderDecisionKind
		reason  RelationOrderReason
	}{
		{"removal_only", []observerelation.ObservedRelationRow{orderTestRow(t, alpha), orderTestRow(t, retired), orderTestRow(t, beta)}, nil, OrderExact, OrderReasonNone},
		{"pending_install", []observerelation.ObservedRelationRow{orderTestRow(t, alpha), orderTestRow(t, retired)}, []topology.SubjectID{beta.Subject()}, OrderConditionalAfterCarrierChange, OrderReasonPendingCarrierInstall},
	} {
		t.Run(test.name, func(t *testing.T) {
			capability, constraint, original := selectedOrderTestInputs(t, test.rows, alpha, beta)
			selected, err := observerelation.NewSelectedSequenceEdit(capability, original, constraint, []int{1})
			if err != nil {
				t.Fatal(err)
			}
			decision, err := NewSelectedRelationOrderDecision(SelectedRelationOrderDecisionInput{
				Target: target.TargetOpenCode, Scope: target.ScopeProject, Constraint: constraint, Selected: selected, PendingInstalls: test.pending,
			})
			if err != nil || decision.Validate() != nil || decision.Kind() != test.kind || decision.Reason() != test.reason ||
				decision.RequiresMutation() || len(decision.PrecedenceChanges()) != 0 || !selected.Changed() {
				t.Fatal("membership and order outcomes were conflated", decision, err)
			}
		})
	}
}

func TestSelectedOrderDecisionRejectsNoncanonicalProjectionAndRetirementOnly(t *testing.T) {
	alpha := orderTestMember(t, "alpha", "alpha")
	beta := orderTestMember(t, "beta", "beta")
	capability, constraint, original := selectedOrderTestInputs(t, []observerelation.ObservedRelationRow{orderTestRow(t, beta), orderTestRow(t, alpha)}, alpha, beta)
	selected, err := observerelation.NewSelectedSequenceEdit(capability, original, constraint, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		class    hostrelation.OrderClassID
		contract string
		meaning  hostrelation.RuntimeMeaning
		members  []hostrelation.RelationOrderMember
	}{
		{"wrong_class", "another-class", constraint.MemberIdentityContract(), constraint.RuntimeMeaning(), []hostrelation.RelationOrderMember{alpha}},
		{"wrong_identity_contract", constraint.ClassID(), "different-identity-v1", constraint.RuntimeMeaning(), []hostrelation.RelationOrderMember{alpha}},
		{"wrong_runtime_meaning", constraint.ClassID(), constraint.MemberIdentityContract(), hostrelation.RuntimeUnknown, []hostrelation.RelationOrderMember{alpha}},
		{"reversed_members", constraint.ClassID(), constraint.MemberIdentityContract(), constraint.RuntimeMeaning(), []hostrelation.RelationOrderMember{beta, alpha}},
		{"changed_load_identity", constraint.ClassID(), constraint.MemberIdentityContract(), constraint.RuntimeMeaning(), []hostrelation.RelationOrderMember{orderTestMember(t, "alpha", "other-alpha")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			projected, err := hostrelation.NewRelationOrderConstraint(test.class, test.contract, test.meaning, test.members)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := NewSelectedRelationOrderDecision(SelectedRelationOrderDecisionInput{
				Target: target.TargetOpenCode, Scope: target.ScopeProject, Constraint: projected, Selected: selected,
			}); err == nil {
				t.Fatal("incompatible selected order constraint was admitted")
			}
		})
	}
	_, _, retirementOriginal := selectedOrderTestInputs(t, []observerelation.ObservedRelationRow{orderTestRow(t, alpha)}, alpha, beta)
	retirement, err := observerelation.NewRetirementSequenceEdit(capability, retirementOriginal, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []observerelation.SelectedSequenceEdit{{}, retirement} {
		if _, err := NewSelectedRelationOrderDecision(SelectedRelationOrderDecisionInput{
			Target: target.TargetOpenCode, Scope: target.ScopeProject, Constraint: constraint, Selected: candidate,
		}); err == nil {
			t.Fatal("retirement-only or zero edit fabricated an order class")
		}
	}
	projected, err := hostrelation.NewRelationOrderConstraint(constraint.ClassID(), constraint.MemberIdentityContract(), constraint.RuntimeMeaning(), []hostrelation.RelationOrderMember{beta})
	if err != nil {
		t.Fatal(err)
	}
	_, _, subsetOriginal := selectedOrderTestInputs(t, []observerelation.ObservedRelationRow{orderTestRow(t, beta)}, alpha, beta)
	subset, err := observerelation.NewSelectedSequenceEdit(capability, subsetOriginal, constraint, nil)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := NewSelectedRelationOrderDecision(SelectedRelationOrderDecisionInput{
		Target: target.TargetOpenCode, Scope: target.ScopeProject, Constraint: projected, Selected: subset,
	})
	if err != nil || decision.Kind() != OrderExact || decision.Validate() != nil {
		t.Fatal("valid loaded member projection was rejected", decision, err)
	}
}

func TestSelectedPendingInstallDisclosesKnownSurvivorPrecedence(t *testing.T) {
	alpha := orderTestMember(t, "alpha", "alpha")
	beta := orderTestMember(t, "beta", "beta")
	gamma := orderTestMember(t, "gamma", "gamma")
	retired := orderTestMember(t, "retired", "retired")
	capability, constraint, original := selectedOrderTestInputs(t, []observerelation.ObservedRelationRow{
		orderTestRow(t, beta), orderTestForeignRow(t, "foreign"), orderTestRow(t, retired), orderTestRow(t, alpha),
	}, alpha, beta, gamma)
	selected, err := observerelation.NewSelectedSequenceEdit(capability, original, constraint, []int{2})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := NewSelectedRelationOrderDecision(SelectedRelationOrderDecisionInput{
		Target: target.TargetOpenCode, Scope: target.ScopeProject, Constraint: constraint, Selected: selected,
		PendingInstalls: []topology.SubjectID{gamma.Subject()},
	})
	if err != nil || decision.Validate() != nil || decision.Kind() != OrderConditionalAfterCarrierChange ||
		decision.Reason() != OrderReasonPendingCarrierInstall || decision.RequiresMutation() ||
		len(decision.PrecedenceChanges()) != 2 || !slices.Equal(decision.PrecedenceChanges(), selected.PrecedenceChanges()) {
		t.Fatal("pending installation hid already-known surviving-row precedence", decision, err)
	}
}

func TestSelectedOrderDecisionValidationRetainsItsInputVariant(t *testing.T) {
	alpha := orderTestMember(t, "alpha", "alpha")
	retired := orderTestMember(t, "retired", "retired")
	capability, constraint, original := selectedOrderTestInputs(t, []observerelation.ObservedRelationRow{orderTestRow(t, retired), orderTestRow(t, alpha)}, alpha)
	selected, err := observerelation.NewSelectedSequenceEdit(capability, original, constraint, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := NewSelectedRelationOrderDecision(SelectedRelationOrderDecisionInput{
		Target: target.TargetOpenCode, Scope: target.ScopeProject, Constraint: constraint, Selected: selected,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, forge := range []func(*RelationOrderDecision){
		func(decision *RelationOrderDecision) { decision.selected = nil },
		func(decision *RelationOrderDecision) { decision.hasSequence = false },
		func(decision *RelationOrderDecision) {
			decision.pendingRemovals = []topology.SubjectID{retired.Subject()}
		},
		func(decision *RelationOrderDecision) {
			decision.sequence, err = observerelation.NewObservedRelationSequence(original.ClassID(), original.SequenceID(), original.Authority(), "another-revision", original.OrderedRows())
			if err != nil {
				t.Fatal(err)
			}
		},
	} {
		forged := decision
		forge(&forged)
		if err := forged.Validate(); err == nil {
			t.Fatal("forged selected input evidence was accepted")
		}
	}
}

func selectedOrderTestInputs(
	t testing.TB,
	rows []observerelation.ObservedRelationRow,
	members ...hostrelation.RelationOrderMember,
) (profile.ExtensionOrderCapability, hostrelation.RelationOrderConstraint, observerelation.ObservedRelationSequence) {
	t.Helper()
	capability, admitted := hostsurfacecatalog.Product().ExtensionOrderCapability(target.TargetOpenCode, target.ScopeProject, desiredextension.CarrierOpenCodePlugin)
	if !admitted {
		t.Fatal("fixture has no order capability")
	}
	constraint, err := hostrelation.NewRelationOrderConstraint(capability.ClassID(), capability.MemberIdentityContract(), capability.RuntimeMeaning(), members)
	if err != nil {
		t.Fatal(err)
	}
	original := orderTestSequence(t, constraint, string(capability.NativeSequences()[0].SequenceID()), rows)
	return capability, constraint, original
}
