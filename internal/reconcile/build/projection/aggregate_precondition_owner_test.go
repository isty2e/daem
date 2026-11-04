package projection

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
)

func TestAggregatePreconditionRequiresItsSelectedCodecOwner(t *testing.T) {
	locked := aggregateOpenCodeMCPContract(t, "peer", "npx", []string{})
	items := aggregateItems(t, locked)
	contracts := aggregateContractsFromItems(items)
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	preconditions, admitted, err := aggregate.OperationPreconditionsForSelection(selection)
	if err != nil || !admitted || len(preconditions) != 1 {
		t.Fatal("fixture lacks an actual fixed codec precondition", err)
	}
	input := AggregateInput{
		Locked: aggregateLockedSection(t, locked), Expected: []lock.LockedSubjectContract{locked}, Desired: items,
		Evidence:        []observe.AggregateEvidence{aggregateEvidenceForContracts(t, contracts, aggregate.AbsentDocument())},
		SelectedTargets: planSelectedTargets(t, target.TargetOpenCode),
	}
	wrong, err := aggregate.NewDocumentCodecKey(selection.DocumentAddress(), "unselected-codec-v1")
	if err != nil {
		t.Fatal(err)
	}
	misbound, err := observe.NewAggregatePreconditionEvidence(wrong, preconditions[0], true)
	if err != nil {
		t.Fatal(err)
	}
	input.PreconditionEvidence = []observe.AggregatePreconditionEvidence{misbound}
	if result, err := buildAggregateDecisionsForTest(input); err == nil || len(result) != 0 {
		t.Fatal("another codec supplied a selected document's precondition")
	}
	matched, err := observe.NewAggregatePreconditionEvidence(selection.DocumentCodecKey(), preconditions[0], true)
	if err != nil || matched.Owner() != selection.DocumentCodecKey() {
		t.Fatal("precondition lost its canonical codec owner", err)
	}
	input.PreconditionEvidence = []observe.AggregatePreconditionEvidence{matched}
	decisions, err := buildAggregateDecisionsForTest(input)
	if err != nil || len(decisions) != 1 || decisions[0].IsBlocked() {
		t.Fatal("properly owned fixed precondition was refused", err)
	}
	if _, err := observe.NewAggregatePreconditionEvidence(aggregate.DocumentCodecKey{}, preconditions[0], true); err == nil {
		t.Fatal("zero precondition owner became valid")
	}
}
