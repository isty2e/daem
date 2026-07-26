package projection

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type selectedTargets map[target.Target]struct{}

func selectedTargetSet(targets reconcile.SelectedTargets) selectedTargets {
	selected := make(selectedTargets, targets.Len())
	for _, selectedTarget := range targets.Values() {
		selected[selectedTarget] = struct{}{}
	}
	return selected
}

func (selected selectedTargets) Includes(target target.Target) bool {
	_, ok := selected[target]
	return ok
}

func expectedAggregateSubjects(
	locked lock.LockedSection,
	values []lock.LockedSubjectContract,
	selection selectedTargets,
	codecs aggregate.CodecCatalog,
) (map[topology.SubjectID]aggregate.SubjectContribution, []aggregateBlockedSubject, error) {
	expected := make(map[topology.SubjectID]aggregate.SubjectContribution, len(values))
	blocked := make([]aggregateBlockedSubject, 0)
	for index, contract := range values {
		contribution, ok, err := aggregateContributionFromLocked(contract)
		if err != nil {
			return nil, nil, fmt.Errorf("expected aggregate[%d]: %w", index, err)
		}
		if !ok || !selection.Includes(contribution.Contribution().Target()) {
			continue
		}
		subject := contribution.SubjectID()
		if _, duplicate := expected[subject]; duplicate {
			return nil, nil, fmt.Errorf("duplicate expected aggregate subject %q", subject)
		}
		expected[subject] = contribution
		actual, present := locked.Subject(subject)
		switch {
		case !present:
			blocked = append(blocked, aggregateBlockedSubject{
				item: contribution, reason: reconcile.ReasonMissingLock,
				detail: "expected aggregate contribution is absent from lock",
			})
		case !actual.Equal(contract):
			blocked = append(blocked, aggregateBlockedSubject{
				item: contribution, reason: reconcile.ReasonStaleLock,
				detail: "locked aggregate contribution does not match manifest lowering",
			})
		}
	}
	for _, contract := range locked.Subjects() {
		contribution, ok, err := aggregateContributionFromLocked(contract)
		if err != nil {
			return nil, nil, err
		}
		if !ok || !selection.Includes(contribution.Contribution().Target()) {
			continue
		}
		if _, admitted := codecs.Lookup(contribution.Contribution().CodecContractID()); !admitted {
			continue
		}
		if _, declared := expected[contract.SubjectID()]; !declared {
			blocked = append(blocked, aggregateBlockedSubject{
				item: contribution, reason: reconcile.ReasonUnexpectedLockSubject,
				detail: "locked aggregate contribution is not declared by the manifest",
			})
		}
	}
	return expected, blocked, nil
}

func desiredAggregateSubjects(
	values []aggregate.SubjectContribution,
	expected map[topology.SubjectID]aggregate.SubjectContribution,
	selection selectedTargets,
) (map[topology.SubjectID]aggregate.SubjectContribution, error) {
	desired := make(map[topology.SubjectID]aggregate.SubjectContribution, len(values))
	for index, item := range values {
		contribution := item.Contribution()
		if !selection.Includes(contribution.Target()) {
			continue
		}
		expectedItem, declared := expected[item.SubjectID()]
		if !declared {
			return nil, fmt.Errorf("desired aggregate[%d] subject %q has no manifest lock expectation", index, item.SubjectID())
		}
		if !expectedItem.Contribution().Contract().Equal(contribution.Contract()) {
			return nil, fmt.Errorf("desired aggregate[%d] subject %q changes its static lock contract", index, item.SubjectID())
		}
		if _, duplicate := desired[item.SubjectID()]; duplicate {
			return nil, fmt.Errorf("duplicate desired aggregate subject %q", item.SubjectID())
		}
		desired[item.SubjectID()] = item
	}
	for subject := range expected {
		if _, present := desired[subject]; !present {
			return nil, fmt.Errorf("expected aggregate subject %q has no ephemeral desired contribution", subject)
		}
	}
	return desired, nil
}

func selectedAggregateStates(
	values []durable.ManagedAggregateState,
	selection selectedTargets,
) (map[topology.SubjectID]durable.ManagedAggregateState, error) {
	states := make(map[topology.SubjectID]durable.ManagedAggregateState, len(values))
	for index, state := range values {
		if !selection.Includes(state.Contribution().Target()) {
			continue
		}
		if _, duplicate := states[state.Subject()]; duplicate {
			return nil, fmt.Errorf("duplicate managed aggregate state[%d] subject %q", index, state.Subject())
		}
		states[state.Subject()] = state
	}
	return states, nil
}
