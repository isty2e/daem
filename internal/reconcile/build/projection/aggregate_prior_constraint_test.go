package projection

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func TestAggregatePriorOnlyConstraintBlocksWithoutDesiredOrRetirement(t *testing.T) {
	input := aggregateConstraintTestInput(t)
	created, err := buildAggregateDecisionsForTest(input)
	if err != nil {
		t.Fatal(err)
	}
	item := input.Desired[0]
	prior, err := durable.NewManagedAggregateState(item.SubjectID(), item.Contribution())
	if err != nil {
		t.Fatal(err)
	}
	input.Evidence = []observe.AggregateEvidence{aggregateEvidence(t, item.Contribution().Contract(), onlyAggregateDecision(t, created).Rendered().Document())}
	input.States, input.Expected, input.Desired, input.Locked = []durable.ManagedAggregateState{prior}, nil, nil, aggregateLockedSection(t)
	constraint, err := NewAggregateSubjectConstraint(prior.Subject(), reconcile.ReasonEffectiveStateConflict, "another selected transition is blocked")
	if err != nil {
		t.Fatal(err)
	}
	input.Constraints = []AggregateSubjectConstraint{constraint}
	decisions, err := buildAggregateDecisionsForTest(input)
	if err != nil {
		t.Fatal("actual selected prior-only subject could not be denied", err)
	}
	decision := onlyAggregateDecision(t, decisions)
	if !decision.IsBlocked() || decision.MutatesHost() || decision.MutatesState() || decision.Reason() != constraint.Reason() || len(decision.DesiredContributions()) != 0 || len(decision.PreviousStates()) != 1 || !decision.PreviousStates()[0].Equal(prior) {
		t.Fatal("prior-only denial changed intent, baseline or failure cause")
	}
	input.Constraints = nil
	removals, err := buildAggregateDecisionsForTest(input)
	if err != nil || onlyAggregateDecision(t, removals).Kind() != reconcile.AggregateRemove {
		t.Fatal("absence of a declaration without a blocker stopped genuine retirement", err)
	}
	for _, fault := range []string{"duplicate", "unselected", "unknown"} {
		t.Run(fault, func(t *testing.T) {
			candidate := input
			candidate.Constraints = []AggregateSubjectConstraint{constraint}
			switch fault {
			case "duplicate":
				candidate.Constraints = append(candidate.Constraints, constraint)
			case "unselected":
				candidate.SelectedTargets = planSelectedTargets(t, target.TargetPi)
			case "unknown":
				candidate.States, candidate.Evidence = nil, nil
			}
			if result, err := buildAggregateDecisionsForTest(candidate); err == nil || len(result) != 0 {
				t.Fatal("invalid prior constraint yielded a usable decision prefix")
			}
		})
	}
}
