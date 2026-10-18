package reconcile

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/topology"
)

func TestAggregateBlockedSubjectsPreserveDocumentAndSubjectBoundaries(t *testing.T) {
	tests := []struct {
		name         string
		documentKind AggregateDecisionKind
		subjectKind  AggregateDecisionKind
		want         []string
	}{
		{name: "clear", documentKind: AggregateNoOp, subjectKind: AggregateNoOp},
		{name: "subject only", documentKind: AggregateNoOp, subjectKind: AggregateBlocked, want: []string{"alpha"}},
		{name: "whole document", documentKind: AggregateBlocked, subjectKind: AggregateNoOp, want: []string{"alpha", "zeta"}},
		{name: "both", documentKind: AggregateBlocked, subjectKind: AggregateBlocked, want: []string{"alpha", "zeta"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := aggregateDecisionInputForServer(t, "zeta")
			sibling := aggregateDecisionInputForServer(t, "alpha")
			sibling.Projections[0].Subjects[0].Kind = test.subjectKind
			input.Projections = append(input.Projections, sibling.Projections...)
			input.Kind = test.documentKind
			decision, err := NewAggregateDecision(input)
			if err != nil {
				t.Fatal(err)
			}

			blocked := decision.BlockedSubjects()
			keys := make([]string, 0, len(blocked))
			for _, subject := range blocked {
				keys = append(keys, subject.Key())
			}
			if !slices.Equal(keys, test.want) {
				t.Fatalf("blocked subjects = %v, want %v", keys, test.want)
			}

			if len(blocked) != 0 {
				blocked[0] = topology.SubjectID{}
				if decision.BlockedSubjects()[0].Key() != test.want[0] {
					t.Fatal("blocked subject accessor exposed mutable model storage")
				}
			}
		})
	}
}
