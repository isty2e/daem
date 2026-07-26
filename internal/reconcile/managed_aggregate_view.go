package reconcile

import (
	"slices"
	"sort"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
)

func (decision AggregateDecision) Kind() AggregateDecisionKind { return decision.kind }

func (decision AggregateDecision) Reason() ActionReason { return decision.reason }

func (decision AggregateDecision) Detail() string { return decision.detail }

func (decision AggregateDecision) Subjects() []topology.SubjectID {
	seen := make(map[topology.SubjectID]struct{})
	for _, projection := range decision.projections {
		for _, subject := range aggregateProjectionSubjects(projection) {
			seen[subject] = struct{}{}
		}
	}
	result := make([]topology.SubjectID, 0, len(seen))
	for subject := range seen {
		result = append(result, subject)
	}
	sort.Slice(result, func(left int, right int) bool {
		return topology.CompareSubjectID(result[left], result[right]) < 0
	})
	return result
}

func (decision AggregateDecision) subjectDeltas() []aggregateSubjectDelta {
	result := make([]aggregateSubjectDelta, 0)
	for _, projection := range decision.projections {
		result = append(result, projection.deltas...)
	}
	sort.Slice(result, func(left int, right int) bool {
		if compared := topology.CompareSubjectID(result[left].subject, result[right].subject); compared != 0 {
			return compared < 0
		}
		return aggregateAddressKey(result[left].contract.Address()) <
			aggregateAddressKey(result[right].contract.Address())
	})
	return result
}

// MutatingSubjects returns canonical subjects whose contribution or owned state
// changes, independently of the physical aggregate write cardinality.
func (decision AggregateDecision) MutatingSubjects() []topology.SubjectID {
	deltas := decision.subjectDeltas()
	result := make([]topology.SubjectID, 0, len(deltas))
	for _, delta := range deltas {
		if aggregateSubjectDeltaMutatesState(delta) {
			result = append(result, delta.subject)
		}
	}
	return result
}

func (decision AggregateDecision) DocumentAddress() aggregate.DocumentAddress {
	return decision.documentAddress
}

func (decision AggregateDecision) CodecContractID() aggregate.CodecContractID {
	return decision.codecContractID
}

func (decision AggregateDecision) Contracts() []aggregate.ProjectionContract {
	result := make([]aggregate.ProjectionContract, len(decision.projections))
	for index, projection := range decision.projections {
		result[index] = projection.contract.Clone()
	}
	return result
}

func (decision AggregateDecision) Projections() []AggregateProjectionDecision {
	return cloneAggregateProjectionDecisions(decision.projections)
}

func (decision AggregateDecision) DesiredContributions() []aggregate.SubjectContribution {
	result := make([]aggregate.SubjectContribution, 0)
	for _, projection := range decision.projections {
		if projection.desired != nil {
			result = append(result, projection.desired.Contributions()...)
		}
	}
	sort.Slice(result, func(left int, right int) bool {
		return topology.CompareSubjectID(result[left].SubjectID(), result[right].SubjectID()) < 0
	})
	return result
}

func (decision AggregateDecision) PreviousStates() []durable.ManagedAggregateState {
	result := make([]durable.ManagedAggregateState, 0)
	for _, projection := range decision.projections {
		result = append(result, projection.previous...)
	}
	sort.Slice(result, func(left int, right int) bool {
		return topology.CompareSubjectID(result[left].Subject(), result[right].Subject()) < 0
	})
	return result
}

func (decision AggregateDecision) BeforeDocument() aggregate.Document { return decision.document }

func (decision AggregateDecision) BeforeSnapshot() aggregate.Snapshot { return decision.snapshot }

func (decision AggregateDecision) CodecPlan() aggregate.Plan { return decision.codecPlan }

func (decision AggregateDecision) Rendered() aggregate.RenderedDocument { return decision.rendered }

func (decision AggregateDecision) Evidence() observe.AggregateEvidence { return decision.evidence }

func (decision AggregateDecision) OperationPreconditions() []aggregate.OperationPrecondition {
	return append([]aggregate.OperationPrecondition(nil), decision.preconditions...)
}

func (decision AggregateDecision) IsBlocked() bool { return decision.kind == AggregateBlocked }

func (decision AggregateDecision) IsNoOp() bool { return decision.kind == AggregateNoOp }

func (decision AggregateDecision) MutatesHost() bool {
	return decision.kind == AggregateCreate || decision.kind == AggregateReplace || decision.kind == AggregateRemove
}

func (decision AggregateDecision) MutatesState() bool {
	return slices.ContainsFunc(decision.subjectDeltas(), aggregateSubjectDeltaMutatesState)
}

func (projection AggregateProjectionDecision) Kind() AggregateDecisionKind { return projection.kind }

func (projection AggregateProjectionDecision) Reason() ActionReason { return projection.reason }

func (projection AggregateProjectionDecision) Detail() string { return projection.detail }

func (projection AggregateProjectionDecision) Contract() aggregate.ProjectionContract {
	return projection.contract.Clone()
}

func (projection AggregateProjectionDecision) Subjects() []topology.SubjectID {
	return aggregateProjectionSubjects(projection)
}

func (projection AggregateProjectionDecision) Desired() (aggregate.ContributionSet, bool) {
	if projection.desired == nil {
		return aggregate.ContributionSet{}, false
	}
	copy, _ := aggregate.NewContributionSet(projection.desired.Contributions())
	return copy, true
}

func (projection AggregateProjectionDecision) PreviousStates() []durable.ManagedAggregateState {
	return append([]durable.ManagedAggregateState(nil), projection.previous...)
}

func (projection AggregateProjectionDecision) Before() aggregate.ProjectionState {
	return projection.before
}

func (projection AggregateProjectionDecision) Expected() aggregate.ProjectionState {
	return projection.expected
}

func (projection AggregateProjectionDecision) MutatesHost() bool {
	for _, delta := range projection.deltas {
		if delta.mutatesHost {
			return true
		}
	}
	return false
}

func (projection AggregateProjectionDecision) MutatesState() bool {
	return slices.ContainsFunc(projection.deltas, aggregateSubjectDeltaMutatesState)
}

func cloneAggregateProjectionDecisions(
	values []AggregateProjectionDecision,
) []AggregateProjectionDecision {
	result := make([]AggregateProjectionDecision, len(values))
	for index, value := range values {
		result[index] = value
		result[index].contract = value.contract.Clone()
		result[index].desired = cloneContributionSetPointer(value.desired)
		result[index].previous = append([]durable.ManagedAggregateState(nil), value.previous...)
		result[index].deltas = append([]aggregateSubjectDelta(nil), value.deltas...)
	}
	return result
}
