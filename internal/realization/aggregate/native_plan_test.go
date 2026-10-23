package aggregate

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestNativePlanRetiresParentValueWithoutInventingNodeAbsence(t *testing.T) {
	parentSet, childSet := nativePlanDesired(t, "a"), nativePlanDesired(t, "a", "b")
	parent, child := parentSet.Contract(), childSet.Contract()
	parentBefore, err := NewProjectionState(parent, true, true, "old-parent")
	if err != nil {
		t.Fatal(err)
	}
	childBefore, err := NewBlockedProjectionState(child, parent.Address().KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	selection, err := NewSelection([]ProjectionContract{parent, child})
	if err != nil {
		t.Fatal(err)
	}
	before, err := NewSnapshot(true, selection, []ProjectionState{parentBefore, childBefore})
	if err != nil {
		t.Fatal(err)
	}
	parentIntent, err := NewProjectionIntent(parentBefore, nil)
	if err != nil {
		t.Fatal(err)
	}
	childIntent, err := NewProjectionIntent(childBefore, &childSet)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(before, []ProjectionIntent{childIntent, parentIntent})
	if err != nil || !plan.RetainsStructuralContainer(parent.Address()) || plan.RetainsStructuralContainer(child.Address()) {
		t.Fatalf("native plan did not retain exactly the retired structural parent: %v", err)
	}
	canonical, err := NewPlan(plan.Before(), plan.Intents())
	if err != nil || !canonical.RetainsStructuralContainer(parent.Address()) {
		t.Fatal("plan canonicalization dropped its derived tree relation")
	}
	parentAfter, err := NewProjectionState(parent, true, true, "actual-container-value")
	if err != nil {
		t.Fatal(err)
	}
	childAfter, err := NewProjectionState(child, true, true, "actual-child-value")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := NewSnapshot(true, selection, []ProjectionState{parentAfter, childAfter})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRenderedDocument(ExistingDocument([]byte(`{"a":{"b":2}}`)), plan, expected); err != nil {
		t.Fatalf("truthful surviving container was rejected: %v", err)
	}
	absentParent, err := NewProjectionState(parent, true, false, "")
	if err != nil {
		t.Fatal(err)
	}
	contradictory, err := NewSnapshot(true, selection, []ProjectionState{absentParent, childAfter})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRenderedDocument(ExistingDocument([]byte(`{"a":{"b":2}}`)), plan, contradictory); err == nil {
		t.Fatal("desired descendant allowed a false absent structural parent")
	}
	parentDesired, err := NewProjectionIntent(parentBefore, &parentSet)
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := NewPlan(before, []ProjectionIntent{parentDesired, childIntent})
	if err == nil || len(invalid.Intents()) != 0 {
		t.Fatal("simultaneously desired native ancestor/descendant acquired a plan")
	}

	replaceParent, err := NewProjectionIntent(parentAfter, &parentSet)
	if err != nil {
		t.Fatal(err)
	}
	removeChild, err := NewProjectionIntent(childAfter, nil)
	if err != nil {
		t.Fatal(err)
	}
	inverse, err := NewPlan(expected, []ProjectionIntent{replaceParent, removeChild})
	if err != nil || inverse.RetainsStructuralContainer(child.Address()) {
		t.Fatal("child retirement acquired an unrelated structural-retention relation")
	}
	if _, err := NewRenderedDocument(ExistingDocument([]byte(`{"a":1}`)), inverse, before); err != nil {
		t.Fatalf("truthful child obstruction after replacing its parent was rejected: %v", err)
	}
}

func TestNativePlanUsesLiteralPrefixRangesRatherThanAdjacentOrRawPrefixKeys(t *testing.T) {
	for _, name := range []string{"", "a", "a/b", "~1", ".", "..", "control\n\x00"} {
		parentSet, childSet := nativePlanDesired(t, name), nativePlanDesired(t, name, "child")
		lookalikeSet := nativePlanDesired(t, name+"!")
		parent, child, lookalike := parentSet.Contract(), childSet.Contract(), lookalikeSet.Contract()
		parentBefore, err := NewProjectionState(parent, true, true, "old-parent")
		if err != nil {
			t.Fatal(err)
		}
		childBefore, err := NewProjectionState(child, true, false, "")
		if err != nil {
			t.Fatal(err)
		}
		lookalikeBefore, err := NewProjectionState(lookalike, true, false, "")
		if err != nil {
			t.Fatal(err)
		}
		selection, err := NewSelection([]ProjectionContract{parent, child, lookalike})
		if err != nil {
			t.Fatal(err)
		}
		before, err := NewSnapshot(true, selection, []ProjectionState{parentBefore, childBefore, lookalikeBefore})
		if err != nil {
			t.Fatal(err)
		}
		remove, err := NewProjectionIntent(parentBefore, nil)
		if err != nil {
			t.Fatal(err)
		}
		create, err := NewProjectionIntent(childBefore, &childSet)
		if err != nil {
			t.Fatal(err)
		}
		other, err := NewProjectionIntent(lookalikeBefore, &lookalikeSet)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := NewPlan(before, []ProjectionIntent{other, create, remove})
		if err != nil || !plan.RetainsStructuralContainer(parent.Address()) || plan.RetainsStructuralContainer(lookalike.Address()) {
			t.Fatalf("literal native prefix relation changed for %q: %v", name, err)
		}

		parentDesired, err := NewProjectionIntent(parentBefore, &parentSet)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewPlan(before, []ProjectionIntent{other, create, parentDesired}); err == nil {
			t.Fatal("a sorted lookalike key hid overlapping desired ancestor/descendant scopes")
		}
		unrelatedSelection, err := NewSelection([]ProjectionContract{parent, lookalike})
		if err != nil {
			t.Fatal(err)
		}
		unrelatedBefore, err := NewSnapshot(true, unrelatedSelection, []ProjectionState{parentBefore, lookalikeBefore})
		if err != nil {
			t.Fatal(err)
		}
		unrelated, err := NewPlan(unrelatedBefore, []ProjectionIntent{remove, other})
		if err != nil || unrelated.RetainsStructuralContainer(parent.Address()) {
			t.Fatal("a lexical prefix-lookalike became a desired descendant")
		}
	}
}

func TestLegacyRemovalStillRequiresAbsentExpectedProjection(t *testing.T) {
	placement, admitted := HookPlacementFor(target.TargetCodex, target.ScopeProject)
	if !admitted {
		t.Fatal("Codex Hook placement unavailable")
	}
	contribution, err := placement.Contribution("opaque-hook-payload")
	if err != nil {
		t.Fatal(err)
	}
	contract := contribution.Contract()
	state, err := NewProjectionState(contract, true, true, "old-hook")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := NewSelection([]ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	before, err := NewSnapshot(true, selection, []ProjectionState{state})
	if err != nil {
		t.Fatal(err)
	}
	remove, err := NewProjectionIntent(state, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan(before, []ProjectionIntent{remove})
	if err != nil || plan.RetainsStructuralContainer(contract.Address()) {
		t.Fatal("legacy codec acquired native tree-retirement semantics")
	}
	if _, err := NewRenderedDocument(ExistingDocument([]byte(`{}`)), plan, before); err == nil {
		t.Fatal("legacy removal retained a present projection")
	}
}

func nativePlanDesired(t testing.TB, segments ...string) ContributionSet {
	t.Helper()
	contract := blockedReadContract(t, segments...)
	id, err := entity.New(entity.KindHostConfig, "plan")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeGlobal, contract.Address().KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := NewManagedContribution(contract, "opaque-native-payload")
	if err != nil {
		t.Fatal(err)
	}
	item, err := NewSubjectContribution(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	set, err := NewContributionSet([]SubjectContribution{item})
	if err != nil {
		t.Fatal(err)
	}
	return set
}
