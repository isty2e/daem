package projection

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func TestAggregateSubjectConstraintBlocksAnOtherwiseWritableProjection(t *testing.T) {
	input := aggregateConstraintTestInput(t)
	subject := input.Expected[0].SubjectID()
	constraint, err := NewAggregateSubjectConstraint(
		subject,
		reconcile.ReasonEffectiveStateConflict,
		"provider-effective source defines the same name",
	)
	if err != nil {
		t.Fatalf("NewAggregateSubjectConstraint returned error: %v", err)
	}
	input.Constraints = []AggregateSubjectConstraint{constraint}

	decisions, err := buildAggregateDecisionsForTest(input)
	if err != nil {
		t.Fatalf("BuildAggregateDecisions returned error: %v", err)
	}
	decision := onlyAggregateDecision(t, decisions)
	if !decision.IsBlocked() ||
		decision.Reason() != reconcile.ReasonEffectiveStateConflict ||
		decision.MutatesHost() {
		t.Fatalf(
			"decision = %q/%q mutates=%t, want blocked effective conflict",
			decision.Kind(),
			decision.Reason(),
			decision.MutatesHost(),
		)
	}
}

func TestAggregateSubjectConstraintRejectsUnknownOrDuplicateSubjects(t *testing.T) {
	input := aggregateConstraintTestInput(t)
	subject := input.Expected[0].SubjectID()
	first, err := NewAggregateSubjectConstraint(
		subject,
		reconcile.ReasonEffectiveStateUnobserved,
		"provider-effective source is opaque",
	)
	if err != nil {
		t.Fatal(err)
	}
	input.Constraints = []AggregateSubjectConstraint{first, first}
	_, err = buildAggregateDecisionsForTest(input)
	if err == nil || !strings.Contains(err.Error(), "duplicate aggregate constraint") {
		t.Fatalf("duplicate constraint error = %v", err)
	}

	foreign := syntheticManagedFileSubject(t, "foreign")
	unknown, err := NewAggregateSubjectConstraint(
		foreign,
		reconcile.ReasonEffectiveStateConflict,
		"foreign constraint",
	)
	if err != nil {
		t.Fatal(err)
	}
	input.Constraints = []AggregateSubjectConstraint{unknown}
	_, err = buildAggregateDecisionsForTest(input)
	if err == nil || !strings.Contains(err.Error(), "has no selected expectation") {
		t.Fatalf("foreign constraint error = %v", err)
	}
}

func aggregateConstraintTestInput(t *testing.T) AggregateInput {
	t.Helper()
	contract := aggregateHookContract(t, "guard", "echo guard")
	desired := aggregateItems(t, contract)
	projection := desired[0].Contribution().Contract()
	return AggregateInput{
		Locked:          aggregateLockedSection(t, contract),
		Expected:        []lock.LockedSubjectContract{contract},
		Desired:         desired,
		Evidence:        []observe.AggregateEvidence{aggregateEvidence(t, projection, aggregate.AbsentDocument())},
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
	}
}
