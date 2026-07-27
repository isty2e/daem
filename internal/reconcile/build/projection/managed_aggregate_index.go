package projection

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/topology"
)

func canonicalAggregateProjection(
	codec aggregate.Codec,
	contract aggregate.ProjectionContract,
	set aggregate.ContributionSet,
) (string, error) {
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		return "", err
	}
	state, err := aggregate.NewProjectionState(contract, false, false, "")
	if err != nil {
		return "", err
	}
	snapshot, err := aggregate.NewSnapshot(false, selection, []aggregate.ProjectionState{state})
	if err != nil {
		return "", err
	}
	intent, err := aggregate.NewProjectionIntent(state, &set)
	if err != nil {
		return "", err
	}
	plan, err := aggregate.NewPlan(snapshot, []aggregate.ProjectionIntent{intent})
	if err != nil {
		return "", err
	}
	rendered, failure := codec.Render(aggregate.AbsentDocument(), plan)
	if failure != nil {
		return "", failure
	}
	states := rendered.Expected().States()
	if len(states) != 1 || !states[0].Present() {
		return "", fmt.Errorf("aggregate codec omitted desired projection")
	}
	return states[0].CanonicalProjection(), nil
}

func aggregateContributionFromLocked(
	contract lock.LockedSubjectContract,
) (aggregate.SubjectContribution, bool, error) {
	return contract.ManagedAggregateContribution()
}

func aggregateEvidenceIndex(values []observe.AggregateEvidence) (map[aggregate.DocumentAddress]observe.AggregateEvidence, error) {
	result := make(map[aggregate.DocumentAddress]observe.AggregateEvidence, len(values))
	for index, evidence := range values {
		address := evidence.Address()
		if _, duplicate := result[address]; duplicate {
			return nil, fmt.Errorf("duplicate aggregate evidence[%d] for document", index)
		}
		result[address] = evidence
	}
	return result, nil
}

func aggregateObservationFailureIndex(
	values []observe.AggregateObservationFailure,
) (map[aggregate.DocumentAddress]observe.AggregateObservationFailure, error) {
	result := make(map[aggregate.DocumentAddress]observe.AggregateObservationFailure, len(values))
	for index, failure := range values {
		address := failure.Address()
		if _, duplicate := result[address]; duplicate {
			return nil, fmt.Errorf("duplicate aggregate observation failure[%d] for document", index)
		}
		result[address] = failure
	}
	return result, nil
}

func aggregatePreconditionEvidenceIndex(
	values []observe.AggregatePreconditionEvidence,
) (map[aggregate.DocumentAddress][]observe.AggregatePreconditionEvidence, error) {
	result := make(map[aggregate.DocumentAddress][]observe.AggregatePreconditionEvidence)
	for index, evidence := range values {
		if err := evidence.Owner().Validate(); err != nil {
			return nil, fmt.Errorf("aggregate precondition evidence[%d]: %w", index, err)
		}
		result[evidence.Owner()] = append(result[evidence.Owner()], evidence)
	}
	return result, nil
}

func validateAggregateObservationCoverage(
	documents map[aggregate.DocumentAddress][]aggregateGroupInput,
	evidence map[aggregate.DocumentAddress]observe.AggregateEvidence,
	failures map[aggregate.DocumentAddress]observe.AggregateObservationFailure,
	preconditions map[aggregate.DocumentAddress][]observe.AggregatePreconditionEvidence,
) error {
	for address := range evidence {
		if _, selected := documents[address]; !selected {
			return fmt.Errorf("aggregate evidence covers an unselected document")
		}
		if _, alsoFailed := failures[address]; alsoFailed {
			return fmt.Errorf("aggregate document has both evidence and failed observation")
		}
	}
	for address := range failures {
		if _, selected := documents[address]; !selected {
			return fmt.Errorf("aggregate failed observation covers an unselected document")
		}
	}
	for address := range preconditions {
		if _, selected := documents[address]; !selected {
			return fmt.Errorf("aggregate precondition evidence covers an unselected document")
		}
	}
	return nil
}

func aggregateGroupSubjects(group aggregateGroupInput) []topology.SubjectID {
	seen := make(map[topology.SubjectID]struct{}, len(group.desired)+len(group.previous)+len(group.blocked))
	for _, item := range group.desired {
		seen[item.SubjectID()] = struct{}{}
	}
	for _, state := range group.previous {
		seen[state.Subject()] = struct{}{}
	}
	for subject := range group.blocked {
		seen[subject] = struct{}{}
	}
	result := make([]topology.SubjectID, 0, len(seen))
	for subject := range seen {
		result = append(result, subject)
	}
	sort.Slice(result, func(left int, right int) bool { return topology.CompareSubjectID(result[left], result[right]) < 0 })
	return result
}

func aggregateSubjectItem(
	group aggregateGroupInput,
	subject topology.SubjectID,
) (aggregate.SubjectContribution, error) {
	for _, item := range group.desired {
		if item.SubjectID() == subject {
			return item, nil
		}
	}
	for _, state := range group.previous {
		if state.Subject() != subject {
			continue
		}
		return aggregate.NewSubjectContribution(subject, state.Contribution())
	}
	if fact, present := group.blocked[subject]; present {
		return fact.item, nil
	}
	return aggregate.SubjectContribution{}, fmt.Errorf("aggregate group has no contribution for subject %q", subject)
}

func cloneContributionSetPointer(value *aggregate.ContributionSet) *aggregate.ContributionSet {
	if value == nil {
		return nil
	}
	copy, _ := aggregate.NewContributionSet(value.Contributions())
	return &copy
}

func aggregateAddressKey(address aggregate.ProjectionAddress) string {
	document := address.Document()
	return string(document.Target()) + "\x00" + string(document.Scope()) + "\x00" + document.AggregateRoot().String() + "\x00" + address.PlacementID() + "\x00" + string(address.MergeUnit()) + "\x00" + string(address.ContentPath())
}

func aggregateDocumentAddressKey(address aggregate.DocumentAddress) string {
	return string(address.Target()) + "\x00" + string(address.Scope()) + "\x00" + address.AggregateRoot().String()
}

func aggregateDecisionKey(decision aggregateDecision) string {
	return aggregateDocumentAddressKey(decision.documentAddress) + "\x00" + string(decision.codecContractID)
}
