package reconcile

import (
	"sort"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
)

func aggregateProjectionSubjects(projection AggregateProjectionDecision) []topology.SubjectID {
	seen := make(map[topology.SubjectID]struct{}, len(projection.previous)+len(projection.deltas))
	for _, state := range projection.previous {
		seen[state.Subject()] = struct{}{}
	}
	if projection.desired != nil {
		for _, item := range projection.desired.Contributions() {
			seen[item.SubjectID()] = struct{}{}
		}
	}
	for _, delta := range projection.deltas {
		seen[delta.subject] = struct{}{}
	}
	subjects := make([]topology.SubjectID, 0, len(seen))
	for subject := range seen {
		subjects = append(subjects, subject)
	}
	sort.Slice(subjects, func(left int, right int) bool {
		return topology.CompareSubjectID(subjects[left], subjects[right]) < 0
	})
	return subjects
}

func aggregateSubjectDeltaMutatesState(delta aggregateSubjectDelta) bool {
	return delta.kind == AggregateCreate || delta.kind == AggregateReplace ||
		delta.kind == AggregateRemove || delta.kind == AggregateRecord
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
	return string(document.Target()) + "\x00" + string(document.Scope()) + "\x00" +
		document.AggregateRoot() + "\x00" + address.PlacementID() + "\x00" +
		string(address.MergeUnit()) + "\x00" + string(address.ContentPath())
}

func aggregateDocumentAddressKey(address aggregate.DocumentAddress) string {
	return string(address.Target()) + "\x00" + string(address.Scope()) + "\x00" + address.AggregateRoot()
}

func aggregateDecisionKey(decision AggregateDecision) string {
	return aggregateDocumentAddressKey(decision.documentAddress) + "\x00" + string(decision.codecContractID)
}
