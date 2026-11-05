package record

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestLegacyJournalRefusesBlockedReadBeforeProjection(t *testing.T) {
	parentSet := blockedJournalDesiredSet(t, "a")
	childSet := blockedJournalDesiredSet(t, "a", "b")
	parent, child := parentSet.Contract(), childSet.Contract()
	parentState, err := aggregate.NewProjectionState(parent, true, true, "old-parent")
	if err != nil {
		t.Fatal(err)
	}
	childState, err := aggregate.NewBlockedProjectionState(child, parent.Address().KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{parent, child})
	if err != nil {
		t.Fatal(err)
	}
	before, err := aggregate.NewSnapshot(true, selection, []aggregate.ProjectionState{parentState, childState})
	if err != nil {
		t.Fatal(err)
	}
	parentIntent, err := aggregate.NewProjectionIntent(parentState, nil)
	if err != nil {
		t.Fatal(err)
	}
	childIntent, err := aggregate.NewProjectionIntent(childState, &childSet)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := aggregate.NewPlan(before, []aggregate.ProjectionIntent{parentIntent, childIntent})
	if err != nil {
		t.Fatal(err)
	}
	parentAfter, err := aggregate.NewProjectionState(parent, true, true, "new-parent")
	if err != nil {
		t.Fatal(err)
	}
	childAfter, err := aggregate.NewProjectionState(child, true, true, "new-child")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := aggregate.NewSnapshot(true, selection, []aggregate.ProjectionState{parentAfter, childAfter})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := aggregate.NewRenderedDocument(aggregate.ExistingDocument([]byte(`{"a":{"b":2}}`)), plan, expected)
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := NewManagedAggregateMutation(AggregateMutationCreate, childSet.Contributions()[0].SubjectID(), child, aggregate.ExistingDocument([]byte(`{"a":1}`)), before, rendered, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := pathMutationFromAggregate(mutation)
	if err == nil || projected.Kind != "" || projected.AggregateContract != nil {
		t.Fatal("legacy journal flattened a blocked key observation into missing-value authority")
	}
}

func blockedJournalDesiredSet(t testing.TB, segments ...string) aggregate.ContributionSet {
	t.Helper()
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, outputtest.Parse(t, "@pi-agent/settings.json"), outputtest.KeyFootprint(t, segments...), "native-json-v1")
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindHostConfig, "blocked")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeGlobal, contract.Address().KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := aggregate.NewManagedContribution(contract, "opaque-native-payload")
	if err != nil {
		t.Fatal(err)
	}
	item, err := aggregate.NewSubjectContribution(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
	if err != nil {
		t.Fatal(err)
	}
	return set
}
